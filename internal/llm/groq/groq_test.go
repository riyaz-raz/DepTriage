package groq

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/deptriage/deptriage/internal/llm"
	"github.com/deptriage/deptriage/internal/llm/adaptertest"
)

func TestGroqAdapter(t *testing.T) {
	adaptertest.RunProviderTests(t,
		func(serverURL string) llm.Provider {
			return NewWithEndpoint("fixture-key", "test-model", serverURL+"/openai/v1/chat/completions", nil)
		},
		encodeResponse,
		func(t *testing.T, request *http.Request, body []byte) {
			t.Helper()
			if request.Method != http.MethodPost || request.URL.Path != "/openai/v1/chat/completions" {
				t.Errorf("request endpoint = %s %s", request.Method, request.URL.Path)
			}
			if request.Header.Get("Authorization") != "Bearer fixture-key" {
				t.Error("Groq bearer authorization header is missing or incorrect")
			}
			var payload struct {
				Model    string `json:"model"`
				Messages []struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				} `json:"messages"`
				ResponseFormat struct {
					Type string `json:"type"`
				} `json:"response_format"`
				Temperature float64 `json:"temperature"`
			}
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Errorf("decode request: %v", err)
				return
			}
			if payload.Model != "test-model" || payload.ResponseFormat.Type != "json_object" || payload.Temperature != 0.25 {
				t.Errorf("request settings: %+v", payload)
			}
			if len(payload.Messages) != 2 || payload.Messages[0].Role != "system" || !strings.Contains(payload.Messages[1].Content, "user prompt") {
				t.Errorf("messages: %+v", payload.Messages)
			}
			if !strings.Contains(payload.Messages[0].Content, "Return ONLY a JSON object matching this exact schema") || !strings.Contains(payload.Messages[0].Content, `"verdict"`) {
				t.Error("schema instruction missing from system message")
			}
		},
	)
}

func encodeResponse(content string) string {
	encoded, _ := json.Marshal(content)
	return `{"choices":[{"message":{"content":` + string(encoded) + `}}]}`
}
