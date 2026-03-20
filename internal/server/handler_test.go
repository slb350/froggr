package server

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/google/go-github/v84/github"
	"github.com/slb350/froggr/internal/config"
	"github.com/slb350/froggr/internal/ghub"
	"github.com/slb350/froggr/internal/review"
	"github.com/stretchr/testify/assert"
)

const testDebounceWindow = 50 * time.Millisecond

// --- Mocks ---

// mockReviewer implements ReviewRunner for testing.
type mockReviewer struct {
	mu    sync.Mutex
	calls []reviewCall
	err   error
}

type reviewCall struct {
	push        ghub.PushContext
	issueNum    int
	cfg         config.Config
	hasDeadline bool
}

func (m *mockReviewer) Review(ctx context.Context, _ review.GitHubClient, push ghub.PushContext, issueNum int, cfg config.Config) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, hasDeadline := ctx.Deadline()
	m.calls = append(m.calls, reviewCall{push: push, issueNum: issueNum, cfg: cfg, hasDeadline: hasDeadline})
	return m.err
}

func (m *mockReviewer) getCalls() []reviewCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]reviewCall{}, m.calls...)
}

// mockGHClient implements review.GitHubClient for testing.
type mockGHClient struct {
	fileContent ghub.FileContent
	fileErr     error
}

func (m *mockGHClient) GetIssue(_ context.Context, _, _ string, _ int) (ghub.IssueInfo, error) {
	return ghub.IssueInfo{}, nil
}

func (m *mockGHClient) GetIssueComments(_ context.Context, _, _ string, _ int) ([]*github.IssueComment, error) {
	return nil, nil
}

func (m *mockGHClient) GetBranchDiff(_ context.Context, _, _, _, _ string) ([]ghub.FileDiff, error) {
	return nil, nil
}

func (m *mockGHClient) GetFileContent(_ context.Context, _, _, _, _ string) (ghub.FileContent, error) {
	return m.fileContent, m.fileErr
}

func (m *mockGHClient) CreateIssueComment(_ context.Context, _, _ string, _ int, _ string) error {
	return nil
}

func (m *mockGHClient) CreateDraftPR(_ context.Context, _, _, _, _, _, _ string) (int, string, error) {
	return 0, "", nil
}

// --- Helpers ---

