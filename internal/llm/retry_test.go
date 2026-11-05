package llm

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestDoWithRetry(t *testing.T) {
	t.Run("retries server error", func(t *testing.T) {
		calls := 0
		response, err := DoWithRetry(context.Background(), func(context.Context) (*http.Response, error) {
			calls++
			status := http.StatusOK
			if calls == 1 {
				status = http.StatusInternalServerError
			}
			return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(""))}, nil
		}, 2)
		if err != nil || response.StatusCode != http.StatusOK || calls != 2 {
			t.Fatalf("response=%v err=%v calls=%d", response, err, calls)
		}
	})
	t.Run("does not retry authentication", func(t *testing.T) {
		calls := 0
		response, err := DoWithRetry(context.Background(), func(context.Context) (*http.Response, error) {
			calls++
			return &http.Response{StatusCode: http.StatusUnauthorized, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(""))}, nil
		}, 2)
		if err != nil || response.StatusCode != http.StatusUnauthorized || calls != 1 {
			t.Fatalf("response=%v err=%v calls=%d", response, err, calls)
		}
	})
}
