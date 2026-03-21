package review

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/google/go-github/v84/github"
	"github.com/slb350/froggr/internal/config"
	"github.com/slb350/froggr/internal/ghub"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockGitHub implements GitHubClient for testing.
type mockGitHub struct {
	issue       ghub.IssueInfo
	issueErr    error
	comments    []*github.IssueComment
	commentsErr error
	diffs       []ghub.FileDiff
	diffErr     error
	files       map[string]ghub.FileContent
	fileErr     error

	// Track calls for assertions.
	commentPosted  string
	commentPostErr error
	draftPRCreated bool
	draftPRErr     error
	draftPRNumber  int
	draftPRURL     string

	mu             sync.Mutex
	requestedFiles []string
}

func (m *mockGitHub) GetIssue(_ context.Context, _, _ string, _ int) (ghub.IssueInfo, error) {
	return m.issue, m.issueErr
}

func (m *mockGitHub) GetIssueComments(_ context.Context, _, _ string, _ int) ([]*github.IssueComment, error) {
	return m.comments, m.commentsErr
}

func (m *mockGitHub) GetBranchDiff(_ context.Context, _, _, _, _ string) ([]ghub.FileDiff, error) {
	return m.diffs, m.diffErr
}

func (m *mockGitHub) GetFileContent(_ context.Context, _, _, path, _ string) (ghub.FileContent, error) {
	m.mu.Lock()
	m.requestedFiles = append(m.requestedFiles, path)
	m.mu.Unlock()

	if m.fileErr != nil {
		return ghub.FileContent{}, m.fileErr
	}
	fc, ok := m.files[path]
	if !ok {
		return ghub.FileContent{}, errors.New("file not found")
	}
	return fc, nil
}

func (m *mockGitHub) CreateIssueComment(_ context.Context, _, _ string, _ int, body string) error {
	m.commentPosted = body
	return m.commentPostErr
}

func (m *mockGitHub) CreateDraftPR(_ context.Context, _, _, _, _, _, _ string) (int, string, error) {
	m.draftPRCreated = true
	return m.draftPRNumber, m.draftPRURL, m.draftPRErr
}

func TestBuildContext_Success(t *testing.T) {
	gh := &mockGitHub{
		issue: ghub.IssueInfo{
			Number: 42,
			Title:  "Add authentication",
			Body:   "We need auth",
			State:  "open",
		},
		comments: []*github.IssueComment{
			{Body: github.Ptr("Human comment"), User: &github.User{Type: github.Ptr("User")}},
			{Body: github.Ptr("froggr review"), User: &github.User{Login: github.Ptr("froggr[bot]"), Type: github.Ptr("Bot")}},
		},
		diffs: []ghub.FileDiff{
			{Path: "src/auth.go", Status: "added", Patch: "+package auth"},
		},
		files: map[string]ghub.FileContent{
			"src/auth.go": {Path: "src/auth.go", Content: "package auth"},
		},
	}

	push := ghub.PushContext{
		Owner:         "owner",
		Repo:          "repo",
		Branch:        "42-add-auth",
		HeadSHA:       "abc123",
		DefaultBranch: "main",
	}

	rc, err := BuildContext(context.Background(), gh, push, 42, config.Defaults())
	require.NoError(t, err)
	assert.Equal(t, gh.issue, rc.Issue)
	assert.Len(t, rc.Diffs, 1)
	assert.Len(t, rc.Files, 1)
	assert.Len(t, rc.PriorReviews, 1)
	assert.Contains(t, rc.PriorReviews[0], "froggr review")
	assert.Equal(t, push, rc.Push)
}

func TestBuildContext_IgnoresConfiguredPaths(t *testing.T) {
	gh := &mockGitHub{
		issue: ghub.IssueInfo{Number: 42, Title: "Test", State: "open"},
		diffs: []ghub.FileDiff{
			{Path: "src/auth.go", Status: "added", Patch: "+package auth"},
			{Path: "vendor/lib/foo.go", Status: "added", Patch: "+package foo"},
			{Path: "yarn.lock", Status: "modified", Patch: "+hash"},
		},
		files: map[string]ghub.FileContent{
			"src/auth.go": {Path: "src/auth.go", Content: "package auth"},
		},
	}

	push := ghub.PushContext{
		Owner: "owner", Repo: "repo", Branch: "42-test",
		HeadSHA: "abc123", DefaultBranch: "main",
	}
	cfg := config.Defaults()

	rc, err := BuildContext(context.Background(), gh, push, 42, cfg)
	require.NoError(t, err)
	// vendor/ and *.lock should be filtered by default ignore paths.
	assert.Len(t, rc.Diffs, 1)
	assert.Equal(t, "src/auth.go", rc.Diffs[0].Path)
}

func TestBuildContext_FiltersPriorFroggrComments(t *testing.T) {
	gh := &mockGitHub{
		issue: ghub.IssueInfo{Number: 42, Title: "Test", State: "open"},
		comments: []*github.IssueComment{
			{Body: github.Ptr("Human discussion"), User: &github.User{Type: github.Ptr("User")}},
			{Body: github.Ptr("froggr: looks good"), User: &github.User{Login: github.Ptr("froggr[bot]"), Type: github.Ptr("Bot")}},
			{Body: github.Ptr("Another bot"), User: &github.User{Login: github.Ptr("other-bot[bot]"), Type: github.Ptr("Bot")}},
		},
		diffs: []ghub.FileDiff{},
	}

	push := ghub.PushContext{
		Owner: "owner", Repo: "repo", Branch: "42-test",
		HeadSHA: "abc123", DefaultBranch: "main",
	}

	rc, err := BuildContext(context.Background(), gh, push, 42, config.Defaults())
	require.NoError(t, err)
	require.Len(t, rc.PriorReviews, 1)
	assert.Contains(t, rc.PriorReviews[0], "froggr: looks good")
}

