// Package groq implements Groq's OpenAI-compatible chat completions API.
package groq

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/deptriage/deptriage/internal/llm"
	"github.com/deptriage/deptriage/internal/triage"
)

const (
	defaultEndpoint = "https://api.groq.com/openai/v1/chat/completions"
	defaultModel    = "llama-3.3-70b-versatile"
)

type chatRequest struct {
	Model          string        `json:"model"`
	Messages       []chatMessage `json:"messages"`
	ResponseFormat struct {
		Type string `json:"type"`
	} `json:"response_format"`
	Temperature float64 `json:"temperature"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
}

// Client calls the Groq-compatible chat completions API.
type Client struct {
	key        string
	model      string
	endpoint   string
	httpClient *http.Client
}

// New creates a Groq client with its configured model or provider default.
func New(key, model string) *Client {
	return NewWithEndpoint(key, model, defaultEndpoint, &http.Client{})
}

// NewWithEndpoint constructs a Groq client with an alternate endpoint and HTTP client.
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
	return &Client{key: key, model: model, endpoint: endpoint, httpClient: httpClient}
}

// Name returns the provider name.
func (*Client) Name() string { return "groq" }

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
		return triage.Verdict{}, fmt.Errorf("Groq API key is required")
	}
	model := c.Model(req.Model)
	content, err := c.complete(ctx, req, model)
	if err != nil {
		return triage.Verdict{}, err
	}
	verdict, validationErr := decodeVerdict(content)
	if validationErr == nil {
		return verdict, nil
	}
	repair := req
	repair.User = llm.RepairUserMessage(req.User, content, validationErr)
	content, err = c.complete(ctx, repair, model)
	if err != nil {
		return triage.Verdict{}, fmt.Errorf("request Groq verdict repair: %w", err)
	}
	verdict, validationErr = decodeVerdict(content)
	if validationErr != nil {
		return llm.DegradedVerdict(), nil
	}
	return verdict, nil
}

func (c *Client) complete(ctx context.Context, req llm.Request, model string) (string, error) {
	system, err := schemaSystem(req.System)
	if err != nil {
		return "", err
	}
	payload := chatRequest{Model: model, Messages: []chatMessage{{Role: "system", Content: system}, {Role: "user", Content: req.User}}, Temperature: req.Temp}
	payload.ResponseFormat.Type = "json_object"
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode Groq request: %w", err)
	}
	response, err := llm.DoWithRetry(ctx, func(attemptCtx context.Context) (*http.Response, error) {
		request, requestErr := http.NewRequestWithContext(attemptCtx, http.MethodPost, c.endpoint, bytes.NewReader(body))
		if requestErr != nil {
			return nil, fmt.Errorf("create Groq request: %w", requestErr)
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Authorization", "Bearer "+c.key)
		return c.httpClient.Do(request)
	}, 2)
	if err != nil {
		return "", fmt.Errorf("call Groq API: %w", err)
	}
	if response == nil {
		return "", fmt.Errorf("Groq API returned an empty response")
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		if len(message) > 0 {
			return "", fmt.Errorf("Groq API returned HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(message)))
		}
		return "", fmt.Errorf("Groq API returned HTTP %d", response.StatusCode)
	}
	var result chatResponse
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode Groq response: %w", err)
	}
	if len(result.Choices) == 0 {
		return "", fmt.Errorf("Groq response contained no choices")
	}
	return result.Choices[0].Message.Content, nil
}

func schemaSystem(system string) (string, error) {
	var compact bytes.Buffer
	if err := json.Compact(&compact, llm.VerdictSchema); err != nil {
		return "", fmt.Errorf("compact verdict schema: %w", err)
	}
	return system + "\n\nReturn ONLY a JSON object matching this exact schema. No prose. No markdown fences.\nSchema: " + compact.String(), nil
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
