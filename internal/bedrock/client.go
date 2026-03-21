// Package bedrock provides a client for the AWS Bedrock Runtime Converse API.
package bedrock

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	"github.com/slb350/froggr/internal/ai"
)

// converseAPI is the subset of the Bedrock Runtime API used by Client.
// Defined here for testability (mock interfaces where consumed).
type converseAPI interface {
	Converse(ctx context.Context, input *bedrockruntime.ConverseInput, opts ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseOutput, error)
}

// Client wraps the Bedrock Runtime Converse API.
type Client struct {
	api converseAPI
}

// NewClient creates a Bedrock client using the default AWS credential chain.
func NewClient(ctx context.Context, region string) (*Client, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("loading AWS config: %w", err)
	}
	return &Client{api: bedrockruntime.NewFromConfig(cfg)}, nil
}

// newClientWithAPI creates a Client with a custom API implementation (for testing).
func newClientWithAPI(api converseAPI) *Client {
	return &Client{api: api}
}

// Complete sends a chat completion request via the Bedrock Converse API.
func (c *Client) Complete(ctx context.Context, req ai.CompletionRequest) (string, error) {
	if err := req.Validate(); err != nil {
		return "", fmt.Errorf("Bedrock: %w", err)
	}

	system, messages := splitMessages(req.Messages)

	input := &bedrockruntime.ConverseInput{
		ModelId:  aws.String(req.Model),
		Messages: messages,
	}
	if len(system) > 0 {
		input.System = system
	}

	resp, err := c.api.Converse(ctx, input)
	if err != nil {
		return "", formatError(err)
	}

	return extractText(resp)
}

// splitMessages separates system messages into Bedrock's System field
// and converts remaining messages to Bedrock's Message type.
func splitMessages(msgs []ai.Message) ([]types.SystemContentBlock, []types.Message) {
	var system []types.SystemContentBlock
	var messages []types.Message

	for _, m := range msgs {
		if m.Role == ai.RoleSystem {
			system = append(system, &types.SystemContentBlockMemberText{Value: m.Content})
			continue
		}
		messages = append(messages, types.Message{
			Role: types.ConversationRole(string(m.Role)),
			Content: []types.ContentBlock{
				&types.ContentBlockMemberText{Value: m.Content},
			},
		})
	}

	return system, messages
}

// formatError wraps Bedrock API errors with descriptive context.
func formatError(err error) error {
	var throttle *types.ThrottlingException
	if errors.As(err, &throttle) {
		return fmt.Errorf("Bedrock rate limit: %w", err)
	}

	var validation *types.ValidationException
	if errors.As(err, &validation) {
		return fmt.Errorf("Bedrock validation: %w", err)
	}

	var access *types.AccessDeniedException
	if errors.As(err, &access) {
		return fmt.Errorf("Bedrock access denied: %w", err)
	}

	var notReady *types.ModelNotReadyException
	if errors.As(err, &notReady) {
		return fmt.Errorf("Bedrock model not ready: %w", err)
	}

	var notFound *types.ResourceNotFoundException
	if errors.As(err, &notFound) {
		return fmt.Errorf("Bedrock model not found (check model ID and region): %w", err)
	}

	var quotaExceeded *types.ServiceQuotaExceededException
	if errors.As(err, &quotaExceeded) {
		return fmt.Errorf("Bedrock service quota exceeded (request a quota increase): %w", err)
	}

	return fmt.Errorf("Bedrock Converse: %w", err)
}

// extractText pulls the assistant's text from the Converse response,
// concatenating all text content blocks.
func extractText(resp *bedrockruntime.ConverseOutput) (string, error) {
	msg, ok := resp.Output.(*types.ConverseOutputMemberMessage)
	if !ok {
		return "", fmt.Errorf("Bedrock: unexpected output type %T", resp.Output)
	}

	var parts []string
	for _, block := range msg.Value.Content {
		if text, ok := block.(*types.ContentBlockMemberText); ok {
			if s := strings.TrimSpace(text.Value); s != "" {
				parts = append(parts, s)
			}
		}
	}

	if len(parts) == 0 {
		return "", fmt.Errorf("Bedrock: no text content in response")
	}
	return strings.Join(parts, "\n"), nil
}
