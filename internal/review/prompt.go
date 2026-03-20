package review

import (
	"fmt"
	"strings"
)

// SystemPrompt returns the system prompt that defines froggr's review focus.
// It directs the AI to find bugs, security issues, and edge cases — not style.
func SystemPrompt() string {
	return `You are a code reviewer. Your job is to find bugs, security issues, logic errors, and missing edge cases in code changes.

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
}

// UserPrompt builds the user prompt from the review context.
func UserPrompt(rc Context) string {
	var b strings.Builder

	fmt.Fprintf(&b, "## Issue #%d: %s\n\n", rc.Issue.Number, rc.Issue.Title)
	if rc.Issue.Body != "" {
		fmt.Fprintf(&b, "%s\n\n", rc.Issue.Body)
	}

	if len(rc.Diffs) > 0 {
		b.WriteString("## Diff\n\n")
		for _, d := range rc.Diffs {
			fmt.Fprintf(&b, "### %s (%s)\n```diff\n%s\n```\n\n", d.Path, d.Status, d.Patch)
		}
	}

	if len(rc.Files) > 0 {
		b.WriteString("## Full File Contents\n\n")
		for _, f := range rc.Files {
			fmt.Fprintf(&b, "### %s\n```\n%s\n```\n\n", f.Path, f.Content)
		}
	}

	if len(rc.PriorReviews) > 0 {
		b.WriteString("## Prior Reviews\n\n")
		for i, r := range rc.PriorReviews {
			fmt.Fprintf(&b, "### Prior Review %d\n%s\n\n", i+1, r)
		}
	}

	return b.String()
}
