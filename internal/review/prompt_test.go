package review

import (
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
