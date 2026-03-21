package review

import (
	"context"

	"github.com/google/go-github/v84/github"
	"github.com/slb350/froggr/internal/ai"
	"github.com/slb350/froggr/internal/ghub"
)

// GitHubClient defines the GitHub API operations needed by the review engine.
// It is satisfied by *ghub.Client.
type GitHubClient interface {
	GetIssue(ctx context.Context, owner, repo string, number int) (ghub.IssueInfo, error)
	GetIssueComments(ctx context.Context, owner, repo string, number int) ([]*github.IssueComment, error)
	GetBranchDiff(ctx context.Context, owner, repo, base, head string) ([]ghub.FileDiff, error)
	GetFileContent(ctx context.Context, owner, repo, path, ref string) (ghub.FileContent, error)
	CreateIssueComment(ctx context.Context, owner, repo string, number int, body string) error
	CreateDraftPR(ctx context.Context, owner, repo, title, body, head, base string) (int, string, error)
}

// AIClient defines the AI completion operation needed by the review engine.
// The engine validates the request before calling Complete; implementations
// may validate defensively but are not required to.
type AIClient interface {
	Complete(ctx context.Context, req ai.CompletionRequest) (string, error)
}
