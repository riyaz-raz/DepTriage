package llm

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestDoWithRetryGivesEachAttemptFreshTimeout(t *testing.T) {
	parent := WithRequestTimeout(context.Background(), 50*time.Millisecond)
	attempts := 0
	var secondBudget time.Duration
	started := time.Now()
	response, err := DoWithRetry(parent, func(attemptCtx context.Context) (*http.Response, error) {
		attempts++
		deadline, ok := attemptCtx.Deadline()
		if !ok {
			t.Fatal("attempt has no per-request deadline")
		}
		if attempts == 1 {
			<-attemptCtx.Done()
			return nil, attemptCtx.Err()
		}
		secondBudget = time.Until(deadline)
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("ok"))}, nil
	}, 1)
	elapsed := time.Since(started)
	if err != nil || response == nil || attempts != 2 {
		t.Fatalf("response=%v err=%v attempts=%d", response, err, attempts)
	}
	if secondBudget < 40*time.Millisecond {
		t.Fatalf("second attempt had only %s remaining, expected a fresh 50ms timeout", secondBudget)
	}
	if elapsed <= 50*time.Millisecond || elapsed >= 2*time.Second {
		t.Fatalf("elapsed=%s, expected first 50ms timeout plus the existing retry backoff", elapsed)
	}
	_ = response.Body.Close()
}
