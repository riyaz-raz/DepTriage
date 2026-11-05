// Package gemini implements Google's generateContent API provider.
package gemini

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/deptriage/deptriage/internal/llm"
	"github.com/deptriage/deptriage/internal/triage"
)

const (
	defaultEndpoint = "https://generativelanguage.googleapis.com"
	defaultModel    = "gemini-3.8-flash"
)

// Keep in sync with internal/llm/schema.go — Gemini uses an OpenAPI subset, not JSON Schema.
var geminiSchema = json.RawMessage(`{
  "type": "OBJECT",
  "properties": {
    "verdict": {"type": "STRING", "enum": ["safe", "review", "risk"]},
    "confidence": {"type": "NUMBER", "minimum": 0.0, "maximum": 1.0},
    "summary": {"type": "STRING", "minLength": 1, "maxLength": 200},
    "reasons": {"type": "ARRAY", "items": {"type": "STRING"}},
    "breaking_change_suspected": {"type": "BOOLEAN"},
    "security_relevant": {"type": "BOOLEAN"}
  },
  "required": ["verdict", "confidence", "summary", "reasons", "breaking_change_suspected", "security_relevant"]
}`)

type generateRequest struct {
	SystemInstruction struct {
		Parts []textPart `json:"parts"`
	} `json:"systemInstruction"`
	Contents []struct {
		Role  string     `json:"role"`
		Parts []textPart `json:"parts"`
	} `json:"contents"`
	GenerationConfig struct {
		ResponseMIMEType string          `json:"responseMimeType"`
		ResponseSchema   json.RawMessage `json:"responseSchema"`
		Temperature      float64         `json:"temperature"`
	} `json:"generationConfig"`
}

type textPart struct {
	Text string `json:"text"`
}

type generateResponse struct {
	Candidates []struct {
		Content struct {
			Parts []textPart `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
}

// Client calls the Gemini generateContent API.
type Client struct {
	key        string
	model      string
	endpoint   string
	httpClient *http.Client
}

// New creates a Gemini client with its configured model or provider default.
func New(key, model string) *Client {
	return NewWithEndpoint(key, model, defaultEndpoint, &http.Client{})
}

// NewWithEndpoint constructs a Gemini client with an alternate endpoint and HTTP client.
func NewWithEndpoint(key, model, endpoint string, httpClient *http.Client) *Client {
	if strings.TrimSpace(model) == "" {
		model = defaultModel
	}
	if strings.TrimSpace(endpoint) == "" {
		endpoint = defaultEndpoint
	}
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	return &Client{key: key, model: model, endpoint: strings.TrimRight(endpoint, "/"), httpClient: httpClient}
}

// Name returns the provider name.
func (*Client) Name() string { return "gemini" }

// Model resolves a request model against the configured/default model.
func (c *Client) Model(requestModel string) string {
	if strings.TrimSpace(requestModel) != "" {
		return strings.TrimSpace(requestModel)
	}
	return c.model
}

// Classify generates and validates a verdict, repairing invalid output once.
func (c *Client) Classify(ctx context.Context, req llm.Request) (triage.Verdict, error) {
	if strings.TrimSpace(c.key) == "" {
		return triage.Verdict{}, fmt.Errorf("Gemini API key is required")
	}
	model := c.Model(req.Model)
	content, err := c.generate(ctx, req, model)
	if err != nil {
		return triage.Verdict{}, err
	}
	verdict, validationErr := decodeVerdict(content)
	if validationErr == nil {
		return verdict, nil
	}
	repair := req
	repair.User = llm.RepairUserMessage(req.User, content, validationErr)
	content, err = c.generate(ctx, repair, model)
	if err != nil {
		return triage.Verdict{}, fmt.Errorf("request Gemini verdict repair: %w", err)
	}
	verdict, validationErr = decodeVerdict(content)
	if validationErr != nil {
		return llm.DegradedVerdict(), nil
	}
	return verdict, nil
}

func (c *Client) generate(ctx context.Context, req llm.Request, model string) (string, error) {
	payload := generateRequest{}
	payload.SystemInstruction.Parts = []textPart{{Text: req.System}}
	payload.Contents = []struct {
		Role  string     `json:"role"`
		Parts []textPart `json:"parts"`
	}{{Role: "user", Parts: []textPart{{Text: req.User}}}}
	payload.GenerationConfig.ResponseMIMEType = "application/json"
	payload.GenerationConfig.ResponseSchema = geminiSchema
	payload.GenerationConfig.Temperature = req.Temp
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode Gemini request: %w", err)
	}
	endpoint := c.endpoint + "/v1beta/models/" + url.PathEscape(model) + ":generateContent"
	response, err := llm.DoWithRetry(ctx, func() (*http.Response, error) {
		request, requestErr := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
		if requestErr != nil {
			return nil, fmt.Errorf("create Gemini request: %w", requestErr)
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("x-goog-api-key", c.key)
		return c.httpClient.Do(request)
	}, 2)
	if err != nil {
		return "", fmt.Errorf("call Gemini API: %w", err)
	}
	if response == nil {
		return "", fmt.Errorf("Gemini API returned an empty response")
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		if len(message) > 0 {
			return "", fmt.Errorf("Gemini API returned HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(message)))
		}
		return "", fmt.Errorf("Gemini API returned HTTP %d", response.StatusCode)
	}
	var result generateResponse
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode Gemini response: %w", err)
	}
	if len(result.Candidates) == 0 || len(result.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("Gemini response contained no candidate text")
	}
	return result.Candidates[0].Content.Parts[0].Text, nil
}

func decodeVerdict(content string) (triage.Verdict, error) {
	var verdict triage.Verdict
	if err := json.Unmarshal([]byte(content), &verdict); err != nil {
		return triage.Verdict{}, fmt.Errorf("decode verdict JSON: %w", err)
	}
	if err := llm.ValidateVerdict(verdict); err != nil {
		return triage.Verdict{}, err
	}
	return verdict, nil
}
