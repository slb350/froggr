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
		Messages: []ai.Message{{Role: "user", Content: "review this code"}},
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
			{Role: "system", Content: "You are a code reviewer."},
			{Role: "user", Content: "review this"},
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
		Messages: []ai.Message{{Role: "user", Content: "test"}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no text content")
}

func TestComplete_ErrorPropagation(t *testing.T) {
	mock := &mockConverseAPI{err: fmt.Errorf("throttling exception")}
	c := newClientWithAPI(mock)

	_, err := c.Complete(context.Background(), ai.CompletionRequest{
		Model:    "anthropic.claude-sonnet-4-6",
		Messages: []ai.Message{{Role: "user", Content: "test"}},
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
		Messages: []ai.Message{{Role: "user", Content: "test"}},
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
	assert.Contains(t, err.Error(), "no messages")
}

func TestComplete_TrimsWhitespace(t *testing.T) {
	mock := &mockConverseAPI{output: converseOutput("  result with whitespace  \n")}
	c := newClientWithAPI(mock)

	result, err := c.Complete(context.Background(), ai.CompletionRequest{
		Model:    "anthropic.claude-sonnet-4-6",
		Messages: []ai.Message{{Role: "user", Content: "test"}},
	})
	require.NoError(t, err)
	assert.Equal(t, "result with whitespace", result)
}

func TestComplete_EmptyTextContent(t *testing.T) {
	mock := &mockConverseAPI{output: converseOutput("")}
	c := newClientWithAPI(mock)

	_, err := c.Complete(context.Background(), ai.CompletionRequest{
		Model:    "anthropic.claude-sonnet-4-6",
		Messages: []ai.Message{{Role: "user", Content: "test"}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty content")
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
		Messages: []ai.Message{{Role: "user", Content: "test"}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected output type")
}

func TestComplete_ThrottlingException(t *testing.T) {
	mock := &mockConverseAPI{err: &types.ThrottlingException{Message: aws.String("rate limit exceeded")}}
	c := newClientWithAPI(mock)

	_, err := c.Complete(context.Background(), ai.CompletionRequest{
		Model:    "anthropic.claude-sonnet-4-6",
		Messages: []ai.Message{{Role: "user", Content: "test"}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rate limit")
	assert.Contains(t, err.Error(), "Bedrock")
}

func TestComplete_ValidationException(t *testing.T) {
	mock := &mockConverseAPI{err: &types.ValidationException{Message: aws.String("invalid model ID")}}
	c := newClientWithAPI(mock)

	_, err := c.Complete(context.Background(), ai.CompletionRequest{
		Model:    "bad-model",
		Messages: []ai.Message{{Role: "user", Content: "test"}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "validation")
	assert.Contains(t, err.Error(), "invalid model ID")
}

func TestComplete_AccessDeniedException(t *testing.T) {
	mock := &mockConverseAPI{err: &types.AccessDeniedException{Message: aws.String("not authorized")}}
	c := newClientWithAPI(mock)

	_, err := c.Complete(context.Background(), ai.CompletionRequest{
		Model:    "anthropic.claude-sonnet-4-6",
		Messages: []ai.Message{{Role: "user", Content: "test"}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "access denied")
	assert.Contains(t, err.Error(), "not authorized")
}

func TestComplete_ModelNotReadyException(t *testing.T) {
	mock := &mockConverseAPI{err: &types.ModelNotReadyException{Message: aws.String("model is warming up")}}
	c := newClientWithAPI(mock)

	_, err := c.Complete(context.Background(), ai.CompletionRequest{
		Model:    "anthropic.claude-sonnet-4-6",
		Messages: []ai.Message{{Role: "user", Content: "test"}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not ready")
}
