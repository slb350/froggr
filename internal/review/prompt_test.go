package review

import (
	"strings"
	"testing"

	"github.com/slb350/froggr/internal/ghub"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSystemPrompt_ContainsReviewFocus(t *testing.T) {
	prompt := SystemPrompt()
	assert.Contains(t, prompt, "bug")
	assert.Contains(t, prompt, "security")
	assert.Contains(t, prompt, "edge case")
}

func TestSystemPrompt_ExcludesStyle(t *testing.T) {
	prompt := SystemPrompt()
	assert.NotContains(t, prompt, "style")
	assert.NotContains(t, prompt, "formatting")
	assert.NotContains(t, prompt, "naming convention")
}

func TestUserPrompt_IncludesIssueContext(t *testing.T) {
	rc := Context{
		Issue: ghub.IssueInfo{
			Number: 42,
			Title:  "Add authentication",
			Body:   "We need JWT auth",
		},
	}
	prompt, err := UserPrompt(rc)
	require.NoError(t, err)
	assert.Contains(t, prompt, "#42")
	assert.Contains(t, prompt, "Add authentication")
	assert.Contains(t, prompt, "We need JWT auth")
}

func TestUserPrompt_IncludesDiff(t *testing.T) {
	rc := Context{
		Issue: ghub.IssueInfo{Number: 1, Title: "T"},
		Diffs: []ghub.FileDiff{
			{Path: "src/auth.go", Status: "added", Patch: "+package auth\n+func Login() {}"},
		},
	}
	prompt, err := UserPrompt(rc)
	require.NoError(t, err)
	assert.Contains(t, prompt, "src/auth.go")
	assert.Contains(t, prompt, "+package auth")
}

func TestUserPrompt_IncludesFileContents(t *testing.T) {
	rc := Context{
		Issue: ghub.IssueInfo{Number: 1, Title: "T"},
		Files: []ghub.FileContent{
			{Path: "src/auth.go", Content: "package auth\n\nfunc Login() {}"},
		},
	}
	prompt, err := UserPrompt(rc)
	require.NoError(t, err)
	assert.Contains(t, prompt, "src/auth.go")
	assert.Contains(t, prompt, "func Login()")
}

func TestUserPrompt_IncludesPriorReviews(t *testing.T) {
	rc := Context{
		Issue:        ghub.IssueInfo{Number: 1, Title: "T"},
		PriorReviews: []string{"Previous review: found a bug in auth.go"},
	}
	prompt, err := UserPrompt(rc)
	require.NoError(t, err)
	assert.Contains(t, prompt, "Previous review")
	assert.Contains(t, prompt, "found a bug")
}

func TestUserPrompt_HandlesMissingPriors(t *testing.T) {
	rc := Context{
		Issue:        ghub.IssueInfo{Number: 1, Title: "T"},
		PriorReviews: nil,
	}
	prompt, err := UserPrompt(rc)
	require.NoError(t, err)
	assert.NotContains(t, prompt, "Prior Review")
}

func TestUserPrompt_HugeTitleExceedsBudgetReturnsError(t *testing.T) {
	rc := Context{
		Issue: ghub.IssueInfo{
			Number: 1,
			Title:  strings.Repeat("x", maxPromptChars+1),
		},
	}
	_, err := UserPrompt(rc)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "prompt budget")
}

func TestUserPrompt_NormalTitleFits(t *testing.T) {
	rc := Context{
		Issue: ghub.IssueInfo{
			Number: 1,
			Title:  "Short title",
		},
	}
	prompt, err := UserPrompt(rc)
	require.NoError(t, err)
	assert.Contains(t, prompt, "Short title")
}

func TestTruncateForPrompt_VerySmallLimit(t *testing.T) {
	result := truncateForPrompt("some content that is too long", 10)
	assert.Len(t, result, 10)
}

func TestTruncateForPrompt_ExactLimit(t *testing.T) {
	content := "exact"
	result := truncateForPrompt(content, len(content))
	assert.Equal(t, content, result)
}

func TestTruncateForPrompt_LimitSmallerThanNote(t *testing.T) {
	result := truncateForPrompt("long content here", 5)
	assert.Len(t, result, 5)
}

func TestUserPrompt_AppliesBudgetAndNotesOmissions(t *testing.T) {
	rc := Context{
		Issue: ghub.IssueInfo{
			Number: 1,
			Title:  "Large review",
			Body:   strings.Repeat("issue body ", 600),
		},
		Diffs: []ghub.FileDiff{
			{
				Path:   "src/large.go",
				Status: "modified",
				Patch:  strings.Repeat("+line\n", 3000),
			},
		},
		Files: []ghub.FileContent{
			{
				Path:    "src/large.go",
				Content: strings.Repeat("package main\n", 3000),
			},
		},
		PriorReviews:  []string{strings.Repeat("prior review\n", 600)},
		OmittedDiffs:  2,
		OmittedPriors: 1,
	}

	prompt, err := UserPrompt(rc)
	require.NoError(t, err)
	assert.LessOrEqual(t, len(prompt), maxPromptChars)
	assert.Contains(t, prompt, "truncated to stay within froggr review budget")
	assert.Contains(t, prompt, "omitted 2 additional diff file(s)")
	assert.Contains(t, prompt, "omitted 1 additional prior review(s)")
}
