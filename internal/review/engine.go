package review

import (
	"context"
	"fmt"

	"github.com/slb350/froggr/internal/config"
	"github.com/slb350/froggr/internal/ghub"
	"github.com/slb350/froggr/internal/openrouter"
)

// Engine orchestrates the full review flow: build context, prompt AI,
// parse response, format comment, post to GitHub, and optionally open a PR.
type Engine struct {
	ai AIClient
}

// NewEngine creates a review Engine with the given AI client.
func NewEngine(ai AIClient) *Engine {
	return &Engine{ai: ai}
}

// Review runs a full code review for the given push event and issue number.
// It posts the review as a comment on the issue. If the review is clean and
// auto_draft_pr is enabled, it also opens a draft PR.
func (e *Engine) Review(ctx context.Context, gh GitHubClient, push ghub.PushContext, issueNum int, cfg config.Config) error {
	rc, err := BuildContext(ctx, gh, push, issueNum, cfg)
	if err != nil {
		return fmt.Errorf("building context: %w", err)
	}

	aiResponse, err := e.ai.Complete(ctx, openrouter.CompletionRequest{
		Model: cfg.Model,
		Messages: []openrouter.Message{
			{Role: "system", Content: SystemPrompt()},
			{Role: "user", Content: UserPrompt(rc)},
		},
	})
	if err != nil {
		return fmt.Errorf("AI review: %w", err)
	}

	result, err := ParseResponse(aiResponse)
	if err != nil {
		return fmt.Errorf("parsing AI response: %w", err)
	}

	comment := FormatComment(result, push)
	if err := gh.CreateIssueComment(ctx, push.Owner, push.Repo, issueNum, comment); err != nil {
		return fmt.Errorf("posting review comment: %w", err)
	}

	if result.IsClean && cfg.AutoDraftPR {
		title := fmt.Sprintf("%s (froggr reviewed)", rc.Issue.Title)
		body := fmt.Sprintf("Closes #%d\n\nAuto-created by froggr after a clean review.", issueNum)
		if _, _, err := gh.CreateDraftPR(ctx, push.Owner, push.Repo, title, body, push.Branch, push.DefaultBranch); err != nil {
			return fmt.Errorf("creating draft PR: %w", err)
		}
	}

	return nil
}
