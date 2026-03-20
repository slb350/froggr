package review

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// ErrInvalidAIResponse means the model did not return a review payload froggr
// can safely trust as either findings or an explicit clean result.
var ErrInvalidAIResponse = errors.New("invalid AI response format")

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
	if findings, err := tryParseJSON(response); err == nil {
		return buildResult(findings), nil
	} else if err != errNotJSON {
		return Result{}, err
	}

	// Try extracting JSON from markdown code fence.
	if extracted := extractFencedJSON(response); extracted != "" {
		if findings, err := tryParseJSON(extracted); err == nil {
			return buildResult(findings), nil
		} else if err != errNotJSON {
			return Result{}, err
		}
	}

	// Fall back to text pattern matching.
	findings := parseTextFindings(response)
	if len(findings) > 0 {
		return buildResult(findings), nil
	}

	return Result{}, fmt.Errorf("%w: expected JSON findings array or recognized fallback format", ErrInvalidAIResponse)
}

var errNotJSON = errors.New("response is not a JSON array")

// tryParseJSON attempts to parse the response as a JSON array of findings.
func tryParseJSON(s string) ([]Finding, error) {
	var raw []findingJSON
	if err := json.Unmarshal([]byte(s), &raw); err != nil {
		return nil, errNotJSON
	}

	findings := make([]Finding, 0, len(raw))
	for i, f := range raw {
		if err := validateFindingJSON(f); err != nil {
			return nil, fmt.Errorf("%w: finding %d: %v", ErrInvalidAIResponse, i+1, err)
		}
		findings = append(findings, Finding{
			Severity:    normalizeSeverity(f.Severity),
			File:        f.File,
			Line:        f.Line,
			Description: f.Description,
		})
	}
	return findings, nil
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

func validateFindingJSON(f findingJSON) error {
	switch normalizeSeverity(f.Severity) {
	case SeverityBug, SeverityConcern:
	default:
		return fmt.Errorf("unsupported severity %q", f.Severity)
	}
	if strings.TrimSpace(f.File) == "" {
		return fmt.Errorf("file is required")
	}
	if f.Line <= 0 {
		return fmt.Errorf("line must be positive")
	}
	if strings.TrimSpace(f.Description) == "" {
		return fmt.Errorf("description is required")
	}
	return nil
}

func normalizeSeverity(s string) Severity {
	return Severity(strings.TrimSpace(s))
}

// buildResult creates a Result from a slice of findings.
func buildResult(findings []Finding) Result {
	return Result{
		Findings: findings,
		IsClean:  len(findings) == 0,
	}
}
