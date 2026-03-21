package review

import (
	"errors"
	"strings"
	"testing"

	"github.com/slb350/froggr/internal/ghub"
	"github.com/stretchr/testify/assert"
)

func TestFormatComment_WithBugs(t *testing.T) {
	result := Result{
		Findings: []Finding{
			{Severity: SeverityBug, File: "src/auth.go", Line: 42, Description: "SQL injection"},
		},
	}
	push := ghub.PushContext{Branch: "42-add-auth", HeadSHA: "abc1234"}
	comment := FormatComment(result, push)

	assert.Contains(t, comment, "Bug")
	assert.Contains(t, comment, "src/auth.go")
	assert.Contains(t, comment, "SQL injection")
}

func TestFormatComment_MixedFindings(t *testing.T) {
	result := Result{
		Findings: []Finding{
			{Severity: SeverityConcern, File: "b.go", Line: 10, Description: "might be nil"},
			{Severity: SeverityBug, File: "a.go", Line: 5, Description: "definite bug"},
		},
	}
	push := ghub.PushContext{Branch: "42-fix", HeadSHA: "def5678"}
	comment := FormatComment(result, push)

	// Bugs should appear before concerns.
	bugIdx := strings.Index(comment, "definite bug")
	concernIdx := strings.Index(comment, "might be nil")
	assert.Greater(t, concernIdx, bugIdx, "bugs should appear before concerns")
}

func TestFormatComment_IncludesHeaderFooter(t *testing.T) {
	result := Result{
		Findings: []Finding{
			{Severity: SeverityBug, File: "x.go", Line: 1, Description: "issue"},
		},
	}
	push := ghub.PushContext{Branch: "42-feature", HeadSHA: "abc1234567890"}
	comment := FormatComment(result, push)

	assert.Contains(t, comment, "42-feature")
	assert.Contains(t, comment, "abc1234")
	assert.Contains(t, comment, "Push fixes")
}

func TestFormatCleanComment(t *testing.T) {
	result := Result{IsClean: true}
	push := ghub.PushContext{Branch: "42-clean", HeadSHA: "abc1234"}
	comment := FormatComment(result, push)

	assert.Contains(t, comment, "clean")
	assert.NotContains(t, comment, "Bug")
}

func TestFormatFailedComment(t *testing.T) {
	push := ghub.PushContext{Branch: "42-feature", HeadSHA: "abc1234567890"}
	comment := FormatFailedComment(push, errors.New("AI provider timeout"))

	assert.Contains(t, comment, "42-feature")
	assert.Contains(t, comment, "abc1234")
	assert.Contains(t, comment, "Review failed")
	assert.Contains(t, comment, "AI provider timeout")
	assert.Contains(t, comment, "Push again to retry")
}

func TestFormatSkippedComment(t *testing.T) {
	push := ghub.PushContext{Branch: "42-too-large", HeadSHA: "abc1234567890"}
	comment := FormatSkippedComment(push, "GitHub compare limit reached.")

	assert.Contains(t, comment, "42-too-large")
	assert.Contains(t, comment, "abc1234")
	assert.Contains(t, comment, "Review skipped")
	assert.Contains(t, comment, "GitHub compare limit reached.")
}
