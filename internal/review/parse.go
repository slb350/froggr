package review

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// findingJSON is the wire format for a finding in the AI response.
type findingJSON struct {
	Severity    string `json:"severity"`
	File        string `json:"file"`
	Line        int    `json:"line"`
	Description string `json:"description"`
}

// textFindingPattern matches lines like:
// **Bug** in src/auth.go line 42: SQL injection vulnerability
var textFindingPattern = regexp.MustCompile(
	`\*\*(Bug|Concern)\*\*\s+in\s+(\S+)\s+line\s+(\d+):\s*(.+)`,
)

// ParseResponse parses an AI response into a Result.
// It tries JSON first (bare or markdown-fenced), then falls back
// to text pattern matching.
func ParseResponse(response string) (Result, error) {
	response = strings.TrimSpace(response)
	if response == "" {
		return Result{}, fmt.Errorf("empty AI response")
	}

	// Try JSON parsing first.
	if findings, ok := tryParseJSON(response); ok {
		return buildResult(findings), nil
	}

	// Try extracting JSON from markdown code fence.
	if extracted := extractFencedJSON(response); extracted != "" {
		if findings, ok := tryParseJSON(extracted); ok {
			return buildResult(findings), nil
		}
	}

	// Fall back to text pattern matching.
	findings := parseTextFindings(response)
	return buildResult(findings), nil
}

// tryParseJSON attempts to parse the response as a JSON array of findings.
func tryParseJSON(s string) ([]Finding, bool) {
	var raw []findingJSON
	if err := json.Unmarshal([]byte(s), &raw); err != nil {
		return nil, false
	}

	findings := make([]Finding, 0, len(raw))
	for _, f := range raw {
		findings = append(findings, Finding{
			Severity:    Severity(f.Severity),
			File:        f.File,
			Line:        f.Line,
			Description: f.Description,
		})
	}
	return findings, true
}

// extractFencedJSON pulls JSON content from a markdown code fence.
func extractFencedJSON(s string) string {
	start := strings.Index(s, "```json\n")
	if start == -1 {
		start = strings.Index(s, "```\n")
		if start == -1 {
			return ""
		}
		start += len("```\n")
	} else {
		start += len("```json\n")
	}

	end := strings.Index(s[start:], "\n```")
	if end == -1 {
		return ""
	}

	return strings.TrimSpace(s[start : start+end])
}

// parseTextFindings extracts findings using text pattern matching.
func parseTextFindings(s string) []Finding {
	var findings []Finding
	for _, line := range strings.Split(s, "\n") {
		matches := textFindingPattern.FindStringSubmatch(line)
		if matches == nil {
			continue
		}
		lineNum, _ := strconv.Atoi(matches[3])
		findings = append(findings, Finding{
			Severity:    Severity(matches[1]),
			File:        matches[2],
			Line:        lineNum,
			Description: strings.TrimSpace(matches[4]),
		})
	}
	return findings
}

// buildResult creates a Result from a slice of findings.
func buildResult(findings []Finding) Result {
	return Result{
		Findings: findings,
		IsClean:  len(findings) == 0,
	}
}
