package review

import (
	"context"
	"errors"
	"fmt"

	"github.com/slb350/froggr/internal/ai"
	"github.com/slb350/froggr/internal/config"
	"github.com/slb350/froggr/internal/ghub"
)

// Engine orchestrates the full review flow: build context, prompt AI,
// parse response, format comment, post to GitHub, and optionally open a PR.
type Engine struct {
	providers map[config.Provider]AIClient
}

// NewEngine creates a review Engine with the given AI providers.
// The map keys are provider names (config.ProviderOpenRouter, config.ProviderBedrock)
// matching the Config.Provider field. The map is copied to prevent callers from
// mutating the engine's state. Panics if providers is empty or contains nil values.
func NewEngine(providers map[config.Provider]AIClient) *Engine {
	if len(providers) == 0 {
		panic("review.NewEngine: at least one AI provider is required")
	}
	copied := make(map[config.Provider]AIClient, len(providers))
	for k, v := range providers {
		if !k.Valid() {
			panic("review.NewEngine: invalid provider key " + string(k))
		}
		if v == nil {
			panic("review.NewEngine: nil provider for key " + string(k))
		}
		copied[k] = v
	}
	return &Engine{providers: copied}
}

// Review runs a full code review for the given push event and issue number.
// It posts the review as a comment on the issue. If the review is clean and
// auto_draft_pr is enabled, it also opens a draft PR.
func (e *Engine) Review(ctx context.Context, gh GitHubClient, push ghub.PushContext, issueNum int, cfg config.Config) error {
	if _, ok := e.providers[cfg.Provider]; !ok {
		return fmt.Errorf("AI provider %q not configured (check environment variables)", cfg.Provider)
	}

	rc, err := BuildContext(ctx, gh, push, issueNum, cfg)
	if err != nil {
		if errors.Is(err, ghub.ErrComparisonTooLarge) {
			return postSkippedReviewComment(ctx, gh, push, issueNum)
		}
		return fmt.Errorf("building context: %w", err)
	}

	if ctx.Err() != nil {
		return ctx.Err()
	}

	result, err := e.runAIReview(ctx, rc, cfg.Provider, cfg.Model)
	if err != nil {
		return err
	}

	comment := FormatComment(result, push)
	if err := gh.CreateIssueComment(ctx, push.Owner, push.Repo, issueNum, comment); err != nil {
		return fmt.Errorf("posting review comment: %w", err)
	}

	if result.IsClean && cfg.AutoDraftPR {
		return maybeCreateDraftPR(ctx, gh, push, rc.Issue.Title, issueNum)
	}

	return nil
}

// runAIReview builds the prompt, calls the AI, and parses the response.
// The caller (Review) must verify that the provider exists before calling.
func (e *Engine) runAIReview(ctx context.Context, rc Context, provider config.Provider, model string) (Result, error) {
	userPrompt, err := UserPrompt(rc)
	if err != nil {
		return Result{}, fmt.Errorf("building prompt: %w", err)
	}

	aiResponse, err := e.providers[provider].Complete(ctx, ai.CompletionRequest{
		Model: model,
		Messages: []ai.Message{
			{Role: ai.RoleSystem, Content: SystemPrompt()},
			{Role: ai.RoleUser, Content: userPrompt},
		},
	})
	if err != nil {
		return Result{}, fmt.Errorf("AI review: %w", err)
	}

	result, err := ParseResponse(aiResponse)
	if err != nil {
		return Result{}, fmt.Errorf("parsing AI response: %w", err)
	}

	return result, nil
}

func maybeCreateDraftPR(ctx context.Context, gh GitHubClient, push ghub.PushContext, issueTitle string, issueNum int) error {
	title := fmt.Sprintf("%s (froggr reviewed)", issueTitle)
	body := fmt.Sprintf("Closes #%d\n\nAuto-created by froggr after a clean review.", issueNum)
	if _, _, err := gh.CreateDraftPR(ctx, push.Owner, push.Repo, title, body, push.Branch, push.DefaultBranch); err != nil {
		return fmt.Errorf("creating draft PR: %w", err)
	}
	return nil
}

func postSkippedReviewComment(ctx context.Context, gh GitHubClient, push ghub.PushContext, issueNum int) error {
	comment := FormatSkippedComment(
		push,
		"GitHub only exposes up to 300 changed files for a branch comparison. This push is at or beyond that limit, so froggr will not pretend a partial diff was fully reviewed.\n\nSplit the branch into smaller changes or narrow the change set, then push again.",
	)
	if err := gh.CreateIssueComment(ctx, push.Owner, push.Repo, issueNum, comment); err != nil {
		return fmt.Errorf("posting skipped review comment: %w", err)
	}
	return nil
}
