package review

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	// Prompt budgeting is deliberate: bounded input keeps review latency and
	// cost predictable, and avoids silent provider-side truncation on big pushes.

	// maxPromptBytes caps total prompt size (~30k tokens for most tokenizers).
	maxPromptBytes = 120000
	// maxIssueBodyBytes limits the issue description included for context.
	maxIssueBodyBytes = 4000
	// maxDiffPatchBytes limits each individual file diff patch.
	maxDiffPatchBytes = 8000
	// maxFileContentBytes limits each full file body fetched at HEAD.
	maxFileContentBytes = 12000
	// maxPriorReviewBytes limits each prior froggr review comment.
	maxPriorReviewBytes  = 4000
	promptTruncationNote = "\n... [truncated to stay within froggr review budget]\n"
)

// systemPrompt is the system prompt that defines froggr's review focus.
// Allocated once at package init to avoid rebuilding on every review.
var systemPrompt = `You are a code reviewer. Your job is to find bugs, security issues, logic errors, and missing edge cases in code changes.

You should focus on:
- Definite bugs and logic errors
- Security vulnerabilities (injection, auth bypass, data exposure)
- Missing edge case handling (nil/null checks, empty inputs, boundary conditions)
- Race conditions or concurrency issues
- Resource leaks (unclosed connections, file handles)

Do NOT comment on:
- Code structure or organization
- Variable or function names
- Whitespace or indentation
- Import ordering

Respond with a JSON array of findings. Each finding has these fields:
- "severity": either "Bug" or "Concern"
- "file": the file path
- "line": the approximate line number (from the diff)
- "description": a clear, actionable description of the issue

If the code looks clean with no issues, respond with an empty JSON array: []

Example response:
[
  {"severity": "Bug", "file": "src/auth.go", "line": 42, "description": "Password comparison uses == instead of constant-time comparison, enabling timing attacks."},
  {"severity": "Concern", "file": "src/handler.go", "line": 15, "description": "HTTP request body is not limited in size, which could lead to memory exhaustion."}
]`

// SystemPrompt returns the system prompt that defines froggr's review focus.
// It directs the AI to find bugs, security issues, and edge cases — not style.
func SystemPrompt() string {
	return systemPrompt
}

// UserPrompt builds the user prompt from the review context.
// It returns an error if the prompt budget is too small to include
// the issue title or any code context (diffs or files).
func UserPrompt(rc Context) (string, error) {
	budget := newPromptBudget(maxPromptBytes)

	if !budget.Write(fmt.Sprintf("## Issue #%d: %s\n\n", rc.Issue.Number, rc.Issue.Title)) {
		return "", fmt.Errorf("issue title exceeds prompt budget (%d bytes)", maxPromptBytes)
	}
	if rc.Issue.Body != "" {
		_ = budget.Write(truncateForPrompt(rc.Issue.Body, maxIssueBodyBytes) + "\n\n")
	}

	diffsWritten := writeDiffSection(budget, rc)
	filesWritten := writeFileSection(budget, rc)
	writePriorReviewSection(budget, rc)

	hasCodeContext := len(rc.Diffs) > 0 || len(rc.Files) > 0
	if hasCodeContext && !diffsWritten && !filesWritten {
		return "", fmt.Errorf("prompt budget exhausted before any code context could be included")
	}

	return budget.String(), nil
}

// writeDiffSection writes diff context into the prompt budget.
// Returns true if at least one diff was written.
func writeDiffSection(budget *promptBudget, rc Context) bool {
	if len(rc.Diffs) == 0 {
		return false
	}

	_ = budget.Write("## Diff\n\n")
	written := 0
	omitted := rc.OmittedDiffs
	for i, d := range rc.Diffs {
		patch := d.Patch
		if patch == "" {
			patch = "[patch unavailable]"
		}

		chunk := fmt.Sprintf(
			"### %s (%s)\n```diff\n%s\n```\n\n",
			d.Path,
			d.Status,
			truncateForPrompt(patch, maxDiffPatchBytes),
		)
		if !budget.Write(chunk) {
			omitted += len(rc.Diffs) - i
			break
		}
		written++
	}
	writeBudgetNote(budget, omitted, "diff file")
	return written > 0
}

// writeFileSection writes file contents into the prompt budget.
// Returns true if at least one file was written.
func writeFileSection(budget *promptBudget, rc Context) bool {
	if len(rc.Files) == 0 {
		return false
	}

	_ = budget.Write("## Full File Contents\n\n")
	written := 0
	omitted := 0
	for i, f := range rc.Files {
		chunk := fmt.Sprintf(
			"### %s\n```\n%s\n```\n\n",
			f.Path,
			truncateForPrompt(f.Content, maxFileContentBytes),
		)
		if !budget.Write(chunk) {
			omitted = len(rc.Files) - i
			break
		}
		written++
	}
	writeBudgetNote(budget, omitted, "full file content block")
	return written > 0
}

func writePriorReviewSection(budget *promptBudget, rc Context) {
	if len(rc.PriorReviews) == 0 {
		return
	}

	_ = budget.Write("## Prior Reviews\n\n")
	omitted := rc.OmittedPriors
	for i, r := range rc.PriorReviews {
		chunk := fmt.Sprintf(
			"### Prior Review %d\n%s\n\n",
			i+1,
			truncateForPrompt(r, maxPriorReviewBytes),
		)
		if !budget.Write(chunk) {
			omitted += len(rc.PriorReviews) - i
			break
		}
	}
	writeBudgetNote(budget, omitted, "prior review")
}

// promptBudget tracks remaining byte capacity for a prompt under
// construction. Write() accepts or rejects whole chunks atomically so we
// never cut through markdown fences, headings, or code blocks mid-section.
type promptBudget struct {
	b         strings.Builder
	remaining int
}

func newPromptBudget(limit int) *promptBudget {
	p := &promptBudget{remaining: limit}
	p.b.Grow(limit)
	return p
}

// Write only appends full chunks so we never cut through markdown fences or
// headings mid-section when the prompt budget is exhausted.
func (p *promptBudget) Write(chunk string) bool {
	if len(chunk) > p.remaining {
		return false
	}
	p.b.WriteString(chunk)
	p.remaining -= len(chunk)
	return true
}

func (p *promptBudget) String() string {
	return p.b.String()
}

// truncateForPrompt trims s to fit in limit bytes, appending a
// truncation note so the model knows content was cut.
func truncateForPrompt(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	if limit <= 0 {
		return ""
	}
	if limit <= len(promptTruncationNote) {
		return utf8SafePrefix(promptTruncationNote, limit)
	}

	keep := limit - len(promptTruncationNote)
	return utf8SafePrefix(s, keep) + promptTruncationNote
}

func utf8SafePrefix(s string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if len(s) <= limit {
		return s
	}
	for limit > 0 && limit < len(s) && !utf8.RuneStart(s[limit]) {
		limit--
	}
	return s[:limit]
}

// writeBudgetNote appends an italicized context note to the prompt when
// items were omitted due to budget constraints, so the model knows context
// is incomplete.
func writeBudgetNote(budget *promptBudget, omitted int, label string) {
	if omitted <= 0 {
		return
	}

	note := fmt.Sprintf(
		"_Context note: omitted %d additional %s(s) to stay within the froggr review budget._\n\n",
		omitted,
		label,
	)
	_ = budget.Write(note)
}
