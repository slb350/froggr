package review

import (
	"strings"
	"testing"

	"github.com/slb350/froggr/internal/ghub"
	"github.com/stretchr/testify/assert"
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
	prompt := UserPrompt(rc)
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
	prompt := UserPrompt(rc)
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
	prompt := UserPrompt(rc)
	assert.Contains(t, prompt, "src/auth.go")
	assert.Contains(t, prompt, "func Login()")
}

func TestUserPrompt_IncludesPriorReviews(t *testing.T) {
	rc := Context{
		Issue:        ghub.IssueInfo{Number: 1, Title: "T"},
		PriorReviews: []string{"Previous review: found a bug in auth.go"},
	}
	prompt := UserPrompt(rc)
	assert.Contains(t, prompt, "Previous review")
	assert.Contains(t, prompt, "found a bug")
}

func TestUserPrompt_HandlesMissingPriors(t *testing.T) {
	rc := Context{
		Issue:        ghub.IssueInfo{Number: 1, Title: "T"},
		PriorReviews: nil,
	}
	prompt := UserPrompt(rc)
	// Should not contain a prior reviews section.
	assert.NotContains(t, prompt, "Prior Review")
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

	prompt := UserPrompt(rc)
	assert.LessOrEqual(t, len(prompt), maxPromptChars)
	assert.Contains(t, prompt, "truncated to stay within froggr review budget")
	assert.Contains(t, prompt, "omitted 2 additional diff file(s)")
	assert.Contains(t, prompt, "omitted 1 additional prior review(s)")
}
