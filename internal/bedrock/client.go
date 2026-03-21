// Package bedrock provides a client for the AWS Bedrock Runtime Converse API.
package bedrock

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	"github.com/slb350/froggr/internal/ai"
)

// converseAPI is the subset of the Bedrock Runtime API used by Client.
// Defined here so the Client can be unit-tested with a mock replacing the real AWS SDK.
type converseAPI interface {
	Converse(ctx context.Context, input *bedrockruntime.ConverseInput, opts ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseOutput, error)
}

// Client wraps the Bedrock Runtime Converse API.
type Client struct {
	api converseAPI
}

// NewClient creates a Bedrock client using the default AWS credential chain.
func NewClient(ctx context.Context, region string) (*Client, error) {
	if region == "" {
		return nil, fmt.Errorf("bedrock: region is required")
	}
	cfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(region),
		awsconfig.WithHTTPClient(&http.Client{Timeout: ai.DefaultHTTPTimeout}),
	)
	if err != nil {
		return nil, fmt.Errorf("loading AWS config: %w", err)
	}
	return &Client{api: bedrockruntime.NewFromConfig(cfg)}, nil
}

// newClientWithAPI creates a Client with a custom API implementation (for testing).
func newClientWithAPI(api converseAPI) *Client {
	if api == nil {
		panic("bedrock.newClientWithAPI: nil api")
	}
	return &Client{api: api}
}

// Complete sends a chat completion request via the Bedrock Converse API.
func (c *Client) Complete(ctx context.Context, req ai.CompletionRequest) (string, error) {
	if err := req.Validate(); err != nil {
		return "", fmt.Errorf("bedrock: %w", err)
	}

	system, messages, err := splitMessages(req.Messages)
	if err != nil {
		return "", err
	}

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
	if resp == nil {
		return "", fmt.Errorf("bedrock: received nil response from converse API")
	}

	if err := checkStopReason(resp.StopReason); err != nil {
		return "", err
	}

	return extractText(resp)
}

// checkStopReason returns an error for stop reasons that indicate the response
// is incomplete or was blocked. Only end_turn and stop_sequence are treated as
// normal completions. tool_use is valid in Bedrock but froggr does not use tool
// calling, so it falls through to the default error case.
func checkStopReason(reason types.StopReason) error {
	switch reason {
	case types.StopReasonEndTurn, types.StopReasonStopSequence:
		return nil
	case types.StopReasonMaxTokens:
		return fmt.Errorf("bedrock: response truncated (max_tokens reached)")
	case types.StopReasonGuardrailIntervened, types.StopReasonContentFiltered:
		return fmt.Errorf("bedrock: response blocked by content filter (stop_reason=%s)", reason)
	case "":
		return fmt.Errorf("bedrock: response missing stop_reason")
	default:
		return fmt.Errorf("bedrock: unexpected stop reason %q", reason)
	}
}

// splitMessages separates system messages into Bedrock's System field
// and converts remaining messages to Bedrock's Message type.
// Returns an error if a non-system message has an unrecognized role.
func splitMessages(msgs []ai.Message) ([]types.SystemContentBlock, []types.Message, error) {
	var system []types.SystemContentBlock
	var messages []types.Message

	for _, m := range msgs {
		if m.Role == ai.RoleSystem {
			system = append(system, &types.SystemContentBlockMemberText{Value: m.Content})
			continue
		}
		role, err := bedrockRole(m.Role)
		if err != nil {
			return nil, nil, err
		}
		messages = append(messages, types.Message{
			Role: role,
			Content: []types.ContentBlock{
				&types.ContentBlockMemberText{Value: m.Content},
			},
		})
	}

	return system, messages, nil
}

// bedrockRole maps ai.Role to Bedrock's ConversationRole via an explicit
// switch so that adding a new ai.Role without updating this mapping
// produces a clear error rather than silently passing an invalid value.
// Invariant: ai.Role string values match Bedrock's ConversationRole values.
// This is verified by TestRoleValues_MatchBedrockConversationRole.
func bedrockRole(r ai.Role) (types.ConversationRole, error) {
	switch r {
	case ai.RoleUser:
		return types.ConversationRoleUser, nil
	case ai.RoleAssistant:
		return types.ConversationRoleAssistant, nil
	default:
		return "", fmt.Errorf("bedrock: unsupported role %q", r)
	}
}

// isErrType returns true if err's chain contains an error of type T.
func isErrType[T error](err error) bool {
	var target T
	return errors.As(err, &target)
}

// bedrockErrors maps known Bedrock API exceptions to operator-friendly prefixes.
var bedrockErrors = []struct {
	match  func(error) bool
	prefix string
}{
	{isErrType[*types.ThrottlingException], "bedrock rate limit"},
	{isErrType[*types.ValidationException], "bedrock validation"},
	{isErrType[*types.AccessDeniedException], "bedrock access denied"},
	{isErrType[*types.ModelNotReadyException], "bedrock model not ready"},
	{isErrType[*types.ResourceNotFoundException], "bedrock model not found (check model ID and region)"},
	{isErrType[*types.ServiceQuotaExceededException], "bedrock service quota exceeded (request a quota increase)"},
	{isErrType[*types.ModelErrorException], "bedrock model error (may be transient, retry)"},
	{isErrType[*types.InternalServerException], "bedrock internal server error (may be transient, retry)"},
	{isErrType[*types.ModelTimeoutException], "bedrock model timeout (prompt may be too large, or retry)"},
	{isErrType[*types.ServiceUnavailableException], "bedrock service unavailable (may be transient, retry)"},
	{isErrType[*types.ConflictException], "bedrock conflict"},
}

// formatError wraps known Bedrock API error types with operator-friendly messages.
// Unknown errors receive a generic "bedrock converse" prefix.
func formatError(err error) error {
	for _, m := range bedrockErrors {
		if m.match(err) {
			return fmt.Errorf("%s: %w", m.prefix, err)
		}
	}
	return fmt.Errorf("bedrock converse: %w", err)
}

// extractText pulls the assistant's text from the Converse response.
// Each text block is trimmed; whitespace-only blocks are skipped.
// Non-empty blocks are joined with newlines.
// Returns an error if the response contains non-text content blocks
// (e.g. tool_use, image) since froggr only expects text responses.
func extractText(resp *bedrockruntime.ConverseOutput) (string, error) {
	msg, ok := resp.Output.(*types.ConverseOutputMemberMessage)
	if !ok {
		return "", fmt.Errorf("bedrock: unexpected output type %T", resp.Output)
	}

	var parts []string
	for _, block := range msg.Value.Content {
		text, ok := block.(*types.ContentBlockMemberText)
		if !ok {
			return "", fmt.Errorf("bedrock: unexpected content block type %T (froggr expects text-only responses)", block)
		}
		if s := strings.TrimSpace(text.Value); s != "" {
			parts = append(parts, s)
		}
	}

	if len(parts) == 0 {
		return "", fmt.Errorf("bedrock: no text content in response")
	}
	return strings.Join(parts, "\n"), nil
}
