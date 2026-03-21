// Package openrouter provides an HTTP client for the OpenRouter chat completions API.
package openrouter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/slb350/froggr/internal/ai"
)

const (
	defaultEndpoint    = "https://openrouter.ai/api/v1/chat/completions"
	defaultHTTPTimeout = 120 * time.Second
	// maxResponseBytes prevents a runaway response from exhausting memory.
	// 2 MB is well above any reasonable code review response.
	maxResponseBytes = 2 * 1024 * 1024
)

// Client is a minimal OpenRouter chat completion client.
type Client struct {
	apiKey     string
	endpoint   string
	httpClient *http.Client
}

// NewClient creates an OpenRouter API client.
func NewClient(apiKey string) *Client {
	return &Client{
		apiKey:     apiKey,
		endpoint:   defaultEndpoint,
		httpClient: &http.Client{Timeout: defaultHTTPTimeout},
	}
}

// setEndpoint overrides the API endpoint (for testing).
func (c *Client) setEndpoint(url string) { c.endpoint = url }

// wireMessage is the OpenAI-compatible message format for the wire.
type wireMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// chatCompletionRequest is the OpenAI-compatible wire format.
type chatCompletionRequest struct {
	Model    string        `json:"model"`
	Messages []wireMessage `json:"messages"`
}

// chatCompletionResponse is the OpenAI-compatible response format.
type chatCompletionResponse struct {
	Choices []choice  `json:"choices"`
	Error   *apiError `json:"error,omitempty"`
}

type choice struct {
	Message wireMessage `json:"message"`
}

type apiError struct {
	Message string       `json:"message"`
	Code    apiErrorCode `json:"code,omitempty"`
}

// apiErrorCode handles error codes that may be strings or numbers.
// OpenRouter aggregates many providers, and the error code format varies —
// some return numeric HTTP codes, others return string identifiers.
type apiErrorCode string

// UnmarshalJSON accepts both string ("rate_limited") and numeric (429) codes.
func (c *apiErrorCode) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		*c = ""
		return nil
	}

	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		*c = apiErrorCode(s)
		return nil
	}

	var n json.Number
	if err := json.Unmarshal(data, &n); err == nil {
		*c = apiErrorCode(n.String())
		return nil
	}

	*c = apiErrorCode(string(data))
	return nil
}

// Complete sends a chat completion request and returns the response content.
func (c *Client) Complete(ctx context.Context, req ai.CompletionRequest) (string, error) {
	if c.apiKey == "" {
		return "", fmt.Errorf("OpenRouter API key is empty (set OPENROUTER_API_KEY)")
	}
	if err := req.Validate(); err != nil {
		return "", fmt.Errorf("OpenRouter: %w", err)
	}

	respBody, err := c.doRequest(ctx, req)
	if err != nil {
		return "", err
	}

	return extractContent(respBody)
}

// doRequest builds, sends, and reads the HTTP request/response.
func (c *Client) doRequest(ctx context.Context, req ai.CompletionRequest) ([]byte, error) {
	msgs := make([]wireMessage, len(req.Messages))
	for i, m := range req.Messages {
		msgs[i] = wireMessage{Role: string(m.Role), Content: m.Content}
	}
	reqJSON, err := json.Marshal(chatCompletionRequest{Model: req.Model, Messages: msgs})
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(reqJSON))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("HTTP-Referer", "https://github.com/slb350/froggr")
	httpReq.Header.Set("X-Title", "froggr")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}
	if len(respBody) > maxResponseBytes {
		return nil, fmt.Errorf("OpenRouter response too large (exceeded %d bytes)", maxResponseBytes)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, formatHTTPError(resp.StatusCode, respBody)
	}

	return respBody, nil
}

// extractContent parses the response body and returns the assistant's content.
func extractContent(respBody []byte) (string, error) {
	var chatResp chatCompletionResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return "", fmt.Errorf("parsing OpenRouter response: %w", err)
	}

	if chatResp.Error != nil && chatResp.Error.Message != "" {
		return "", fmt.Errorf("OpenRouter API: %s", chatResp.Error.Message)
	}

	if len(chatResp.Choices) == 0 {
		return "", fmt.Errorf("OpenRouter returned no choices")
	}

	content := strings.TrimSpace(chatResp.Choices[0].Message.Content)
	if content == "" {
		return "", fmt.Errorf("OpenRouter returned empty content")
	}

	return content, nil
}

func formatHTTPError(statusCode int, respBody []byte) error {
	detail := extractErrorDetail(respBody)
	switch statusCode {
	case http.StatusUnauthorized:
		if detail != "" {
			return fmt.Errorf("OpenRouter auth failed (401): %s (check OPENROUTER_API_KEY)", detail)
		}
		return fmt.Errorf("OpenRouter auth failed (401): check your API key (set OPENROUTER_API_KEY)")
	case http.StatusTooManyRequests:
		if detail != "" {
			return fmt.Errorf("OpenRouter rate limit exceeded (429): %s", detail)
		}
		return fmt.Errorf("OpenRouter rate limit exceeded (429): please wait and try again")
	default:
		if detail != "" {
			return fmt.Errorf("OpenRouter API error (status %d): %s", statusCode, detail)
		}
		if len(respBody) > 0 {
			b := respBody
			if len(b) > 200 {
				b = b[:200]
			}
			return fmt.Errorf("OpenRouter API error (status %d): %s", statusCode, strings.TrimSpace(string(b)))
		}
		return fmt.Errorf("OpenRouter API error (status %d)", statusCode)
	}
}

func extractErrorDetail(body []byte) string {
	var errResp struct {
		Error *apiError `json:"error"`
	}
	if err := json.Unmarshal(body, &errResp); err != nil || errResp.Error == nil {
		return ""
	}
	return strings.TrimSpace(errResp.Error.Message)
}
