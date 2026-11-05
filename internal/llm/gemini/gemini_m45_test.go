package gemini

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/deptriage/deptriage/internal/llm"
)

func TestThinkingConfigCompatibility(t *testing.T) {
	cases := []struct {
		model        string
		wantThinking bool
	}{
		{"gemini-3.8-flash", true},
		{"gemini-1.5-pro", false},
	}
	for _, tc := range cases {
		t.Run(tc.model, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var payload struct {
					GenerationConfig map[string]json.RawMessage `json:"generationConfig"`
				}
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Errorf("decode request: %v", err)
				}
				_, found := payload.GenerationConfig["thinkingConfig"]
				if found != tc.wantThinking {
					t.Errorf("thinkingConfig present=%t, want %t", found, tc.wantThinking)
				}
				writeGeminiResponse(w, `{"verdict":"safe","confidence":0.9,"summary":"ok","reasons":[],"breaking_change_suspected":false,"security_relevant":false}`)
			}))
			defer server.Close()
			client := NewWithEndpoint("key", tc.model, server.URL, nil)
			if _, err := client.Classify(context.Background(), llm.Request{System: "system", User: "user"}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestThinkingConfig400Fallback(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			GenerationConfig map[string]json.RawMessage `json:"generationConfig"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode request: %v", err)
		}
		call := calls.Add(1)
		_, hasThinking := payload.GenerationConfig["thinkingConfig"]
		if call == 1 && !hasThinking {
			t.Error("first request did not contain thinkingConfig")
		}
		if call == 2 && hasThinking {
			t.Error("fallback request retained thinkingConfig")
		}
		if call == 1 {
			http.Error(w, `{"error":{"message":"thinkingConfig is not supported"}}`, http.StatusBadRequest)
			return
		}
		writeGeminiResponse(w, `{"verdict":"safe","confidence":0.9,"summary":"ok","reasons":[],"breaking_change_suspected":false,"security_relevant":false}`)
	}))
	defer server.Close()
	client := NewWithEndpoint("key", "gemini-3.8-flash", server.URL, nil)
	verdict, err := client.Classify(context.Background(), llm.Request{System: "system", User: "user"})
	if err != nil || verdict.Verdict != "safe" || calls.Load() != 2 {
		t.Fatalf("verdict=%+v err=%v calls=%d", verdict, err, calls.Load())
	}
}

func TestDeprecatedModelSuggestion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":{"message":"Please update your code to use models/gemini-4.0-flash..."}}`, http.StatusNotFound)
	}))
	defer server.Close()
	client := NewWithEndpoint("key", "gemini-2.0-flash", server.URL, nil)
	_, err := client.Classify(context.Background(), llm.Request{Model: "gemini-2.0-flash"})
	if err == nil || !strings.Contains(err.Error(), `gemini-4.0-flash`) || !strings.Contains(err.Error(), "--model gemini-4.0-flash") {
		t.Fatalf("error = %v, want suggested model and --model hint", err)
	}
}

func writeGeminiResponse(w http.ResponseWriter, content string) {
	w.Header().Set("Content-Type", "application/json")
	encoded, _ := json.Marshal(content)
	_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":` + string(encoded) + `}]}}]}`))
}
