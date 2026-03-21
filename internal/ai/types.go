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

// Valid reports whether r is one of the three known roles.
func (r Role) Valid() bool {
	switch r {
	case RoleSystem, RoleUser, RoleAssistant:
		return true
	}
	return false
}

// Validate checks that the request has a model, at least one message with
// valid roles and non-empty content, and at least one non-system message
// (required by all providers).
func (r CompletionRequest) Validate() error {
	if r.Model == "" {
		return fmt.Errorf("model is required")
	}
	if len(r.Messages) == 0 {
		return fmt.Errorf("at least one message is required")
	}
	hasNonSystem := false
	for i, m := range r.Messages {
		if !m.Role.Valid() {
			return fmt.Errorf("message %d has invalid role %q", i, m.Role)
		}
		if m.Content == "" {
			return fmt.Errorf("message %d has empty content", i)
		}
		if m.Role != RoleSystem {
			hasNonSystem = true
		}
	}
	if !hasNonSystem {
		return fmt.Errorf("at least one non-system message is required")
	}
	return nil
}
