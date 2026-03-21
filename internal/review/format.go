package review

import (
	"fmt"
	"sort"
	"strings"

	"github.com/slb350/froggr/internal/ghub"
)

const shortSHALen = 7

// writeHeader writes the standard froggr review comment header.
func writeHeader(b *strings.Builder, push ghub.PushContext) {
	fmt.Fprintf(b, "## froggr review: `%s` @ `%s`\n\n", push.Branch, shortSHA(push.HeadSHA))
}

// FormatComment formats a Result as a GitHub markdown comment.
func FormatComment(result Result, push ghub.PushContext) string {
	var b strings.Builder
	writeHeader(&b, push)

	if result.IsClean {
		b.WriteString("All clean! No bugs, security issues, or concerns found.\n\n")
		b.WriteString("This branch looks ready for a PR.\n")
		return b.String()
	}

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

// FormatSkippedComment explains why froggr deliberately skipped a review.
func FormatSkippedComment(push ghub.PushContext, reason string) string {
	var b strings.Builder
	writeHeader(&b, push)
	b.WriteString("Review skipped.\n\n")
	b.WriteString(reason)
	b.WriteString("\n")
	return b.String()
}

// FormatFailedComment explains that froggr attempted but failed to review a push.
func FormatFailedComment(push ghub.PushContext, reviewErr error) string {
	var b strings.Builder
	writeHeader(&b, push)
	fmt.Fprintf(&b, "Review failed: %s\n\n", reviewErr)
	b.WriteString("Push again to retry.\n")
	return b.String()
}

func shortSHA(sha string) string {
	if len(sha) > shortSHALen {
		return sha[:shortSHALen]
	}
	return sha
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
