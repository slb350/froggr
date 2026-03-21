// Package openrouter provides an HTTP client for the OpenRouter chat completions API.
package openrouter

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/slb350/froggr/internal/ai"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestClient(t *testing.T, handler http.HandlerFunc) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	c := NewClient("sk-or-v1-test")
	c.httpClient = srv.Client()
	c.setEndpoint(srv.URL)
	return c, srv
}

func TestComplete_Success(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(chatCompletionResponse{
			Choices: []choice{
				{Message: wireMessage{Role: "assistant", Content: "Review looks clean."}},
			},
		})
	})

	result, err := c.Complete(context.Background(), ai.CompletionRequest{
		Model:    "anthropic/claude-sonnet-4",
		Messages: []ai.Message{{Role: ai.RoleSystem, Content: "sys"}, {Role: ai.RoleUser, Content: "usr"}},
	})
	require.NoError(t, err)
	assert.Equal(t, "Review looks clean.", result)
}

func TestComplete_VerifiesHeaders(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer sk-or-v1-test", r.Header.Get("Authorization"))
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		assert.NotEmpty(t, r.Header.Get("HTTP-Referer"))
		assert.NotEmpty(t, r.Header.Get("X-Title"))

		_ = json.NewEncoder(w).Encode(chatCompletionResponse{
			Choices: []choice{{Message: wireMessage{Content: "ok"}}},
		})
	})

	_, err := c.Complete(context.Background(), ai.CompletionRequest{
		Model:    "model",
		Messages: []ai.Message{{Role: ai.RoleUser, Content: "test"}},
	})
	require.NoError(t, err)
}

func TestComplete_VerifiesBody(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		var req chatCompletionRequest
		err := json.NewDecoder(r.Body).Decode(&req)
		require.NoError(t, err)

		assert.Equal(t, "anthropic/claude-sonnet-4", req.Model)
		require.Len(t, req.Messages, 2)
		assert.Equal(t, "system", req.Messages[0].Role)
		assert.Equal(t, "Review this code", req.Messages[0].Content)
		assert.Equal(t, "user", req.Messages[1].Role)
		assert.Equal(t, "diff here", req.Messages[1].Content)

		_ = json.NewEncoder(w).Encode(chatCompletionResponse{
			Choices: []choice{{Message: wireMessage{Content: "ok"}}},
		})
	})

	_, err := c.Complete(context.Background(), ai.CompletionRequest{
		Model: "anthropic/claude-sonnet-4",
		Messages: []ai.Message{
			{Role: ai.RoleSystem, Content: "Review this code"},
			{Role: ai.RoleUser, Content: "diff here"},
		},
	})
	require.NoError(t, err)
}

func TestComplete_EmptyAPIKey(t *testing.T) {
	c := NewClient("")
	_, err := c.Complete(context.Background(), ai.CompletionRequest{
		Model:    "model",
		Messages: []ai.Message{{Role: ai.RoleUser, Content: "test"}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "API key")
}

func TestComplete_AuthError401(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]string{"message": "invalid api key"},
		})
	})

	_, err := c.Complete(context.Background(), ai.CompletionRequest{
		Model:    "model",
		Messages: []ai.Message{{Role: ai.RoleUser, Content: "test"}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "401")
	assert.Contains(t, err.Error(), "invalid api key")
}

func TestComplete_RateLimit429(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]string{"message": "rate limit exceeded, retry after 30s"},
		})
	})

	_, err := c.Complete(context.Background(), ai.CompletionRequest{
		Model:    "model",
		Messages: []ai.Message{{Role: ai.RoleUser, Content: "test"}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "429")
	assert.Contains(t, err.Error(), "retry after 30s")
}

func TestComplete_ServerError500(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]string{"message": "model overloaded"},
		})
	})

	_, err := c.Complete(context.Background(), ai.CompletionRequest{
		Model:    "model",
		Messages: []ai.Message{{Role: ai.RoleUser, Content: "test"}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "500")
	assert.Contains(t, err.Error(), "model overloaded")
}

func TestComplete_MalformedResponse(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`not json`))
	})

	_, err := c.Complete(context.Background(), ai.CompletionRequest{
		Model:    "model",
		Messages: []ai.Message{{Role: ai.RoleUser, Content: "test"}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parsing")
}

func TestComplete_EmptyChoices(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(chatCompletionResponse{Choices: []choice{}})
	})

	_, err := c.Complete(context.Background(), ai.CompletionRequest{
		Model:    "model",
		Messages: []ai.Message{{Role: ai.RoleUser, Content: "test"}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no choices")
}

func TestComplete_EmptyContent(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(chatCompletionResponse{
			Choices: []choice{{Message: wireMessage{Role: "assistant", Content: ""}}},
		})
	})

	_, err := c.Complete(context.Background(), ai.CompletionRequest{
		Model:    "model",
		Messages: []ai.Message{{Role: ai.RoleUser, Content: "test"}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty content")
}

func TestComplete_ContextCancellation(t *testing.T) {
	c, _ := newTestClient(t, func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := c.Complete(ctx, ai.CompletionRequest{
		Model:    "model",
		Messages: []ai.Message{{Role: ai.RoleUser, Content: "test"}},
	})
	require.Error(t, err)
}

func TestComplete_ResponseTooLarge(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(make([]byte, 2*1024*1024+100))
	})

	_, err := c.Complete(context.Background(), ai.CompletionRequest{
		Model:    "model",
		Messages: []ai.Message{{Role: ai.RoleUser, Content: "test"}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "too large")
}

func TestComplete_APIErrorInBody(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(chatCompletionResponse{
			Choices: nil,
			Error:   &apiError{Message: "model not found", Code: "model_not_found"},
		})
	})

	_, err := c.Complete(context.Background(), ai.CompletionRequest{
		Model:    "nonexistent/model",
		Messages: []ai.Message{{Role: ai.RoleUser, Content: "test"}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "model not found")
}

func TestAPIErrorCode_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  apiErrorCode
	}{
		{"string_code", `"model_not_found"`, "model_not_found"},
		{"numeric_code", `429`, "429"},
		{"null_code", `null`, ""},
		{"empty", ``, ""},
		{"unexpected_bool", `true`, "true"},
		{"unexpected_object", `{"type":"internal"}`, `{"type":"internal"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got apiErrorCode
			err := got.UnmarshalJSON([]byte(tt.input))
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestComplete_ServerError_NoBody(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	})

	_, err := c.Complete(context.Background(), ai.CompletionRequest{
		Model:    "model",
		Messages: []ai.Message{{Role: ai.RoleUser, Content: "test"}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "502")
}

func TestComplete_ServerError_HTMLBody(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("<html><body>Service Temporarily Unavailable</body></html>"))
	})

	_, err := c.Complete(context.Background(), ai.CompletionRequest{
		Model:    "model",
		Messages: []ai.Message{{Role: ai.RoleUser, Content: "test"}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "502")
	assert.Contains(t, err.Error(), "Service Temporarily Unavailable")
}

func TestComplete_TrimsWhitespace(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(chatCompletionResponse{
			Choices: []choice{{Message: wireMessage{Content: "  result with whitespace  \n"}}},
		})
	})

	result, err := c.Complete(context.Background(), ai.CompletionRequest{
		Model:    "model",
		Messages: []ai.Message{{Role: ai.RoleUser, Content: "test"}},
	})
	require.NoError(t, err)
	assert.False(t, strings.HasPrefix(result, " "))
	assert.False(t, strings.HasSuffix(result, "\n"))
}
