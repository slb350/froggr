package ghub

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/go-github/v84/github"
)

// githubCompareFileLimit is the maximum number of files GitHub's compare API
// returns. Beyond this, changed files are silently omitted from the response.
// See: https://docs.github.com/en/rest/commits/commits#compare-two-commits
const githubCompareFileLimit = 300

// ErrComparisonTooLarge indicates the comparison has reached GitHub's 300-file
// limit, meaning additional changed files may exist that froggr cannot see.
// froggr fails closed rather than silently reviewing a partial diff.
var ErrComparisonTooLarge = errors.New("comparison reached GitHub's 300-file limit")

// errPRNotFound is returned by findExistingPullRequest when no matching PR exists.
var errPRNotFound = errors.New("no matching pull request found")

// Client wraps a go-github client and provides the GitHub API operations
// needed by froggr's review engine.
type Client struct {
	gh *github.Client
}

// NewClient creates a Client from an authenticated go-github client.
func NewClient(gh *github.Client) *Client {
	return &Client{gh: gh}
}

// IsNotFound reports whether an error ultimately came from a GitHub 404.
func IsNotFound(err error) bool {
	var ghErr *github.ErrorResponse
	return errors.As(err, &ghErr) && ghErr.Response != nil && ghErr.Response.StatusCode == 404
}

// GetIssue fetches a single issue by number.
func (c *Client) GetIssue(ctx context.Context, owner, repo string, number int) (IssueInfo, error) {
	issue, _, err := c.gh.Issues.Get(ctx, owner, repo, number)
	if err != nil {
		return IssueInfo{}, fmt.Errorf("getting issue #%d: %w", number, err)
	}

	return IssueInfo{
		Number: issue.GetNumber(),
		Title:  issue.GetTitle(),
		Body:   issue.GetBody(),
		State:  issue.GetState(),
	}, nil
}

