// Package server provides the HTTP webhook server and event routing for froggr.
package server

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/slb350/froggr/internal/config"
	"github.com/slb350/froggr/internal/debounce"
	"github.com/slb350/froggr/internal/ghub"
	"github.com/slb350/froggr/internal/review"
)

const froggrConfigPath = ".froggr.yml"

// reviewTimeout bounds each AI review call so a stalled upstream (AI provider
// or GitHub) cannot block the handler goroutine indefinitely. The underlying
// HTTP clients have their own shorter timeouts (30s GitHub, 120s OpenRouter),
// and Bedrock relies on this context timeout rather than a fixed client timeout.
// This acts as an outer safety net.
const reviewTimeout = 3 * time.Minute

// ClientFactory creates GitHub API clients authenticated for a specific installation.
type ClientFactory func(installationID int64) review.GitHubClient

// ReviewRunner runs a code review for a push event.
type ReviewRunner interface {
	Review(ctx context.Context, gh review.GitHubClient, push ghub.PushContext, issueNum int, cfg config.Config) error
}

// issueRef identifies an issue in a specific repository, used as the key in
// the issueBranches reverse-lookup map. When HandleIssuesClosed is called, we
// need to find the debounce.Key (which contains the branch name) from just the
// issue number — this mapping provides that reverse lookup.
type issueRef struct {
	owner string
	repo  string
	issue int
}

// pushData holds the context needed to run a review after debounce.
type pushData struct {
	gh       review.GitHubClient
	push     ghub.PushContext
	issueNum int
	cfg      config.Config
}

// Handler routes webhook events to the appropriate action.
// It loads repo configuration, matches branches to issues,
// debounces rapid pushes, and triggers AI code reviews.
type Handler struct {
	clients ClientFactory
	engine  ReviewRunner
	buf     *debounce.Buffer
	logger  *slog.Logger
	ctx     context.Context
	cancel  context.CancelFunc

	mu            sync.Mutex
	issueBranches map[issueRef]debounce.Key
	stopOnce      sync.Once
}

// NewHandler creates a Handler with the given dependencies.
// The debounce buffer is created internally with the provided window duration.
func NewHandler(clients ClientFactory, engine ReviewRunner, debounceWindow time.Duration, logger *slog.Logger) *Handler {
	reviewCtx, cancel := context.WithCancel(context.Background()) //nolint:gosec // cancel is stored in Handler and called via Stop()
	h := &Handler{
		clients:       clients,
		engine:        engine,
		logger:        logger,
		ctx:           reviewCtx,
		cancel:        cancel,
		issueBranches: make(map[issueRef]debounce.Key),
	}
	h.buf = debounce.NewBuffer(debounceWindow, h.onDebounce)
	return h
}

// HandlePush processes a push event. It loads the repo config, matches the
// branch to an issue number, and queues a debounced review.
func (h *Handler) HandlePush(ctx context.Context, push ghub.PushContext) {
	if push.Branch == push.DefaultBranch {
		h.logger.Info("ignoring push to default branch", "branch", push.Branch)
		return
	}

	gh := h.clients(push.InstallationID)
	cfg, err := h.loadConfig(ctx, gh, push)
	if err != nil {
		h.logger.Warn("skipping review because repo config could not be loaded",
			"error", err,
			"branch", push.Branch,
			"repo", push.Owner+"/"+push.Repo,
		)
		return
	}

	issueNum, ok := cfg.MatchBranch(push.Branch)
	if !ok {
		h.logger.Info("branch does not match pattern", "branch", push.Branch)
		return
	}

	key := debounce.Key{Owner: push.Owner, Repo: push.Repo, Branch: push.Branch}

	h.mu.Lock()
	h.issueBranches[issueRef{owner: push.Owner, repo: push.Repo, issue: issueNum}] = key
	h.mu.Unlock()

	h.buf.Push(key, pushData{gh: gh, push: push, issueNum: issueNum, cfg: cfg})
	h.logger.Info("queued review", "branch", push.Branch, "issue", issueNum)
}

// HandleIssuesClosed cancels any pending review for the given issue.
func (h *Handler) HandleIssuesClosed(owner, repo string, issueNum int) {
	ref := issueRef{owner: owner, repo: repo, issue: issueNum}

	h.mu.Lock()
	key, ok := h.issueBranches[ref]
	if ok {
		delete(h.issueBranches, ref)
	}
	h.mu.Unlock()

	if ok {
		h.buf.Cancel(key)
		h.logger.Info("canceled review for closed issue", "issue", issueNum)
	}
}

// Stop cancels all pending reviews and any in-flight review work started from
// this handler. No further callbacks will fire. Safe to call multiple times.
func (h *Handler) Stop() {
	h.stopOnce.Do(func() {
		h.buf.Stop()
		h.cancel()
	})
}

// onDebounce is called when the debounce window expires for a push.
func (h *Handler) onDebounce(_ debounce.Key, data any) {
	pd, ok := data.(pushData)
	if !ok {
		h.logger.Error("unexpected data type in debounce callback", "type", fmt.Sprintf("%T", data))
		return
	}

	h.mu.Lock()
	delete(h.issueBranches, issueRef{owner: pd.push.Owner, repo: pd.push.Repo, issue: pd.issueNum})
	h.mu.Unlock()

	reviewCtx, cancel := context.WithTimeout(h.ctx, reviewTimeout)
	defer cancel()

	h.logger.Info("starting review", "branch", pd.push.Branch, "issue", pd.issueNum)
	if err := h.engine.Review(reviewCtx, pd.gh, pd.push, pd.issueNum, pd.cfg); err != nil {
		h.logger.Error("review failed",
			"error", err,
			"branch", pd.push.Branch,
			"issue", pd.issueNum,
		)
	}
}

// loadConfig fetches .froggr.yml from the repo and parses it.
// Returns defaults only when the file is genuinely absent (404).
// Invalid YAML and other fetch failures both return errors — froggr
// skips the review rather than silently changing review policy.
func (h *Handler) loadConfig(ctx context.Context, gh review.GitHubClient, push ghub.PushContext) (config.Config, error) {
	fc, err := gh.GetFileContent(ctx, push.Owner, push.Repo, froggrConfigPath, push.HeadSHA)
	if err != nil {
		if ghub.IsNotFound(err) {
			h.logger.Debug("no .froggr.yml found, using defaults")
			return config.Defaults(), nil
		}
		return config.Config{}, err
	}

	cfg, err := config.Parse([]byte(fc.Content))
	if err != nil {
		return config.Config{}, fmt.Errorf("invalid .froggr.yml: %w", err)
	}

	return cfg, nil
}
