package review

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParse_JSONResponse(t *testing.T) {
	response := `[
		{"severity": "Bug", "file": "src/auth.go", "line": 42, "description": "SQL injection via string concat"},
		{"severity": "Concern", "file": "src/handler.go", "line": 15, "description": "Missing input validation"}
	]`

	result, err := ParseResponse(response)
	require.NoError(t, err)
	require.Len(t, result.Findings, 2)

	assert.Equal(t, SeverityBug, result.Findings[0].Severity)
	assert.Equal(t, "src/auth.go", result.Findings[0].File)
	assert.Equal(t, 42, result.Findings[0].Line)
	assert.Contains(t, result.Findings[0].Description, "SQL injection")

	assert.Equal(t, SeverityConcern, result.Findings[1].Severity)
	assert.False(t, result.IsClean)
}

func TestParse_MarkdownFencedJSON(t *testing.T) {
	response := "Here are my findings:\n```json\n" +
		`[{"severity": "Bug", "file": "main.go", "line": 10, "description": "nil pointer"}]` +
		"\n```\n"

	result, err := ParseResponse(response)
	require.NoError(t, err)
	require.Len(t, result.Findings, 1)
	assert.Equal(t, SeverityBug, result.Findings[0].Severity)
}

func TestParse_NoFindings(t *testing.T) {
	result, err := ParseResponse("[]")
	require.NoError(t, err)
	assert.Empty(t, result.Findings)
	assert.True(t, result.IsClean)
}

func TestParse_MalformedJSON_Fallback(t *testing.T) {
	response := `Here's what I found:
**Bug** in src/auth.go line 42: SQL injection vulnerability
**Concern** in src/handler.go line 15: missing validation`

	result, err := ParseResponse(response)
	require.NoError(t, err)
	require.Len(t, result.Findings, 2)

	assert.Equal(t, SeverityBug, result.Findings[0].Severity)
	assert.Equal(t, "src/auth.go", result.Findings[0].File)
	assert.Equal(t, 42, result.Findings[0].Line)

	assert.Equal(t, SeverityConcern, result.Findings[1].Severity)
}

func TestParse_BugPattern(t *testing.T) {
	response := `**Bug** in src/main.go line 5: use after free`

	result, err := ParseResponse(response)
	require.NoError(t, err)
	require.Len(t, result.Findings, 1)
	assert.Equal(t, SeverityBug, result.Findings[0].Severity)
	assert.Equal(t, "src/main.go", result.Findings[0].File)
	assert.Equal(t, 5, result.Findings[0].Line)
	assert.Contains(t, result.Findings[0].Description, "use after free")
}

func TestParse_ConcernPattern(t *testing.T) {
	response := `**Concern** in pkg/util.go line 99: potential race condition`

	result, err := ParseResponse(response)
	require.NoError(t, err)
	require.Len(t, result.Findings, 1)
	assert.Equal(t, SeverityConcern, result.Findings[0].Severity)
}

func TestParse_EmptyResponse(t *testing.T) {
	_, err := ParseResponse("")
	require.Error(t, err)
}

func TestParse_InvalidJSONFinding_Severity(t *testing.T) {
	response := `[{"severity":"Info","file":"src/auth.go","line":1,"description":"not allowed"}]`

	_, err := ParseResponse(response)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidAIResponse)
	assert.Contains(t, err.Error(), "unsupported severity")
}

func TestParse_InvalidJSONFinding_MissingFields(t *testing.T) {
	response := `[{"severity":"Bug","file":"","line":0,"description":""}]`

	_, err := ParseResponse(response)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidAIResponse)
}

func TestParse_UnrecognizedTextFailsClosed(t *testing.T) {
	response := `I think this looks good overall.`

	_, err := ParseResponse(response)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidAIResponse)
}

func TestParse_CaseInsensitiveSeverity(t *testing.T) {
	response := `[{"severity":"bug","file":"src/auth.go","line":1,"description":"case test"}]`

	result, err := ParseResponse(response)
	require.NoError(t, err)
	require.Len(t, result.Findings, 1)
	assert.Equal(t, SeverityBug, result.Findings[0].Severity)
}

func TestParse_SeverityWithWhitespace(t *testing.T) {
	response := `[{"severity":" Concern ","file":"src/auth.go","line":1,"description":"ws test"}]`

	result, err := ParseResponse(response)
	require.NoError(t, err)
	require.Len(t, result.Findings, 1)
	assert.Equal(t, SeverityConcern, result.Findings[0].Severity)
}

func TestParse_InvalidFencedJSONFailsClosed(t *testing.T) {
	response := "```json\n" +
		`[{"severity":"Bug","file":"","line":0,"description":""}]` +
		"\n```"

	_, err := ParseResponse(response)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidAIResponse)
}
