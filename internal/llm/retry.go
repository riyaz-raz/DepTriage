package llm

import (
	"context"
	"math/rand"
	"net/http"
	"strconv"
	"time"
)

const retryBaseDelay = 500 * time.Millisecond

// DoWithRetry performs an HTTP operation with retries for transient failures.
// maxRetries is additional attempts after the initial request.
func DoWithRetry(ctx context.Context, fn func() (*http.Response, error), maxRetries int) (*http.Response, error) {
	if maxRetries < 0 {
		maxRetries = 0
	}
	random := rand.New(rand.NewSource(time.Now().UnixNano()))
	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		response, err := fn()
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if attempt >= maxRetries {
				return nil, err
			}
			if waitErr := waitForRetry(ctx, jitteredBackoff(attempt, random)); waitErr != nil {
				return nil, waitErr
			}
			continue
		}
		if response == nil || !retryableStatus(response.StatusCode) || attempt >= maxRetries {
			return response, nil
		}
		delay := jitteredBackoff(attempt, random)
		if retryAfter, ok := retryAfterDelay(response.Header.Get("Retry-After")); ok {
			delay = retryAfter
		}
		if response.Body != nil {
			_ = response.Body.Close()
		}
		if waitErr := waitForRetry(ctx, delay); waitErr != nil {
			return nil, waitErr
		}
	}
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