func testPush() ghub.PushContext {
	return ghub.PushContext{
		InstallationID: 12345,
		Owner:          "owner",
		Repo:           "repo",
		Branch:         "42-add-auth",
		HeadSHA:        "abc123",
		DefaultBranch:  "main",
	}
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newTestHandler(gh *mockGHClient, eng ReviewRunner) *Handler {
	factory := func(_ int64) review.GitHubClient {
		return gh
	}
	return NewHandler(factory, eng, testDebounceWindow, testLogger())
}

func waitForReview(t *testing.T, eng *mockReviewer, timeout time.Duration) reviewCall {
	t.Helper()
	deadline := time.After(timeout)
	for {
		calls := eng.getCalls()
		if len(calls) > 0 {
			return calls[len(calls)-1]
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for review call")
			return reviewCall{}
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func noReview(t *testing.T, eng *mockReviewer, wait time.Duration) {
	t.Helper()
	time.Sleep(wait)
	assert.Empty(t, eng.getCalls(), "expected no review calls")
}

func notFoundError() error {
	return &github.ErrorResponse{Response: &http.Response{StatusCode: http.StatusNotFound}}
}

// --- Tests ---

func TestHandlePush_MatchingBranch(t *testing.T) {
	gh := &mockGHClient{fileErr: notFoundError()}
	eng := &mockReviewer{}
	h := newTestHandler(gh, eng)
	defer h.Stop()

	h.HandlePush(context.Background(), testPush())

	call := waitForReview(t, eng, 500*time.Millisecond)
	assert.Equal(t, "42-add-auth", call.push.Branch)
	assert.Equal(t, 42, call.issueNum)
	assert.True(t, call.hasDeadline)
}

func TestHandlePush_NonMatchingBranch(t *testing.T) {
	gh := &mockGHClient{fileErr: notFoundError()}
	eng := &mockReviewer{}
	h := newTestHandler(gh, eng)
	defer h.Stop()

	push := testPush()
	push.Branch = "feature-no-number"
	h.HandlePush(context.Background(), push)

	noReview(t, eng, testDebounceWindow*3)
}

func TestHandlePush_DefaultBranch(t *testing.T) {
	gh := &mockGHClient{fileErr: notFoundError()}
	eng := &mockReviewer{}
	h := newTestHandler(gh, eng)
	defer h.Stop()

	push := testPush()
	push.Branch = "main"
	h.HandlePush(context.Background(), push)

	noReview(t, eng, testDebounceWindow*3)
}

func TestHandleIssuesClosed(t *testing.T) {
	gh := &mockGHClient{fileErr: notFoundError()}
	eng := &mockReviewer{}
	h := newTestHandler(gh, eng)
	defer h.Stop()

	// Queue a review via push.
	h.HandlePush(context.Background(), testPush())

	// Close the issue before debounce fires.
	h.HandleIssuesClosed("owner", "repo", 42)

	// Review should never fire.
	noReview(t, eng, testDebounceWindow*3)
}

func TestHandler_FetchesRepoConfig(t *testing.T) {
	gh := &mockGHClient{
		fileContent: ghub.FileContent{
			Path:    ".froggr.yml",
			Content: "model: openai/gpt-4o\nauto_draft_pr: false\n",
		},
	}
	eng := &mockReviewer{}
	h := newTestHandler(gh, eng)
	defer h.Stop()

	h.HandlePush(context.Background(), testPush())

	call := waitForReview(t, eng, 500*time.Millisecond)
	assert.Equal(t, "openai/gpt-4o", call.cfg.Model)
	assert.False(t, call.cfg.AutoDraftPR)
}

func TestHandler_FallbackToDefaults(t *testing.T) {
	gh := &mockGHClient{fileErr: notFoundError()}
	eng := &mockReviewer{}
	h := newTestHandler(gh, eng)
	defer h.Stop()

	h.HandlePush(context.Background(), testPush())

	call := waitForReview(t, eng, 500*time.Millisecond)
	defaults := config.Defaults()
	assert.Equal(t, defaults.Model, call.cfg.Model)
	assert.True(t, call.cfg.AutoDraftPR)
}

func TestHandler_ConfigFetchFailure_SkipsReview(t *testing.T) {
	gh := &mockGHClient{
		fileErr: &github.ErrorResponse{Response: &http.Response{StatusCode: http.StatusForbidden}},
	}
	eng := &mockReviewer{}
	h := newTestHandler(gh, eng)
	defer h.Stop()

	h.HandlePush(context.Background(), testPush())

	noReview(t, eng, testDebounceWindow*3)
}

type blockingReviewer struct {
	started  chan struct{}
	canceled chan error
	once     sync.Once
}

func (b *blockingReviewer) Review(ctx context.Context, _ review.GitHubClient, _ ghub.PushContext, _ int, _ config.Config) error {
	b.once.Do(func() { close(b.started) })
	<-ctx.Done()
	b.canceled <- ctx.Err()
	return ctx.Err()
}

func TestHandler_StopCancelsInFlightReview(t *testing.T) {
	gh := &mockGHClient{fileErr: notFoundError()}
	eng := &blockingReviewer{
		started:  make(chan struct{}),
		canceled: make(chan error, 1),
	}
	h := newTestHandler(gh, eng)

	h.HandlePush(context.Background(), testPush())

	select {
	case <-eng.started:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for review to start")
	}

	h.Stop()

	select {
	case err := <-eng.canceled:
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for review cancellation")
	}
}

// Compile-time interface checks.
var _ ReviewRunner = (*mockReviewer)(nil)
var _ ReviewRunner = (*blockingReviewer)(nil)
var _ review.GitHubClient = (*mockGHClient)(nil)
