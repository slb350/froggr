package review

import (
	"context"
	"fmt"
	"strings"

	"github.com/slb350/froggr/internal/config"
	"github.com/slb350/froggr/internal/ghub"
	"golang.org/x/sync/errgroup"
)

const (
	// froggrBotSuffix identifies froggr's own comments by matching the GitHub
	// App bot login suffix (e.g., "froggr[bot]").
	froggrBotSuffix = "froggr[bot]"

	// Keep upstream GitHub fetches bounded so large pushes do not fan out into
	// dozens of content requests before prompt budgeting even starts.
	// 25 files is large enough for typical feature branches while keeping API
	// usage and prompt size manageable.
	maxContextDiffFiles = 25
	// 5 prior reviews gives the model enough history to track what was fixed
	// without flooding the prompt. We keep the most recent N (not the first N)
	// because recent reviews are more relevant to the current push.
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
		return Context{}, fmt.Errorf("%w: issue #%d is closed, skipping review", errIssueClosed, issueNum)
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

// limitDiffs keeps diff context bounded by keeping the first N files from
// GitHub's compare response and dropping the rest. The omission count is
// surfaced to the model in the prompt. (Prior reviews use a different
// strategy: they keep the most recent N instead — see fetchPriorReviews.)
func limitDiffs(diffs []ghub.FileDiff, limit int) ([]ghub.FileDiff, int) {
	if len(diffs) <= limit {
		return diffs, 0
	}
	return diffs[:limit], len(diffs) - limit
}

// maxConcurrentFetches limits parallel GitHub content requests to avoid
// hitting secondary rate limits while still being significantly faster
// than sequential fetching.
const maxConcurrentFetches = 10

// fetchFileContents fetches the content of each non-removed file in parallel.
// Up to maxConcurrentFetches requests run concurrently. If any fetch fails the
// entire group is canceled and the first error is returned.
func fetchFileContents(ctx context.Context, gh GitHubClient, push ghub.PushContext, diffs []ghub.FileDiff) ([]ghub.FileContent, error) {
	// Build the list of files to fetch, skipping removed files (no content at HEAD).
	type indexedDiff struct {
		idx  int
		diff ghub.FileDiff
	}
	toFetch := make([]indexedDiff, 0, len(diffs))
	for _, d := range diffs {
		if d.Status != "removed" {
			toFetch = append(toFetch, indexedDiff{idx: len(toFetch), diff: d})
		}
	}
	if len(toFetch) == 0 {
		return nil, nil
	}

	// Each goroutine writes to a distinct index — no mutex needed.
	results := make([]ghub.FileContent, len(toFetch))

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(maxConcurrentFetches)

	for _, item := range toFetch {
		g.Go(func() error {
			fc, err := gh.GetFileContent(gctx, push.Owner, push.Repo, item.diff.Path, push.HeadSHA)
			if err != nil {
				return fmt.Errorf("fetching %s: %w", item.diff.Path, err)
			}
			results[item.idx] = fc
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}
	return results, nil
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

	// Keep the most recent N, not the first N. Recent reviews track what was
	// fixed vs. what remains, which is more useful context than early reviews
	// about code that may have been rewritten. (This differs from limitDiffs,
	// which keeps the first N — diffs are ordered by path, not time.)

	start := len(priors) - maxContextPriorReviews
	return priors[start:], start, nil
}
