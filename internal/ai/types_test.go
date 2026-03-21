package ai

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRole_Valid(t *testing.T) {
	tests := []struct {
		role  Role
		valid bool
	}{
		{RoleSystem, true},
		{RoleUser, true},
		{RoleAssistant, true},
		{Role("admin"), false},
		{Role(""), false},
	}
	for _, tt := range tests {
		t.Run(string(tt.role), func(t *testing.T) {
			assert.Equal(t, tt.valid, tt.role.Valid())
		})
	}
}

func TestValidate_ValidRequest(t *testing.T) {
	req := CompletionRequest{
		Model: "anthropic/claude-sonnet-4.6",
		Messages: []Message{
			{Role: RoleSystem, Content: "system prompt"},
			{Role: RoleUser, Content: "user message"},
		},
	}
	assert.NoError(t, req.Validate())
}

func TestValidate_EmptyModel(t *testing.T) {
	req := CompletionRequest{
		Model:    "",
		Messages: []Message{{Role: RoleUser, Content: "test"}},
	}
	err := req.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "model is required")
}

func TestValidate_EmptyMessages(t *testing.T) {
	req := CompletionRequest{
		Model:    "model",
		Messages: []Message{},
	}
	err := req.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least one message is required")
}

func TestValidate_NilMessages(t *testing.T) {
	req := CompletionRequest{
		Model:    "model",
		Messages: nil,
	}
	err := req.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least one message is required")
}

func TestValidate_InvalidRole(t *testing.T) {
	req := CompletionRequest{
		Model:    "model",
		Messages: []Message{{Role: Role("admin"), Content: "test"}},
	}
	err := req.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid role")
}

func TestValidate_EmptyContent(t *testing.T) {
	req := CompletionRequest{
		Model:    "model",
		Messages: []Message{{Role: RoleUser, Content: ""}},
	}
	err := req.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty content")
}

func TestValidate_SystemOnlyMessages(t *testing.T) {
	req := CompletionRequest{
		Model:    "model",
		Messages: []Message{{Role: RoleSystem, Content: "system prompt"}},
	}
	err := req.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "non-system message")
}
