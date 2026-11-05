package llm

import (
	"context"
	"io"
	"math/rand"
	"net/http"
	"strconv"
	"time"
)

const retryBaseDelay = 500 * time.Millisecond

type requestTimeoutContextKey struct{}

// WithRequestTimeout associates a per-request timeout with a parent context.
// A zero or negative timeout means requests inherit only parent cancellation.
func WithRequestTimeout(parent context.Context, timeout time.Duration) context.Context {
	return context.WithValue(parent, requestTimeoutContextKey{}, timeout)
}

// DoWithRetry performs an HTTP operation with retries for transient failures.
// maxRetries is additional attempts after the initial request.
func DoWithRetry(ctx context.Context, fn func(context.Context) (*http.Response, error), maxRetries int) (*http.Response, error) {
	if maxRetries < 0 {
		maxRetries = 0
	}
	random := rand.New(rand.NewSource(time.Now().UnixNano()))
	timeout, _ := ctx.Value(requestTimeoutContextKey{}).(time.Duration)
	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		attemptCtx, cancel := newAttemptContext(ctx, timeout)
		response, err := fn(attemptCtx)
		if err != nil {
			attemptErr := attemptCtx.Err()
			cancel()
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if attemptErr != nil {
				err = attemptErr
			}
			if attempt >= maxRetries {
				return nil, err
			}
			if waitErr := waitForRetry(ctx, jitteredBackoff(attempt, random)); waitErr != nil {
				return nil, waitErr
			}
			continue
		}
		if response == nil {
			cancel()
			return nil, nil
		}
		if retryableStatus(response.StatusCode) && attempt < maxRetries {
			delay := jitteredBackoff(attempt, random)
			if retryAfter, ok := retryAfterDelay(response.Header.Get("Retry-After")); ok {
				delay = retryAfter
			}
			if response.Body != nil {
				_ = response.Body.Close()
			}
			cancel()
			if waitErr := waitForRetry(ctx, delay); waitErr != nil {
				return nil, waitErr
			}
			continue
		}
		if response.Body == nil {
			cancel()
		} else {
			response.Body = &cancelOnClose{ReadCloser: response.Body, cancel: cancel}
		}
		return response, nil
	}
}

func newAttemptContext(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout > 0 {
		return context.WithTimeout(parent, timeout)
	}
	return parent, func() {}
}

type cancelOnClose struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (body *cancelOnClose) Close() error {
	err := body.ReadCloser.Close()
	body.cancel()
	return err
}

func retryableStatus(status int) bool {
	switch status {
	case http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

func retryAfterDelay(value string) (time.Duration, bool) {
	seconds, err := strconv.Atoi(value)
	if err != nil || seconds < 0 {
		return 0, false
	}
	return time.Duration(seconds) * time.Second, true
}

func jitteredBackoff(attempt int, random *rand.Rand) time.Duration {
	delay := retryBaseDelay * time.Duration(1<<attempt)
	factor := 0.8 + random.Float64()*0.4
	return time.Duration(float64(delay) * factor)
}

func waitForRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
