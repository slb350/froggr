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

func (m *mockConverseAPI) Converse(_ context.Context, input *bedrockruntime.ConverseInput, _ ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseOutput, error) {
	m.input = input
	return m.output, m.err
}

func converseOutput(text string) *bedrockruntime.ConverseOutput {
	return &bedrockruntime.ConverseOutput{
		StopReason: types.StopReasonEndTurn,
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
	assert.Nil(t, mock.input.System)
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
			StopReason: types.StopReasonEndTurn,
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
	assert.Contains(t, err.Error(), "no text content in response")
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
			StopReason: types.StopReasonEndTurn,
			Output:     nil,
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

func TestComplete_NilResponse(t *testing.T) {
	mock := &mockConverseAPI{output: nil}
	c := newClientWithAPI(mock)

	_, err := c.Complete(context.Background(), ai.CompletionRequest{
		Model:    "anthropic.claude-sonnet-4-6",
		Messages: []ai.Message{{Role: ai.RoleUser, Content: "test"}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil response")
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
			[]string{"rate limit", "bedrock"},
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
			[]string{"quota exceeded", "bedrock"},
		},
		{
			"model_error",
			&types.ModelErrorException{Message: aws.String("model processing failed")},
			[]string{"model error", "transient"},
		},
		{
			"internal_server",
			&types.InternalServerException{Message: aws.String("internal failure")},
			[]string{"internal server error", "transient"},
		},
		{
			"model_timeout",
			&types.ModelTimeoutException{Message: aws.String("processing timed out")},
			[]string{"timeout", "prompt may be too large"},
		},
		{
			"service_unavailable",
			&types.ServiceUnavailableException{Message: aws.String("service down")},
			[]string{"service unavailable", "transient"},
		},
		{
			"conflict",
			&types.ConflictException{Message: aws.String("conflict occurred")},
			[]string{"conflict"},
		},
		{
			"generic",
			fmt.Errorf("unknown API error"),
			[]string{"bedrock converse", "unknown API error"},
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
			StopReason: types.StopReasonEndTurn,
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
	assert.Contains(t, err.Error(), "no text content in response")
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

func TestComplete_StopReasons(t *testing.T) {
	tests := []struct {
		name    string
		reason  types.StopReason
		wantErr bool
		contain string
	}{
		{"end_turn", types.StopReasonEndTurn, false, ""},
		{"stop_sequence", types.StopReasonStopSequence, false, ""},
		{"max_tokens", types.StopReasonMaxTokens, true, "truncated"},
		{"guardrail_intervened", types.StopReasonGuardrailIntervened, true, "content filter"},
		{"content_filtered", types.StopReasonContentFiltered, true, "content filter"},
		{"tool_use", types.StopReasonToolUse, true, "unexpected stop reason"},
		{"unknown_future", types.StopReason("new_reason"), true, "unexpected stop reason"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockConverseAPI{
				output: &bedrockruntime.ConverseOutput{
					StopReason: tt.reason,
					Output: &types.ConverseOutputMemberMessage{
						Value: types.Message{
							Role: "assistant",
							Content: []types.ContentBlock{
								&types.ContentBlockMemberText{Value: "response text"},
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
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.contain)
			} else {
				require.NoError(t, err)
				assert.Equal(t, "response text", result)
			}
		})
	}
}

func TestComplete_NonTextContentBlock(t *testing.T) {
	mock := &mockConverseAPI{
		output: &bedrockruntime.ConverseOutput{
			StopReason: types.StopReasonEndTurn,
			Output: &types.ConverseOutputMemberMessage{
				Value: types.Message{
					Role: "assistant",
					Content: []types.ContentBlock{
						&types.ContentBlockMemberText{Value: "some text"},
						&types.ContentBlockMemberImage{},
					},
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
	assert.Contains(t, err.Error(), "unexpected content block type")
	assert.Contains(t, err.Error(), "text-only responses")
}

func TestNewClientWithAPI_NilPanics(t *testing.T) {
	assert.PanicsWithValue(t, "bedrock.newClientWithAPI: nil api", func() {
		newClientWithAPI(nil)
	})
}

func TestNewClient_EmptyRegion(t *testing.T) {
	_, err := NewClient(context.Background(), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "region is required")
}
