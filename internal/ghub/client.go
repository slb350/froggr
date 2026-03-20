package ghub

import (
	"context"
	"fmt"

	"github.com/google/go-github/v84/github"
)

// Client wraps a go-github client and provides the GitHub API operations
// needed by froggr's review engine.
type Client struct {
	gh *github.Client
}

// NewClient creates a Client from an authenticated go-github client.
func NewClient(gh *github.Client) *Client {
	return &Client{gh: gh}
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
	comparison, _, err := c.gh.Repositories.CompareCommits(ctx, owner, repo, base, head, nil)
	if err != nil {
		return nil, fmt.Errorf("comparing %s...%s: %w", base, head, err)
	}

	diffs := make([]FileDiff, 0, len(comparison.Files))
	for _, f := range comparison.Files {
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
		return 0, "", fmt.Errorf("creating draft PR: %w", err)
	}

	return created.GetNumber(), created.GetHTMLURL(), nil
}