// GetIssueComments returns all comments on an issue, paginating through all
// results to ensure none are missed.
func (c *Client) GetIssueComments(ctx context.Context, owner, repo string, number int) ([]*github.IssueComment, error) {
	opts := &github.IssueListCommentsOptions{
		ListOptions: github.ListOptions{PerPage: 100},
	}

	var all []*github.IssueComment
	for {
		comments, resp, err := c.gh.Issues.ListComments(ctx, owner, repo, number, opts)
		if err != nil {
			return nil, fmt.Errorf("listing comments for issue #%d: %w", number, err)
		}
		all = append(all, comments...)
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return all, nil
}

// GetBranchDiff returns the file diffs between base and head refs.
func (c *Client) GetBranchDiff(ctx context.Context, owner, repo, base, head string) ([]FileDiff, error) {
	// Minimize commit history in the response. GitHub's compare endpoint always
	// includes the changed-file list (capped at 300) regardless of pagination, so
	// PerPage only controls how many commits are returned per page.
	opts := &github.ListOptions{Page: 1, PerPage: 1}
	comparison, _, err := c.gh.Repositories.CompareCommits(ctx, owner, repo, base, head, opts)
	if err != nil {
		return nil, fmt.Errorf("comparing %s...%s: %w", base, head, err)
	}

	// GitHub documents that compare responses expose at most 300 changed files.
	// At the ceiling we can no longer prove the review is complete, so fail closed
	// instead of silently reviewing a partial diff.
	if len(comparison.Files) >= githubCompareFileLimit {
		return nil, fmt.Errorf("%w: %s...%s touches at least %d files", ErrComparisonTooLarge, base, head, githubCompareFileLimit)
	}

	diffs := make([]FileDiff, 0, len(comparison.Files))
	for _, f := range comparison.Files {
		if f == nil {
			continue
		}
		diffs = append(diffs, FileDiff{
			Path:   f.GetFilename(),
			Status: f.GetStatus(),
			Patch:  f.GetPatch(),
		})
	}
	return diffs, nil
}

// GetFileContent fetches the decoded content of a file at a specific ref.
func (c *Client) GetFileContent(ctx context.Context, owner, repo, path, ref string) (FileContent, error) {
	opts := &github.RepositoryContentGetOptions{Ref: ref}
	fc, _, _, err := c.gh.Repositories.GetContents(ctx, owner, repo, path, opts)
	if err != nil {
		return FileContent{}, fmt.Errorf("getting content for %s@%s: %w", path, ref, err)
	}
	// GetContents returns nil for the file content when the path is a directory.
	if fc == nil {
		return FileContent{}, fmt.Errorf("path %s is a directory, not a file", path)
	}

	content, err := fc.GetContent()
	if err != nil {
		return FileContent{}, fmt.Errorf("decoding content for %s: %w", path, err)
	}

	return FileContent{
		Path:    fc.GetPath(),
		Content: content,
	}, nil
}

// CreateIssueComment posts a comment on an issue.
func (c *Client) CreateIssueComment(ctx context.Context, owner, repo string, number int, body string) error {
	comment := &github.IssueComment{Body: github.Ptr(body)}
	_, _, err := c.gh.Issues.CreateComment(ctx, owner, repo, number, comment)
	if err != nil {
		return fmt.Errorf("creating comment on issue #%d: %w", number, err)
	}
	return nil
}

// CreateDraftPR creates a draft pull request. Returns the PR number and URL.
// If a PR already exists for the same head→base pair (common when froggr
// re-reviews a branch that was already marked clean), it returns the existing
// PR's details instead of failing. This makes the operation idempotent.
func (c *Client) CreateDraftPR(ctx context.Context, owner, repo, title, body, head, base string) (int, string, error) {
	pr := &github.NewPullRequest{
		Title: github.Ptr(title),
		Body:  github.Ptr(body),
		Head:  github.Ptr(head),
		Base:  github.Ptr(base),
		Draft: github.Ptr(true),
	}

	created, _, err := c.gh.PullRequests.Create(ctx, owner, repo, pr)
	if err != nil {
		if isAlreadyExistsPRError(err) {
			// Treat duplicate draft/open PR creation as an idempotent success path.
			existing, lookupErr := c.findExistingPullRequest(ctx, owner, repo, head, base)
			if errors.Is(lookupErr, errPRNotFound) {
				// GitHub reported "already exists" but the lookup returned nothing.
				// This can indicate a race (PR closed between error and lookup),
				// permissions on the list endpoint, or API inconsistency.
				return 0, "", fmt.Errorf("creating draft PR: %w (existing PR lookup returned no results)", err)
			}
			if lookupErr != nil {
				return 0, "", fmt.Errorf("creating draft PR: %w; looking up existing PR: %v", err, lookupErr)
			}
			return existing.GetNumber(), existing.GetHTMLURL(), nil
		}
		return 0, "", fmt.Errorf("creating draft PR: %w", err)
	}

	return created.GetNumber(), created.GetHTMLURL(), nil
}

// isAlreadyExistsPRError detects GitHub's 422 "already exists" error, which
// can appear as an error code or in the message text depending on API version.
func isAlreadyExistsPRError(err error) bool {
	var ghErr *github.ErrorResponse
	if !errors.As(err, &ghErr) || ghErr.Response == nil || ghErr.Response.StatusCode != 422 {
		return false
	}

	for _, detail := range ghErr.Errors {
		if detail.Code == "already_exists" {
			return true
		}
		if strings.Contains(strings.ToLower(detail.Message), "already exists") {
			return true
		}
	}

	return strings.Contains(strings.ToLower(ghErr.Message), "already exists")
}

// findExistingPullRequest looks up an open PR matching head→base. Only called
// after CreateDraftPR fails with "already exists" to recover the PR details.
func (c *Client) findExistingPullRequest(ctx context.Context, owner, repo, head, base string) (*github.PullRequest, error) {
	opts := &github.PullRequestListOptions{
		State: "open",
		Head:  owner + ":" + head,
		Base:  base,
		ListOptions: github.ListOptions{
			Page:    1,
			PerPage: 1,
		},
	}

	prs, _, err := c.gh.PullRequests.List(ctx, owner, repo, opts)
	if err != nil {
		return nil, fmt.Errorf("listing pull requests for %s:%s -> %s: %w", owner, head, base, err)
	}
	if len(prs) == 0 || prs[0] == nil {
		return nil, errPRNotFound
	}
	return prs[0], nil
}
