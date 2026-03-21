// Package ai defines provider-agnostic types for AI chat completions.
// Both openrouter and bedrock clients accept these types, allowing the
// review engine to work with any provider without importing provider packages.
package ai

// Message represents a single chat message.
type Message struct {
	Role    string
	Content string
}

// CompletionRequest holds the parameters for a chat completion call.
type CompletionRequest struct {
	Model    string
	Messages []Message
}
