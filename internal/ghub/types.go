// Package ghub provides GitHub App authentication, webhook handling, and API operations.
package ghub

// PushContext holds the extracted context from a GitHub push event.
type PushContext struct {
	InstallationID int64
	Owner          string
	Repo           string
	Branch         string
	HeadSHA        string
	DefaultBranch  string
}

// IssueInfo holds the relevant fields from a GitHub issue.
type IssueInfo struct {
	Number int
	Title  string
	Body   string
	State  string
}

// FileDiff represents a single file change in a commit comparison.
type FileDiff struct {
	Path   string
	Status string
	Patch  string
}

// FileContent holds the decoded content of a file at a specific ref.
type FileContent struct {
	Path    string
	Content string
}
