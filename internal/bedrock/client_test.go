package bedrock

import (
	"context"
	"fmt"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	"github.com/slb350/froggr/internal/ai"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockConverseAPI implements converseAPI for testing.
type mockConverseAPI struct {
	output *bedrockruntime.ConverseOutput
	err    error
	input  *bedrockruntime.ConverseInput // captured for assertions
}

func (m *mockConverseAPI) Converse(ctx context.Context, input *bedrockruntime.ConverseInput, _ ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseOutput, error) {
	m.input = input
	return m.output, m.err
}

func converseOutput(text string) *bedrockruntime.ConverseOutput {
	return &bedrockruntime.ConverseOutput{
		Output: &types.ConverseOutputMemberMessage{
			Value: types.Message{
				Role: "assistant",
				Content: []types.ContentBlock{
					&types.ContentBlockMemberText{Value: text},
				},
			},
		},
	}
}

func TestComplete_Success(t *testing.T) {
	mock := &mockConverseAPI{output: converseOutput("Review looks clean.")}
	c := newClientWithAPI(mock)

	result, err := c.Complete(context.Background(), ai.CompletionRequest{
		Model:    "anthropic.claude-sonnet-4-6",
		Messages: []ai.Message{{Role: ai.RoleUser, Content: "review this code"}},
	})
	require.NoError(t, err)
	assert.Equal(t, "Review looks clean.", result)
	assert.Equal(t, "anthropic.claude-sonnet-4-6", aws.ToString(mock.input.ModelId))
}

func TestComplete_SystemMessageSeparation(t *testing.T) {
	mock := &mockConverseAPI{output: converseOutput("ok")}
	c := newClientWithAPI(mock)

	_, err := c.Complete(context.Background(), ai.CompletionRequest{
		Model: "anthropic.claude-sonnet-4-6",
		Messages: []ai.Message{
			{Role: ai.RoleSystem, Content: "You are a code reviewer."},
			{Role: ai.RoleUser, Content: "review this"},
		},
	})
	require.NoError(t, err)

	// System message should be in the System field, not in Messages.
	require.Len(t, mock.input.System, 1)
	sysBlock, ok := mock.input.System[0].(*types.SystemContentBlockMemberText)
	require.True(t, ok)
	assert.Equal(t, "You are a code reviewer.", sysBlock.Value)

	// Only the user message should be in Messages.
	require.Len(t, mock.input.Messages, 1)
	assert.Equal(t, "user", string(mock.input.Messages[0].Role))
}

func TestComplete_EmptyResponse(t *testing.T) {
	mock := &mockConverseAPI{
		output: &bedrockruntime.ConverseOutput{
			Output: &types.ConverseOutputMemberMessage{
				Value: types.Message{
					Role:    "assistant",
					Content: []types.ContentBlock{},
				},
			},
		},
	}
	c := newClientWithAPI(mock)

	_, err := c.Complete(context.Background(), ai.CompletionRequest{
		Model:    "anthropic.claude-sonnet-4-6",
		Messages: []ai.Message{{Role: ai.RoleUser, Content: "test"}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no text content")
}

func TestComplete_ErrorPropagation(t *testing.T) {
	mock := &mockConverseAPI{err: fmt.Errorf("throttling exception")}
	c := newClientWithAPI(mock)

	_, err := c.Complete(context.Background(), ai.CompletionRequest{
		Model:    "anthropic.claude-sonnet-4-6",
		Messages: []ai.Message{{Role: ai.RoleUser, Content: "test"}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "throttling exception")
}

func TestComplete_ContextCancellation(t *testing.T) {
	mock := &mockConverseAPI{err: context.Canceled}
	c := newClientWithAPI(mock)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := c.Complete(ctx, ai.CompletionRequest{
		Model:    "anthropic.claude-sonnet-4-6",
		Messages: []ai.Message{{Role: ai.RoleUser, Content: "test"}},
	})
	require.Error(t, err)
}

func TestComplete_NoMessages(t *testing.T) {
	mock := &mockConverseAPI{output: converseOutput("ok")}
	c := newClientWithAPI(mock)

	_, err := c.Complete(context.Background(), ai.CompletionRequest{
		Model:    "anthropic.claude-sonnet-4-6",
		Messages: []ai.Message{},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least one message")
}

func TestComplete_TrimsWhitespace(t *testing.T) {
	mock := &mockConverseAPI{output: converseOutput("  result with whitespace  \n")}
	c := newClientWithAPI(mock)

	result, err := c.Complete(context.Background(), ai.CompletionRequest{
		Model:    "anthropic.claude-sonnet-4-6",
		Messages: []ai.Message{{Role: ai.RoleUser, Content: "test"}},
	})
	require.NoError(t, err)
	assert.Equal(t, "result with whitespace", result)
}

func TestComplete_EmptyTextContent(t *testing.T) {
	mock := &mockConverseAPI{output: converseOutput("")}
	c := newClientWithAPI(mock)

	_, err := c.Complete(context.Background(), ai.CompletionRequest{
		Model:    "anthropic.claude-sonnet-4-6",
		Messages: []ai.Message{{Role: ai.RoleUser, Content: "test"}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no text content")
}

func TestComplete_UnexpectedOutputType(t *testing.T) {
	mock := &mockConverseAPI{
		output: &bedrockruntime.ConverseOutput{
			Output: nil,
		},
	}
	c := newClientWithAPI(mock)

	_, err := c.Complete(context.Background(), ai.CompletionRequest{
		Model:    "anthropic.claude-sonnet-4-6",
		Messages: []ai.Message{{Role: ai.RoleUser, Content: "test"}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected output type")
}

func TestComplete_BedrockErrors(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		contain []string
	}{
		{
			"throttling",
			&types.ThrottlingException{Message: aws.String("rate limit exceeded")},
			[]string{"rate limit", "Bedrock"},
		},
		{
			"validation",
			&types.ValidationException{Message: aws.String("invalid model ID")},
			[]string{"validation", "invalid model ID"},
		},
		{
			"access_denied",
			&types.AccessDeniedException{Message: aws.String("not authorized")},
			[]string{"access denied", "not authorized"},
		},
		{
			"model_not_ready",
			&types.ModelNotReadyException{Message: aws.String("model is warming up")},
			[]string{"not ready"},
		},
		{
			"resource_not_found",
			&types.ResourceNotFoundException{Message: aws.String("model not found in us-east-1")},
			[]string{"model not found", "check model ID and region"},
		},
		{
			"quota_exceeded",
			&types.ServiceQuotaExceededException{Message: aws.String("quota exceeded")},
			[]string{"quota exceeded", "Bedrock"},
		},
		{
			"generic",
			fmt.Errorf("unknown API error"),
			[]string{"Bedrock Converse", "unknown API error"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockConverseAPI{err: tt.err}
			c := newClientWithAPI(mock)

			_, err := c.Complete(context.Background(), ai.CompletionRequest{
				Model:    "anthropic.claude-sonnet-4-6",
				Messages: []ai.Message{{Role: ai.RoleUser, Content: "test"}},
			})
			require.Error(t, err)
			for _, s := range tt.contain {
				assert.Contains(t, err.Error(), s)
			}
		})
	}
}

func TestComplete_MultipleTextBlocks(t *testing.T) {
	mock := &mockConverseAPI{
		output: &bedrockruntime.ConverseOutput{
			Output: &types.ConverseOutputMemberMessage{
				Value: types.Message{
					Role: "assistant",
					Content: []types.ContentBlock{
						&types.ContentBlockMemberText{Value: "first part"},
						&types.ContentBlockMemberText{Value: "second part"},
					},
				},
			},
		},
	}
	c := newClientWithAPI(mock)

	result, err := c.Complete(context.Background(), ai.CompletionRequest{
		Model:    "anthropic.claude-sonnet-4-6",
		Messages: []ai.Message{{Role: ai.RoleUser, Content: "test"}},
	})
	require.NoError(t, err)
	assert.Equal(t, "first part\nsecond part", result)
}

func TestComplete_MultipleSystemMessages(t *testing.T) {
	mock := &mockConverseAPI{output: converseOutput("ok")}
	c := newClientWithAPI(mock)

	_, err := c.Complete(context.Background(), ai.CompletionRequest{
		Model: "anthropic.claude-sonnet-4-6",
		Messages: []ai.Message{
			{Role: ai.RoleSystem, Content: "System prompt 1"},
			{Role: ai.RoleSystem, Content: "System prompt 2"},
			{Role: ai.RoleUser, Content: "review this"},
		},
	})
	require.NoError(t, err)

	require.Len(t, mock.input.System, 2)
	sys0, ok := mock.input.System[0].(*types.SystemContentBlockMemberText)
	require.True(t, ok)
	assert.Equal(t, "System prompt 1", sys0.Value)
	sys1, ok := mock.input.System[1].(*types.SystemContentBlockMemberText)
	require.True(t, ok)
	assert.Equal(t, "System prompt 2", sys1.Value)

	require.Len(t, mock.input.Messages, 1)
}

func TestComplete_WhitespaceOnlyContent(t *testing.T) {
	mock := &mockConverseAPI{output: converseOutput("   \n  ")}
	c := newClientWithAPI(mock)

	_, err := c.Complete(context.Background(), ai.CompletionRequest{
		Model:    "anthropic.claude-sonnet-4-6",
		Messages: []ai.Message{{Role: ai.RoleUser, Content: "test"}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no text content")
}

func TestComplete_EmptyModel(t *testing.T) {
	mock := &mockConverseAPI{output: converseOutput("ok")}
	c := newClientWithAPI(mock)

	_, err := c.Complete(context.Background(), ai.CompletionRequest{
		Model:    "",
		Messages: []ai.Message{{Role: ai.RoleUser, Content: "test"}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "model is required")
}

func TestComplete_SystemOnlyMessages(t *testing.T) {
	mock := &mockConverseAPI{output: converseOutput("ok")}
	c := newClientWithAPI(mock)

	_, err := c.Complete(context.Background(), ai.CompletionRequest{
		Model:    "anthropic.claude-sonnet-4-6",
		Messages: []ai.Message{{Role: ai.RoleSystem, Content: "system prompt"}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "non-system message")
}

func TestNewClient_EmptyRegion(t *testing.T) {
	_, err := NewClient(context.Background(), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "region is required")
}
