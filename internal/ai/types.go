// Package ai defines provider-agnostic types for AI chat completions.
// Both openrouter and bedrock clients accept these types, allowing the
// review engine to work with any provider without importing provider packages.
package ai

import "fmt"

// Role identifies the sender of a chat message.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// Message represents a single chat message.
type Message struct {
	Role    Role
	Content string
}

// CompletionRequest holds the parameters for a chat completion call.
type CompletionRequest struct {
	Model    string
	Messages []Message
}

// Validate checks that the request has a model and at least one message.
func (r CompletionRequest) Validate() error {
	if r.Model == "" {
		return fmt.Errorf("model is required")
	}
	if len(r.Messages) == 0 {
		return fmt.Errorf("at least one message is required")
	}
	return nil
}