func TestBuildContext_ExcludesSkippedAndFailedPriorComments(t *testing.T) {
	gh := &mockGitHub{
		issue: ghub.IssueInfo{Number: 42, Title: "Test", State: "open"},
		comments: []*github.IssueComment{
			{Body: github.Ptr("## froggr review: `42-test` @ `abc1234`\n\nReview failed: rate limited\n\nPush again to retry.\n"), User: &github.User{Login: github.Ptr("froggr[bot]"), Type: github.Ptr("Bot")}},
			{Body: github.Ptr("## froggr review: `42-test` @ `abc1234`\n\nReview skipped.\n\nComparison too large.\n"), User: &github.User{Login: github.Ptr("froggr[bot]"), Type: github.Ptr("Bot")}},
			{Body: github.Ptr("## froggr review: `42-test` @ `abc1234`\n\nAll clean! No bugs found.\n"), User: &github.User{Login: github.Ptr("froggr[bot]"), Type: github.Ptr("Bot")}},
		},
		diffs: []ghub.FileDiff{},
	}

	push := ghub.PushContext{
		Owner: "owner", Repo: "repo", Branch: "42-test",
		HeadSHA: "abc123", DefaultBranch: "main",
	}

	rc, err := BuildContext(context.Background(), gh, push, 42, config.Defaults())
	require.NoError(t, err)
	require.Len(t, rc.PriorReviews, 1)
	assert.Contains(t, rc.PriorReviews[0], "All clean!")
}

func TestBuildContext_LimitsFetchedDiffFiles(t *testing.T) {
	gh := &mockGitHub{
		issue: ghub.IssueInfo{Number: 42, Title: "Large change", State: "open"},
		files: map[string]ghub.FileContent{},
	}

	for i := 0; i < maxContextDiffFiles+3; i++ {
		path := fmt.Sprintf("src/file-%02d.go", i)
		gh.diffs = append(gh.diffs, ghub.FileDiff{
			Path:   path,
			Status: "modified",
			Patch:  "+package main",
		})
		gh.files[path] = ghub.FileContent{Path: path, Content: "package main"}
	}

	push := ghub.PushContext{
		Owner: "owner", Repo: "repo", Branch: "42-large",
		HeadSHA: "abc123", DefaultBranch: "main",
	}

	rc, err := BuildContext(context.Background(), gh, push, 42, config.Defaults())
	require.NoError(t, err)
	assert.Len(t, rc.Diffs, maxContextDiffFiles)
	assert.Len(t, rc.Files, maxContextDiffFiles)
	assert.Equal(t, 3, rc.OmittedDiffs)
	assert.Len(t, gh.requestedFiles, maxContextDiffFiles)
}

func TestBuildContext_UsesLatestPriorReviews(t *testing.T) {
	gh := &mockGitHub{
		issue: ghub.IssueInfo{Number: 42, Title: "History", State: "open"},
		diffs: []ghub.FileDiff{},
	}

	for i := 0; i < maxContextPriorReviews+2; i++ {
		gh.comments = append(gh.comments, &github.IssueComment{
			Body: github.Ptr(fmt.Sprintf("froggr review %d", i+1)),
			User: &github.User{Login: github.Ptr("froggr[bot]"), Type: github.Ptr("Bot")},
		})
	}

	push := ghub.PushContext{
		Owner: "owner", Repo: "repo", Branch: "42-history",
		HeadSHA: "abc123", DefaultBranch: "main",
	}

	rc, err := BuildContext(context.Background(), gh, push, 42, config.Defaults())
	require.NoError(t, err)
	require.Len(t, rc.PriorReviews, maxContextPriorReviews)
	assert.Equal(t, 2, rc.OmittedPriors)
	assert.Equal(t, "froggr review 3", rc.PriorReviews[0])
	assert.Equal(t, "froggr review 7", rc.PriorReviews[len(rc.PriorReviews)-1])
}

func TestBuildContext_AllDiffsFilteredByIgnorePaths(t *testing.T) {
	gh := &mockGitHub{
		issue: ghub.IssueInfo{Number: 42, Title: "Vendor update", State: "open"},
		diffs: []ghub.FileDiff{
			{Path: "vendor/lib/foo.go", Status: "modified", Patch: "+update"},
			{Path: "go.lock", Status: "modified", Patch: "+hash"},
		},
	}

	push := ghub.PushContext{
		Owner: "owner", Repo: "repo", Branch: "42-vendor",
		HeadSHA: "abc123", DefaultBranch: "main",
	}

	rc, err := BuildContext(context.Background(), gh, push, 42, config.Defaults())
	require.NoError(t, err)
	assert.Empty(t, rc.Diffs)
	assert.Empty(t, rc.Files)
}

func TestBuildContext_IssueClosed(t *testing.T) {
	gh := &mockGitHub{
		issue: ghub.IssueInfo{Number: 42, Title: "Done", State: "closed"},
	}

	push := ghub.PushContext{
		Owner: "owner", Repo: "repo", Branch: "42-done",
		HeadSHA: "abc123", DefaultBranch: "main",
	}

	_, err := BuildContext(context.Background(), gh, push, 42, config.Defaults())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "closed")
}

func TestBuildContext_DiffError(t *testing.T) {
	gh := &mockGitHub{
		issue:   ghub.IssueInfo{Number: 42, Title: "Test", State: "open"},
		diffErr: errors.New("API rate limit exceeded"),
	}

	push := ghub.PushContext{
		Owner: "owner", Repo: "repo", Branch: "42-test",
		HeadSHA: "abc123", DefaultBranch: "main",
	}

	_, err := BuildContext(context.Background(), gh, push, 42, config.Defaults())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rate limit")
}
