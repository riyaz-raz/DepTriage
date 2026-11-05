// Package adaptertest contains reusable httptest coverage for cloud LLM adapters.
package adaptertest

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/deptriage/deptriage/internal/llm"
)

// ProviderFactory creates a provider against a test server URL.
type ProviderFactory func(serverURL string) llm.Provider

// ResponseEncoder wraps model output in a provider-specific response fixture.
type ResponseEncoder func(content string) string

// RequestInspector validates provider-specific request details.
type RequestInspector func(t *testing.T, request *http.Request, body []byte)

// RunProviderTests runs shared adapter behavior tests against an httptest.Server.
func RunProviderTests(t *testing.T, newProvider ProviderFactory, response ResponseEncoder, inspect RequestInspector) {
	t.Helper()
	request := llm.Request{System: "system prompt", User: "user prompt", Model: "test-model", Temp: 0.25}

	t.Run("happy path", func(t *testing.T) {
		server := httptest.NewServer(handler(t, response, inspect, func(int) (int, string) { return http.StatusOK, response(validVerdict) }))
		defer server.Close()
		verdict, err := newProvider(server.URL).Classify(context.Background(), request)
		if err != nil || verdict.Verdict != "safe" || verdict.Summary != "valid result" {
			t.Fatalf("verdict=%+v err=%v", verdict, err)
		}
	})

	t.Run("malformed output repaired", func(t *testing.T) {
		var calls atomic.Int32
		server := httptest.NewServer(handler(t, response, inspect, func(call int) (int, string) {
			calls.Add(1)
			if call == 1 {
				return http.StatusOK, response("not-json")
			}
			return http.StatusOK, response(validVerdict)
		}))
		defer server.Close()
		verdict, err := newProvider(server.URL).Classify(context.Background(), request)
		if err != nil || verdict.Verdict != "safe" || calls.Load() != 2 {
			t.Fatalf("verdict=%+v err=%v calls=%d", verdict, err, calls.Load())
		}
	})

	t.Run("invalid repair degrades", func(t *testing.T) {
		var calls atomic.Int32
		server := httptest.NewServer(handler(t, response, inspect, func(int) (int, string) { calls.Add(1); return http.StatusOK, response("{") }))
		defer server.Close()
		verdict, err := newProvider(server.URL).Classify(context.Background(), request)
		if err != nil || !verdict.Degraded || verdict.Verdict != "review" || verdict.Confidence != 0 || len(verdict.Reasons) != 1 || verdict.Reasons[0] != "parse_failure" || calls.Load() != 2 {
			t.Fatalf("verdict=%+v err=%v calls=%d", verdict, err, calls.Load())
		}
	})

	t.Run("429 Retry-After", func(t *testing.T) {
		var calls atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, err := readRequest(r)
			if err != nil {
				t.Errorf("read request: %v", err)
			}
			if inspect != nil {
				inspect(t, r, body)
			}
			if calls.Add(1) == 1 {
				w.Header().Set("Retry-After", "1")
				http.Error(w, "rate limited", http.StatusTooManyRequests)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(w, response(validVerdict))
		}))
		defer server.Close()
		started := time.Now()
		verdict, err := newProvider(server.URL).Classify(context.Background(), request)
		if err != nil || verdict.Verdict != "safe" || calls.Load() != 2 {
			t.Fatalf("verdict=%+v err=%v calls=%d", verdict, err, calls.Load())
		}
		if time.Since(started) < 950*time.Millisecond {
			t.Fatal("Retry-After delay was not observed")
		}
	})

	t.Run("500 retries twice then succeeds", func(t *testing.T) {
		var calls atomic.Int32
		server := httptest.NewServer(handler(t, response, inspect, func(call int) (int, string) {
			calls.Add(1)
			if call < 3 {
				return http.StatusInternalServerError, "temporary"
			}
			return http.StatusOK, response(validVerdict)
		}))
		defer server.Close()
		verdict, err := newProvider(server.URL).Classify(context.Background(), request)
		if err != nil || verdict.Verdict != "safe" || calls.Load() != 3 {
			t.Fatalf("verdict=%+v err=%v calls=%d", verdict, err, calls.Load())
		}
	})

	t.Run("500 retries exhausted", func(t *testing.T) {
		var calls atomic.Int32
		server := httptest.NewServer(handler(t, response, inspect, func(int) (int, string) { calls.Add(1); return http.StatusInternalServerError, "temporary" }))
		defer server.Close()
		_, err := newProvider(server.URL).Classify(context.Background(), request)
		if err == nil || calls.Load() != 3 {
			t.Fatalf("err=%v calls=%d", err, calls.Load())
		}
	})

	t.Run("context timeout", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			time.Sleep(100 * time.Millisecond)
			_, _ = fmt.Fprint(w, response(validVerdict))
		}))
		defer server.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()
		_, err := newProvider(server.URL).Classify(ctx, request)
		if !strings.Contains(fmt.Sprint(err), context.DeadlineExceeded.Error()) {
			t.Fatalf("error=%v, want deadline exceeded", err)
		}
	})
}

const validVerdict = `{"verdict":"safe","confidence":0.9,"summary":"valid result","reasons":["fixture"],"breaking_change_suspected":false,"security_relevant":false}`

func handler(t *testing.T, encode ResponseEncoder, inspect RequestInspector, result func(call int) (int, string)) http.Handler {
	t.Helper()
	var calls atomic.Int32
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := readRequest(r)
		if err != nil {
			t.Errorf("read request: %v", err)
		}
		if inspect != nil {
			inspect(t, r, body)
		}
		status, content := result(int(calls.Add(1)))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if content != "" {
			_, _ = fmt.Fprint(w, content)
		}
	})
}

func readRequest(r *http.Request) ([]byte, error) {
	defer r.Body.Close()
	return io.ReadAll(r.Body)
}
