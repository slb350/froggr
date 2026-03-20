package review

import (
	"context"
	"fmt"
	"strings"

	"github.com/slb350/froggr/internal/config"
	"github.com/slb350/froggr/internal/ghub"
)

const (
	froggrBotSuffix = "froggr[bot]"

	// Keep upstream GitHub fetches bounded so large pushes do not fan out into
	// dozens of content requests before prompt budgeting even starts.
	maxContextDiffFiles    = 25
	maxContextPriorReviews = 5
)

// Context holds all the information needed to build a review prompt.
type Context struct {
	Push          ghub.PushContext
	Issue         ghub.IssueInfo
	Diffs         []ghub.FileDiff
	Files         []ghub.FileContent
	PriorReviews  []string
	OmittedDiffs  int
	OmittedPriors int
}

// BuildContext fetches the diff, file contents, issue details, and prior
// froggr comments needed for a review. It returns an error if the issue
// is closed or if any required API call fails.
func BuildContext(ctx context.Context, gh GitHubClient, push ghub.PushContext, issueNum int, cfg config.Config) (Context, error) {
	issue, err := gh.GetIssue(ctx, push.Owner, push.Repo, issueNum)
	if err != nil {
		return Context{}, fmt.Errorf("fetching issue: %w", err)
	}

	if issue.State == "closed" {
		return Context{}, fmt.Errorf("issue #%d is closed, skipping review", issueNum)
	}

	diffs, err := gh.GetBranchDiff(ctx, push.Owner, push.Repo, push.DefaultBranch, push.HeadSHA)
	if err != nil {
		return Context{}, fmt.Errorf("fetching diff: %w", err)
	}

	filtered := filterDiffs(diffs, cfg)
	boundedDiffs, omittedDiffs := limitDiffs(filtered, maxContextDiffFiles)

	files, err := fetchFileContents(ctx, gh, push, boundedDiffs)
	if err != nil {
		return Context{}, fmt.Errorf("fetching file contents: %w", err)
	}

	priors, omittedPriors, err := fetchPriorReviews(ctx, gh, push, issueNum)
	if err != nil {
		return Context{}, fmt.Errorf("fetching prior reviews: %w", err)
	}

	return Context{
		Push:          push,
		Issue:         issue,
		Diffs:         boundedDiffs,
		Files:         files,
		PriorReviews:  priors,
		OmittedDiffs:  omittedDiffs,
		OmittedPriors: omittedPriors,
	}, nil
}

// filterDiffs removes diffs for paths that match the config's ignore patterns.
func filterDiffs(diffs []ghub.FileDiff, cfg config.Config) []ghub.FileDiff {
	result := make([]ghub.FileDiff, 0, len(diffs))
	for _, d := range diffs {
		if !cfg.ShouldIgnore(d.Path) {
			result = append(result, d)
		}
	}
	return result
}

// limitDiffs keeps the highest-priority diff context bounded. We keep the
// earliest compare results in order and surface the omission to the model later.
func limitDiffs(diffs []ghub.FileDiff, limit int) ([]ghub.FileDiff, int) {
	if len(diffs) <= limit {
		return diffs, 0
	}
	return diffs[:limit], len(diffs) - limit
}

// fetchFileContents fetches the content of each file that was added or modified.
func fetchFileContents(ctx context.Context, gh GitHubClient, push ghub.PushContext, diffs []ghub.FileDiff) ([]ghub.FileContent, error) {
	var files []ghub.FileContent
	for _, d := range diffs {
		if d.Status == "removed" {
			continue
		}
		fc, err := gh.GetFileContent(ctx, push.Owner, push.Repo, d.Path, push.HeadSHA)
		if err != nil {
			return nil, fmt.Errorf("fetching %s: %w", d.Path, err)
		}
		files = append(files, fc)
	}
	return files, nil
}

// fetchPriorReviews returns the bodies of prior froggr bot comments on the issue.
func fetchPriorReviews(ctx context.Context, gh GitHubClient, push ghub.PushContext, issueNum int) ([]string, int, error) {
	comments, err := gh.GetIssueComments(ctx, push.Owner, push.Repo, issueNum)
	if err != nil {
		return nil, 0, fmt.Errorf("fetching comments: %w", err)
	}

	var priors []string
	for _, c := range comments {
		if c.GetUser() != nil && strings.HasSuffix(c.GetUser().GetLogin(), froggrBotSuffix) {
			priors = append(priors, c.GetBody())
		}
	}

	if len(priors) <= maxContextPriorReviews {
		return priors, 0, nil
	}

	start := len(priors) - maxContextPriorReviews
	return priors[start:], start, nil
}
