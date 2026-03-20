package ghub

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/go-github/v84/github"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestGHClient(t *testing.T, mux *http.ServeMux) *Client {
	t.Helper()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ghClient := github.NewClient(nil)
	ghClient.BaseURL, _ = ghClient.BaseURL.Parse(srv.URL + "/")
	return NewClient(ghClient)
}

func TestGetIssue_Success(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/owner/repo/issues/42", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(github.Issue{
			Number: github.Ptr(42),
			Title:  github.Ptr("Add authentication"),
			Body:   github.Ptr("We need auth"),
			State:  github.Ptr("open"),
		})
	})

	c := newTestGHClient(t, mux)
	issue, err := c.GetIssue(context.Background(), "owner", "repo", 42)
	require.NoError(t, err)
	assert.Equal(t, 42, issue.Number)
	assert.Equal(t, "Add authentication", issue.Title)
	assert.Equal(t, "We need auth", issue.Body)
	assert.Equal(t, "open", issue.State)
}

func TestGetIssue_NotFound(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/owner/repo/issues/999", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"message": "Not Found"})
	})

	c := newTestGHClient(t, mux)
	_, err := c.GetIssue(context.Background(), "owner", "repo", 999)
	require.Error(t, err)
}

func TestGetIssue_Closed(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/owner/repo/issues/42", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(github.Issue{
			Number: github.Ptr(42),
			Title:  github.Ptr("Done"),
			State:  github.Ptr("closed"),
		})
	})

	c := newTestGHClient(t, mux)
	issue, err := c.GetIssue(context.Background(), "owner", "repo", 42)
	require.NoError(t, err)
	assert.Equal(t, "closed", issue.State)
}

func TestGetBranchDiff_Success(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/owner/repo/compare/main...feature", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(github.CommitsComparison{
			Files: []*github.CommitFile{
				{
					Filename: github.Ptr("src/auth.go"),
					Status:   github.Ptr("added"),
					Patch:    github.Ptr("@@ -0,0 +1,10 @@\n+package auth"),
				},
				{
					Filename: github.Ptr("src/main.go"),
					Status:   github.Ptr("modified"),
					Patch:    github.Ptr("@@ -1,3 +1,5 @@"),
				},
			},
		})
	})

	c := newTestGHClient(t, mux)
	diffs, err := c.GetBranchDiff(context.Background(), "owner", "repo", "main", "feature")
	require.NoError(t, err)
	require.Len(t, diffs, 2)
	assert.Equal(t, "src/auth.go", diffs[0].Path)
	assert.Equal(t, "added", diffs[0].Status)
	assert.Contains(t, diffs[0].Patch, "package auth")
	assert.Equal(t, "src/main.go", diffs[1].Path)
}

func TestGetBranchDiff_NoChanges(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/owner/repo/compare/main...feature", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(github.CommitsComparison{
			Files: []*github.CommitFile{},
		})
	})

	c := newTestGHClient(t, mux)
	diffs, err := c.GetBranchDiff(context.Background(), "owner", "repo", "main", "feature")
	require.NoError(t, err)
	assert.Empty(t, diffs)
}

func TestGetFileContent_Success(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/owner/repo/contents/src/auth.go", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "abc123", r.URL.Query().Get("ref"))
		_ = json.NewEncoder(w).Encode(github.RepositoryContent{
			Path:     github.Ptr("src/auth.go"),
			Encoding: github.Ptr("base64"),
			Content:  github.Ptr("cGFja2FnZSBhdXRo"), // "package auth" in base64
		})
	})

	c := newTestGHClient(t, mux)
	fc, err := c.GetFileContent(context.Background(), "owner", "repo", "src/auth.go", "abc123")
	require.NoError(t, err)
	assert.Equal(t, "src/auth.go", fc.Path)
	assert.Equal(t, "package auth", fc.Content)
}

func TestGetFileContent_NotFound(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/owner/repo/contents/missing.go", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"message": "Not Found"})
	})

	c := newTestGHClient(t, mux)
	_, err := c.GetFileContent(context.Background(), "owner", "repo", "missing.go", "abc123")
	require.Error(t, err)
}

func TestCreateIssueComment_Success(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /repos/owner/repo/issues/42/comments", func(w http.ResponseWriter, r *http.Request) {
		var comment github.IssueComment
		require.NoError(t, json.NewDecoder(r.Body).Decode(&comment))
		assert.Equal(t, "Review comment", comment.GetBody())

		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(github.IssueComment{
			ID:   github.Ptr(int64(1)),
			Body: comment.Body,
		})
	})

	c := newTestGHClient(t, mux)
	err := c.CreateIssueComment(context.Background(), "owner", "repo", 42, "Review comment")
	require.NoError(t, err)
}

func TestCreateDraftPR_Success(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /repos/owner/repo/pulls", func(w http.ResponseWriter, r *http.Request) {
		var pr github.NewPullRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&pr))
		assert.Equal(t, "Add auth", pr.GetTitle())
		assert.Equal(t, "feature", pr.GetHead())
		assert.Equal(t, "main", pr.GetBase())
		assert.True(t, pr.GetDraft())

		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(github.PullRequest{
			Number:  github.Ptr(99),
			HTMLURL: github.Ptr("https://github.com/owner/repo/pull/99"),
		})
	})

	c := newTestGHClient(t, mux)
	num, url, err := c.CreateDraftPR(context.Background(), "owner", "repo", "Add auth", "Closes #42", "feature", "main")
	require.NoError(t, err)
	assert.Equal(t, 99, num)
	assert.Equal(t, "https://github.com/owner/repo/pull/99", url)
}

func TestCreateDraftPR_AlreadyExists(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /repos/owner/repo/pulls", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"message": "Validation Failed",
			"errors":  []map[string]string{{"message": "A pull request already exists"}},
		})
	})

	c := newTestGHClient(t, mux)
	_, _, err := c.CreateDraftPR(context.Background(), "owner", "repo", "Add auth", "body", "feature", "main")
	require.Error(t, err)
}

func TestGetIssueComments_Success(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/owner/repo/issues/42/comments", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]*github.IssueComment{
			{ID: github.Ptr(int64(1)), Body: github.Ptr("First comment")},
			{ID: github.Ptr(int64(2)), Body: github.Ptr("Second comment")},
		})
	})

	c := newTestGHClient(t, mux)
	comments, err := c.GetIssueComments(context.Background(), "owner", "repo", 42)
	require.NoError(t, err)
	require.Len(t, comments, 2)
	assert.Equal(t, "First comment", comments[0].GetBody())
}
