package ollama

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/deptriage/deptriage/internal/llm"
)

const validOutput = `{"verdict":"review","confidence":0.8,"summary":"Check the update before merging.","reasons":["API changed"],"breaking_change_suspected":false,"security_relevant":false}`
const invalidOutput = `{"verdict":"unknown","confidence":1.5,"summary":"","reasons":[],"breaking_change_suspected":false,"security_relevant":false}`

func TestClassifyHappyPathAndRequestShape(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/chat" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		var request struct {
			Model    string `json:"model"`
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
			Format  json.RawMessage `json:"format"`
			Stream  bool            `json:"stream"`
			Options struct {
				Temperature float64 `json:"temperature"`
			} `json:"options"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if request.Model != "unit-model" || request.Stream || request.Options.Temperature != 0.2 {
			t.Errorf("request settings: %+v", request)
		}
		if len(request.Messages) != 2 || request.Messages[0].Role != "system" || request.Messages[1].Content != "user prompt" {
			t.Errorf("messages: %+v", request.Messages)
		}
		var schema map[string]any
		if err := json.Unmarshal(request.Format, &schema); err != nil || schema["type"] != "object" {
			t.Errorf("format schema invalid: %s", request.Format)
		}
		writeContent(w, validOutput)
	}))
	defer server.Close()

	verdict, err := New(server.URL, "default-model").Classify(context.Background(), llm.Request{System: "system prompt", User: "user prompt", Model: "unit-model", Temp: 0.2})
	if err != nil {
		t.Fatal(err)
	}
	if verdict.Verdict != "review" || verdict.Summary != "Check the update before merging." {
		t.Fatalf("verdict: %+v", verdict)
	}
}

func TestClassifyRepairAndDegradedFallback(t *testing.T) {
	t.Run("repair succeeds", func(t *testing.T) {
		calls := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			var request struct {
				Messages []struct {
					Content string `json:"content"`
				} `json:"messages"`
			}
			_ = json.NewDecoder(r.Body).Decode(&request)
			if calls == 2 && (!strings.Contains(request.Messages[1].Content, invalidOutput) || !strings.Contains(request.Messages[1].Content, "Return ONLY valid JSON")) {
				t.Errorf("repair user message missing prior output/instruction: %s", request.Messages[1].Content)
			}
			if calls == 1 {
				writeContent(w, invalidOutput)
			} else {
				writeContent(w, validOutput)
			}
		}))
		defer server.Close()
		verdict, err := New(server.URL, "model").Classify(context.Background(), llm.Request{System: "system", User: "user"})
		if err != nil || verdict.Verdict != "review" || calls != 2 {
			t.Fatalf("verdict=%+v err=%v calls=%d", verdict, err, calls)
		}
	})

	t.Run("invalid repair degrades without error", func(t *testing.T) {
		calls := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls++; writeContent(w, invalidOutput) }))
		defer server.Close()
		verdict, err := New(server.URL, "model").Classify(context.Background(), llm.Request{})
		if err != nil {
			t.Fatalf("Classify returned error: %v", err)
		}
		if !verdict.Degraded || verdict.Verdict != "review" || verdict.Confidence != 0 || verdict.Summary != "LLM output invalid; manual review required" || len(verdict.Reasons) != 1 || verdict.Reasons[0] != "parse_failure" || calls != 2 {
			t.Fatalf("degraded verdict: %+v calls=%d", verdict, calls)
		}
	})
}

func TestRetryOnServerError(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls == 1 {
			http.Error(w, "temporary", http.StatusInternalServerError)
			return
		}
		writeContent(w, validOutput)
	}))
	defer server.Close()
	verdict, err := New(server.URL, "model").Classify(context.Background(), llm.Request{})
	if err != nil || verdict.Verdict != "review" || calls != 2 {
		t.Fatalf("verdict=%+v err=%v calls=%d", verdict, err, calls)
	}
}

func TestRetryExhaustion(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		http.Error(w, "temporary", http.StatusInternalServerError)
	}))
	defer server.Close()
	_, err := New(server.URL, "model").Classify(context.Background(), llm.Request{})
	if err == nil || calls.Load() != 3 {
		t.Fatalf("err=%v calls=%d", err, calls.Load())
	}
}

func TestClassifyTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(100 * time.Millisecond)
		writeContent(w, validOutput)
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := New(server.URL, "model").Classify(ctx, llm.Request{})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want context deadline exceeded", err)
	}
}

func TestRetryAfterHeader(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls == 1 {
			w.Header().Set("Retry-After", "1")
			http.Error(w, "slow down", http.StatusTooManyRequests)
			return
		}
		writeContent(w, validOutput)
	}))
	defer server.Close()
	started := time.Now()
	verdict, err := New(server.URL, "model").Classify(context.Background(), llm.Request{})
	elapsed := time.Since(started)
	if err != nil || verdict.Verdict != "review" || calls != 2 {
		t.Fatalf("verdict=%+v err=%v calls=%d", verdict, err, calls)
	}
	if elapsed < 950*time.Millisecond {
		t.Fatalf("Retry-After not respected: waited %s", elapsed)
	}
}

func TestHostModelResolution(t *testing.T) {
	t.Setenv("OLLAMA_HOST", "http://override:1234/")
	client := New("http://config:11434/", "config-model")
	if client.host != "http://override:1234" || client.Model("") != "config-model" || client.Model("request-model") != "request-model" {
		t.Fatalf("resolution host=%q model=%q", client.host, client.Model(""))
	}
	t.Setenv("OLLAMA_HOST", "")
	if got := New("", ""); got.host != defaultHost || got.Model("") != defaultModel {
		t.Fatalf("default resolution: host=%q model=%q", got.host, got.Model(""))
	}
}

func writeContent(w http.ResponseWriter, content string) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = fmt.Fprintf(w, `{"message":{"content":%s}}`, mustJSON(content))
}

func mustJSON(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}
