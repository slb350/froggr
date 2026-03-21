package review

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/slb350/froggr/internal/ai"
	"github.com/slb350/froggr/internal/config"
	"github.com/slb350/froggr/internal/ghub"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockAI implements AIClient for testing.
type mockAI struct {
	response string
	err      error
	calls    int
}

func (m *mockAI) Complete(_ context.Context, _ ai.CompletionRequest) (string, error) {
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

	engine := NewEngine(map[string]AIClient{config.ProviderOpenRouter: ai})
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

	engine := NewEngine(map[string]AIClient{config.ProviderOpenRouter: ai})
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

	engine := NewEngine(map[string]AIClient{config.ProviderOpenRouter: ai})
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

	engine := NewEngine(map[string]AIClient{config.ProviderOpenRouter: ai})
	err := engine.Review(context.Background(), gh, basePush(), 42, cfg)
	require.NoError(t, err)

	assert.Contains(t, gh.commentPosted, "oops")
	assert.False(t, gh.draftPRCreated)
}

func TestEngine_Review_IssueClosed_Skips(t *testing.T) {
	gh := baseGitHub()
	gh.issue.State = "closed"
	ai := &mockAI{response: "[]"}

	engine := NewEngine(map[string]AIClient{config.ProviderOpenRouter: ai})
	err := engine.Review(context.Background(), gh, basePush(), 42, config.Defaults())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "closed")
	assert.Empty(t, gh.commentPosted)
}

func TestEngine_Review_AIError_Propagates(t *testing.T) {
	gh := baseGitHub()
	ai := &mockAI{err: errors.New("rate limited")}

	engine := NewEngine(map[string]AIClient{config.ProviderOpenRouter: ai})
	err := engine.Review(context.Background(), gh, basePush(), 42, config.Defaults())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rate limited")
}

func TestEngine_Review_InvalidAIResponse_Propagates(t *testing.T) {
	gh := baseGitHub()
	ai := &mockAI{response: "looks good to me"}

	engine := NewEngine(map[string]AIClient{config.ProviderOpenRouter: ai})
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

	engine := NewEngine(map[string]AIClient{config.ProviderOpenRouter: ai})
	err := engine.Review(ctx, gh, basePush(), 42, config.Defaults())
	require.Error(t, err)
}

func TestEngine_Review_CommentPostError_Propagates(t *testing.T) {
	gh := baseGitHub()
	gh.commentPostErr = errors.New("rate limited")
	ai := &mockAI{response: "[]"}

	engine := NewEngine(map[string]AIClient{config.ProviderOpenRouter: ai})
	err := engine.Review(context.Background(), gh, basePush(), 42, config.Defaults())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rate limited")
	assert.Contains(t, err.Error(), "posting review comment")
}

func TestEngine_Review_DraftPRError_Propagates(t *testing.T) {
	gh := baseGitHub()
	gh.draftPRErr = errors.New("forbidden")
	ai := &mockAI{response: "[]"}

	cfg := config.Defaults()
	cfg.AutoDraftPR = true

	engine := NewEngine(map[string]AIClient{config.ProviderOpenRouter: ai})
	err := engine.Review(context.Background(), gh, basePush(), 42, cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "forbidden")
	assert.Contains(t, err.Error(), "creating draft PR")
}

func TestEngine_Review_ComparisonTooLarge_PostsSkipComment(t *testing.T) {
	gh := baseGitHub()
	gh.diffErr = ghub.ErrComparisonTooLarge
	ai := &mockAI{response: "[]"}

	engine := NewEngine(map[string]AIClient{config.ProviderOpenRouter: ai})
	err := engine.Review(context.Background(), gh, basePush(), 42, config.Defaults())
	require.NoError(t, err)

	assert.Contains(t, gh.commentPosted, "Review skipped")
	assert.Contains(t, gh.commentPosted, "up to 300 changed files")
	assert.Equal(t, 0, ai.calls)
	assert.False(t, gh.draftPRCreated)
}

func TestEngine_Review_ComparisonTooLarge_SkipCommentFails(t *testing.T) {
	gh := baseGitHub()
	gh.diffErr = ghub.ErrComparisonTooLarge
	gh.commentPostErr = errors.New("rate limited")
	ai := &mockAI{}

	engine := NewEngine(map[string]AIClient{config.ProviderOpenRouter: ai})
	err := engine.Review(context.Background(), gh, basePush(), 42, config.Defaults())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "posting skipped review comment")
	assert.Equal(t, 0, ai.calls)
}

func TestEngine_Review_PromptBudgetExhausted(t *testing.T) {
	gh := baseGitHub()
	gh.issue.Title = "x" // very short title
	gh.issue.Body = strings.Repeat("body ", 30000)
	ai := &mockAI{response: "[]"}

	engine := NewEngine(map[string]AIClient{config.ProviderOpenRouter: ai})
	err := engine.Review(context.Background(), gh, basePush(), 42, config.Defaults())
	// The issue body is huge but truncated; diffs should still fit.
	// This test verifies the prompt pipeline handles large bodies gracefully.
	require.NoError(t, err)
}

func TestEngine_Review_SelectsProviderFromConfig(t *testing.T) {
	gh := baseGitHub()
	orMock := &mockAI{response: "[]"}
	brMock := &mockAI{response: `[{"severity":"Bug","file":"x.go","line":1,"description":"bedrock found it"}]`}

	engine := NewEngine(map[string]AIClient{config.ProviderOpenRouter: orMock, config.ProviderBedrock: brMock})

	cfg := config.Defaults()
	cfg.Provider = config.ProviderBedrock

	err := engine.Review(context.Background(), gh, basePush(), 42, cfg)
	require.NoError(t, err)
	assert.Equal(t, 0, orMock.calls)
	assert.Equal(t, 1, brMock.calls)
	assert.Contains(t, gh.commentPosted, "bedrock found it")
}

func TestEngine_Review_UnconfiguredProvider(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		contain  []string
	}{
		{"unknown", "unknown", []string{"unknown", "provider"}},
		{"missing", config.ProviderBedrock, []string{"bedrock", "not configured"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gh := baseGitHub()
			mock := &mockAI{response: "[]"}
			engine := NewEngine(map[string]AIClient{config.ProviderOpenRouter: mock})

			cfg := config.Defaults()
			cfg.Provider = tt.provider

			err := engine.Review(context.Background(), gh, basePush(), 42, cfg)
			require.Error(t, err)
			for _, s := range tt.contain {
				assert.Contains(t, err.Error(), s)
			}
		})
	}
}

// Ensure mockGitHub satisfies GitHubClient at compile time.
var _ GitHubClient = (*mockGitHub)(nil)

// Ensure mockAI satisfies AIClient at compile time.
var _ AIClient = (*mockAI)(nil)
