// Package review provides the AI-powered code review engine.
// It orchestrates fetching context from GitHub, building prompts,
// calling the AI, and formatting results.
package review

// Severity indicates how serious a finding is.
type Severity string

const (
	// SeverityBug indicates a definite bug, security issue, or logic error.
	SeverityBug Severity = "Bug"
	// SeverityConcern indicates a potential issue worth investigating.
	SeverityConcern Severity = "Concern"
)

// Finding represents a single issue found during code review.
type Finding struct {
	Severity    Severity
	File        string
	Line        int
	Description string
}

// Result holds the structured output of a code review.
type Result struct {
	Findings []Finding
	IsClean  bool
}
