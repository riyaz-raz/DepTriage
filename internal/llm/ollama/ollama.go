// Package ollama implements structured-output classification through Ollama's chat API.
package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/deptriage/deptriage/internal/llm"
	"github.com/deptriage/deptriage/internal/triage"
)

const (
	defaultHost  = "http://localhost:11434"
	defaultModel = "llama3.2:1b"
)

type chatRequest struct {
	Model    string          `json:"model"`
	Messages []chatMessage   `json:"messages"`
	Format   json.RawMessage `json:"format"`
	Stream   bool            `json:"stream"`
	Options  chatOptions     `json:"options"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatOptions struct {
	Temperature float64 `json:"temperature"`
}

type chatResponse struct {
	Message struct {
		Content string `json:"content"`
	} `json:"message"`
}

// Client classifies pull requests using an Ollama server.
type Client struct {
	host       string
	model      string
	httpClient *http.Client
}

// New creates an Ollama client. OLLAMA_HOST takes precedence over host;
// defaults are used when either setting is empty.
func New(host, model string) *Client {
	if envHost := strings.TrimSpace(os.Getenv("OLLAMA_HOST")); envHost != "" {
		host = envHost
	}
	if strings.TrimSpace(host) == "" {
		host = defaultHost
	}
	if strings.TrimSpace(model) == "" {
		model = defaultModel
	}
	return &Client{host: strings.TrimRight(host, "/"), model: model, httpClient: &http.Client{}}
}

// Name returns the provider name.
func (*Client) Name() string { return "ollama" }

// Model resolves the request model against the configured client default.
func (c *Client) Model(requestModel string) string {
	if strings.TrimSpace(requestModel) != "" {
		return strings.TrimSpace(requestModel)
	}
	return c.model
}

// Classify requests and validates a structured verdict, making one repair attempt if needed.
func (c *Client) Classify(ctx context.Context, req llm.Request) (triage.Verdict, error) {
	model := c.Model(req.Model)
	output, err := c.request(ctx, req, model)
	if err != nil {
		return triage.Verdict{}, err
	}
	verdict, validationErr := decodeVerdict(output)
	if validationErr == nil {
		return verdict, nil
	}

	repairReq := req
	repairReq.User = llm.RepairUserMessage(req.User, output, validationErr)
	repairedOutput, err := c.request(ctx, repairReq, model)
	if err != nil {
		return triage.Verdict{}, fmt.Errorf("request verdict repair: %w", err)
	}
	verdict, validationErr = decodeVerdict(repairedOutput)
	if validationErr != nil {
		return llm.DegradedVerdict(), nil
	}
	return verdict, nil
}

func (c *Client) request(ctx context.Context, req llm.Request, model string) (string, error) {
	payload := chatRequest{
		Model:    model,
		Messages: []chatMessage{{Role: "system", Content: req.System}, {Role: "user", Content: req.User}},
		Format:   llm.VerdictSchema,
		Stream:   false,
		Options:  chatOptions{Temperature: req.Temp},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode Ollama request: %w", err)
	}
	endpoint := c.host + "/api/chat"
	response, err := llm.DoWithRetry(ctx, func() (*http.Response, error) {
		request, requestErr := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
		if requestErr != nil {
			return nil, fmt.Errorf("create Ollama request: %w", requestErr)
		}
		request.Header.Set("Content-Type", "application/json")
		return c.httpClient.Do(request)
	}, 2)
	if err != nil {
		return "", fmt.Errorf("call Ollama chat API: %w", err)
	}
	if response == nil {
		return "", fmt.Errorf("Ollama chat API returned an empty response")
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		if len(message) > 0 {
			return "", fmt.Errorf("Ollama chat API returned HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(message)))
		}
		return "", fmt.Errorf("Ollama chat API returned HTTP %d", response.StatusCode)
	}
	var result chatResponse
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode Ollama response: %w", err)
	}
	return result.Message.Content, nil
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
