package review

import (
	"fmt"
	"sort"
	"strings"

	"github.com/slb350/froggr/internal/ghub"
)

const shortSHALen = 7

// FormatComment formats a Result as a GitHub markdown comment.
func FormatComment(result Result, push ghub.PushContext) string {
	shortSHA := push.HeadSHA
	if len(shortSHA) > shortSHALen {
		shortSHA = shortSHA[:shortSHALen]
	}

	var b strings.Builder

	if result.IsClean {
		fmt.Fprintf(&b, "## froggr review: `%s` @ `%s`\n\n", push.Branch, shortSHA)
		b.WriteString("All clean! No bugs, security issues, or concerns found.\n\n")
		b.WriteString("This branch looks ready for a PR.\n")
		return b.String()
	}

	fmt.Fprintf(&b, "## froggr review: `%s` @ `%s`\n\n", push.Branch, shortSHA)
	fmt.Fprintf(&b, "Found **%d** issue(s). Push fixes and I'll review again.\n\n", len(result.Findings))

	// Sort: bugs first, then concerns.
	sorted := make([]Finding, len(result.Findings))
	copy(sorted, result.Findings)
	sort.SliceStable(sorted, func(i, j int) bool {
		return severityOrder(sorted[i].Severity) < severityOrder(sorted[j].Severity)
	})

	for _, f := range sorted {
		fmt.Fprintf(&b, "### %s: `%s:%d`\n%s\n\n", f.Severity, f.File, f.Line, f.Description)
	}

	return b.String()
}

// severityOrder returns a sort key for severity (lower = higher priority).
func severityOrder(s Severity) int {
	switch s {
	case SeverityBug:
		return 0
	case SeverityConcern:
		return 1
	default:
		return 2
	}
}
