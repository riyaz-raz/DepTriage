package gemini

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/deptriage/deptriage/internal/llm"
	"github.com/deptriage/deptriage/internal/llm/adaptertest"
)

func TestGeminiAdapter(t *testing.T) {
	adaptertest.RunProviderTests(t,
		func(serverURL string) llm.Provider {
			return NewWithEndpoint("fixture-key", "test-model", serverURL, nil)
		},
		encodeResponse,
		func(t *testing.T, request *http.Request, body []byte) {
			t.Helper()
			if request.Method != http.MethodPost || request.URL.Path != "/v1beta/models/test-model:generateContent" {
				t.Errorf("request endpoint = %s %s", request.Method, request.URL.Path)
			}
			if request.Header.Get("x-goog-api-key") != "fixture-key" {
				t.Error("Gemini API key header is missing or incorrect")
			}
			if request.URL.RawQuery != "" {
				t.Error("Gemini API key must not be passed in the URL")
			}
			var payload struct {
				SystemInstruction struct {
					Parts []textPart `json:"parts"`
				} `json:"systemInstruction"`
				Contents []struct {
					Role  string     `json:"role"`
					Parts []textPart `json:"parts"`
				} `json:"contents"`
				GenerationConfig struct {
					ResponseMIMEType string          `json:"responseMimeType"`
					ResponseSchema   map[string]any  `json:"responseSchema"`
					Temperature      float64         `json:"temperature"`
					ThinkingConfig   *thinkingConfig `json:"thinkingConfig"`
				} `json:"generationConfig"`
			}
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Errorf("decode request body: %v", err)
				return
			}
			if payload.GenerationConfig.ResponseMIMEType != "application/json" || payload.GenerationConfig.Temperature != 0.25 {
				t.Errorf("generation config: %+v", payload.GenerationConfig)
			}
			if payload.SystemInstruction.Parts[0].Text != "system prompt" || len(payload.Contents) != 1 || payload.Contents[0].Role != "user" || !strings.Contains(payload.Contents[0].Parts[0].Text, "user prompt") {
				t.Error("Gemini system/user prompts were not serialized correctly")
			}
			if payload.GenerationConfig.ResponseSchema["type"] != "OBJECT" {
				t.Errorf("Gemini responseSchema type = %#v, want OBJECT", payload.GenerationConfig.ResponseSchema["type"])
			}
			properties, _ := payload.GenerationConfig.ResponseSchema["properties"].(map[string]any)
			verdict, _ := properties["verdict"].(map[string]any)
			if verdict["type"] != "STRING" {
				t.Errorf("Gemini verdict schema type = %#v, want STRING", verdict["type"])
			}
		},
	)
}

func encodeResponse(content string) string {
	encoded, _ := json.Marshal(content)
	return `{"candidates":[{"content":{"parts":[{"text":` + string(encoded) + `}]}}]}`
}
