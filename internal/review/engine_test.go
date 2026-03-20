package review

import (
	"context"
	"errors"
	"testing"

	"github.com/slb350/froggr/internal/config"
	"github.com/slb350/froggr/internal/ghub"
	"github.com/slb350/froggr/internal/openrouter"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockAI implements AIClient for testing.
type mockAI struct {
	response string
	err      error
	calls    int
}

func (m *mockAI) Complete(_ context.Context, _ openrouter.CompletionRequest) (string, error) {
	m.calls++
	return m.response, m.err
}

func basePush() ghub.PushContext {
	return ghub.PushContext{
		Owner: "owner", Repo: "repo", Branch: "42-feature",
		HeadSHA: "abc123", DefaultBranch: "main",
	}
}

func baseGitHub() *mockGitHub {
	return &mockGitHub{
		issue: ghub.IssueInfo{Number: 42, Title: "Add feature", Body: "Details", State: "open"},
		diffs: []ghub.FileDiff{
			{Path: "src/main.go", Status: "modified", Patch: "+new line"},
		},
		files: map[string]ghub.FileContent{
			"src/main.go": {Path: "src/main.go", Content: "package main"},
		},
		draftPRNumber: 99,
		draftPRURL:    "https://github.com/owner/repo/pull/99",
	}
}

func TestEngine_Review_PostsComment(t *testing.T) {
	gh := baseGitHub()
	ai := &mockAI{response: `[{"severity":"Bug","file":"src/main.go","line":1,"description":"bug found"}]`}

	engine := NewEngine(ai)
	err := engine.Review(context.Background(), gh, basePush(), 42, config.Defaults())
	require.NoError(t, err)

	assert.NotEmpty(t, gh.commentPosted)
	assert.Contains(t, gh.commentPosted, "bug found")
	assert.False(t, gh.draftPRCreated)
}

func TestEngine_Review_Clean_CreatesDraftPR(t *testing.T) {
	gh := baseGitHub()
	ai := &mockAI{response: "[]"}

	cfg := config.Defaults()
	cfg.AutoDraftPR = true

	engine := NewEngine(ai)
	err := engine.Review(context.Background(), gh, basePush(), 42, cfg)
	require.NoError(t, err)

	assert.NotEmpty(t, gh.commentPosted)
	assert.Contains(t, gh.commentPosted, "clean")
	assert.True(t, gh.draftPRCreated)
}

func TestEngine_Review_Clean_NoPR_WhenDisabled(t *testing.T) {
	gh := baseGitHub()
	ai := &mockAI{response: "[]"}

	cfg := config.Defaults()
	cfg.AutoDraftPR = false

	engine := NewEngine(ai)
	err := engine.Review(context.Background(), gh, basePush(), 42, cfg)
	require.NoError(t, err)

	assert.Contains(t, gh.commentPosted, "clean")
	assert.False(t, gh.draftPRCreated)
}

func TestEngine_Review_WithBugs_NoPR(t *testing.T) {
	gh := baseGitHub()
	ai := &mockAI{response: `[{"severity":"Bug","file":"x.go","line":1,"description":"oops"}]`}

	cfg := config.Defaults()
	cfg.AutoDraftPR = true

	engine := NewEngine(ai)
	err := engine.Review(context.Background(), gh, basePush(), 42, cfg)
	require.NoError(t, err)

	assert.Contains(t, gh.commentPosted, "oops")
	assert.False(t, gh.draftPRCreated)
}

func TestEngine_Review_IssueClosed_Skips(t *testing.T) {
	gh := baseGitHub()
	gh.issue.State = "closed"
	ai := &mockAI{response: "[]"}

	engine := NewEngine(ai)
	err := engine.Review(context.Background(), gh, basePush(), 42, config.Defaults())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "closed")
	assert.Empty(t, gh.commentPosted)
}

func TestEngine_Review_AIError_Propagates(t *testing.T) {
	gh := baseGitHub()
	ai := &mockAI{err: errors.New("rate limited")}

	engine := NewEngine(ai)
	err := engine.Review(context.Background(), gh, basePush(), 42, config.Defaults())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rate limited")
}

func TestEngine_Review_InvalidAIResponse_Propagates(t *testing.T) {
	gh := baseGitHub()
	ai := &mockAI{response: "looks good to me"}

	engine := NewEngine(ai)
	err := engine.Review(context.Background(), gh, basePush(), 42, config.Defaults())
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidAIResponse)
	assert.Empty(t, gh.commentPosted)
	assert.False(t, gh.draftPRCreated)
}

func TestEngine_Review_ContextCancellation(t *testing.T) {
	gh := baseGitHub()
	ai := &mockAI{err: context.Canceled}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	engine := NewEngine(ai)
	err := engine.Review(ctx, gh, basePush(), 42, config.Defaults())
	require.Error(t, err)
}

func TestEngine_Review_ComparisonTooLarge_PostsSkipComment(t *testing.T) {
	gh := baseGitHub()
	gh.diffErr = ghub.ErrComparisonTooLarge
	ai := &mockAI{response: "[]"}

	engine := NewEngine(ai)
	err := engine.Review(context.Background(), gh, basePush(), 42, config.Defaults())
	require.NoError(t, err)

	assert.Contains(t, gh.commentPosted, "Review skipped")
	assert.Contains(t, gh.commentPosted, "up to 300 changed files")
	assert.Equal(t, 0, ai.calls)
	assert.False(t, gh.draftPRCreated)
}

// Ensure mockGitHub satisfies GitHubClient at compile time.
var _ GitHubClient = (*mockGitHub)(nil)

// Ensure mockAI satisfies AIClient at compile time.
var _ AIClient = (*mockAI)(nil)
