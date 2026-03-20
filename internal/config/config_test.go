// Package config handles parsing and validation of .froggr.yml configuration.
package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParse_ValidConfig(t *testing.T) {
	input := []byte(`
branch_pattern: "^(\\d+)-"
auto_draft_pr: true
ignore_paths:
  - "*.lock"
  - "vendor/**"
  - "generated/**"
model: "anthropic/claude-sonnet-4"
`)

	cfg, err := Parse(input)
	require.NoError(t, err)
	assert.True(t, cfg.AutoDraftPR)
	assert.Equal(t, "anthropic/claude-sonnet-4", cfg.Model)
	assert.Equal(t, []string{"*.lock", "vendor/**", "generated/**"}, cfg.IgnorePaths)
	assert.NotNil(t, cfg.BranchPattern)
	assert.Equal(t, `^(\d+)-`, cfg.BranchPattern.String())
}

func TestParse_PartialConfig(t *testing.T) {
	input := []byte(`
model: "google/gemini-2.5-pro"
`)

	cfg, err := Parse(input)
	require.NoError(t, err)

	defaults := Defaults()
	assert.Equal(t, "google/gemini-2.5-pro", cfg.Model)
	assert.Equal(t, defaults.AutoDraftPR, cfg.AutoDraftPR)
	assert.Equal(t, defaults.BranchPattern.String(), cfg.BranchPattern.String())
	assert.Equal(t, defaults.IgnorePaths, cfg.IgnorePaths)
}

func TestParse_EmptyContent(t *testing.T) {
	cfg, err := Parse([]byte(""))
	require.NoError(t, err)

	defaults := Defaults()
	assert.Equal(t, defaults.Model, cfg.Model)
	assert.Equal(t, defaults.AutoDraftPR, cfg.AutoDraftPR)
	assert.Equal(t, defaults.BranchPattern.String(), cfg.BranchPattern.String())
	assert.Equal(t, defaults.IgnorePaths, cfg.IgnorePaths)
}

func TestParse_InvalidYAML(t *testing.T) {
	input := []byte(`
branch_pattern: [invalid yaml
  this is broken
`)
	_, err := Parse(input)
	assert.Error(t, err)
}

func TestParse_InvalidRegex(t *testing.T) {
	input := []byte(`
branch_pattern: "[invalid("
`)
	_, err := Parse(input)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "branch_pattern")
}

func TestParse_NoCaptureGroup(t *testing.T) {
	input := []byte(`
branch_pattern: "^\\d+-"
`)
	_, err := Parse(input)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "capture group")
}

func TestParse_InvalidIgnorePattern(t *testing.T) {
	input := []byte(`
ignore_paths:
  - "[invalid"
`)
	_, err := Parse(input)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "ignore_paths")
}

func TestMatchBranch_DefaultPattern(t *testing.T) {
	tests := []struct {
		name      string
		branch    string
		wantNum   int
		wantMatch bool
	}{
		{"standard", "42-add-auth", 42, true},
		{"just number prefix", "7-fix", 7, true},
		{"large number", "1234-refactor-db", 1234, true},
		{"no number prefix", "feature-no-number", 0, false},
		{"number not at start", "feature-42-thing", 0, false},
		{"bare number", "42", 0, false},
	}

	cfg := Defaults()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			num, ok := cfg.MatchBranch(tt.branch)
			assert.Equal(t, tt.wantMatch, ok)
			if ok {
				assert.Equal(t, tt.wantNum, num)
			}
		})
	}
}

func TestMatchBranch_CustomPattern(t *testing.T) {
	input := []byte(`
branch_pattern: "^issue-(\\d+)/"
`)
	cfg, err := Parse(input)
	require.NoError(t, err)

	tests := []struct {
		name      string
		branch    string
		wantNum   int
		wantMatch bool
	}{
		{"matches custom", "issue-99/add-feature", 99, true},
		{"default format no match", "42-add-auth", 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			num, ok := cfg.MatchBranch(tt.branch)
			assert.Equal(t, tt.wantMatch, ok)
			if ok {
				assert.Equal(t, tt.wantNum, num)
			}
		})
	}
}

func TestMatchBranch_NoMatch(t *testing.T) {
	cfg := Defaults()

	branches := []string{
		"feature-no-number",
		"main",
		"develop",
		"release/v1.0",
	}

	for _, branch := range branches {
		t.Run(branch, func(t *testing.T) {
			_, ok := cfg.MatchBranch(branch)
			assert.False(t, ok)
		})
	}
}

func TestShouldIgnore_Patterns(t *testing.T) {
	cfg, err := Parse([]byte(`
ignore_paths:
  - "*.lock"
  - "vendor/**"
  - "generated/**"
  - ".env*"
`))
	require.NoError(t, err)

	tests := []struct {
		name   string
		path   string
		ignore bool
	}{
		{"lock file", "package-lock.json", false},
		{"go.sum lock", "go.lock", true},
		{"vendor file", "vendor/github.com/foo/bar.go", true},
		{"generated file", "generated/api/types.go", true},
		{"env file", ".env.local", true},
		{"normal go file", "internal/config/config.go", false},
		{"normal test file", "internal/config/config_test.go", false},
		{"readme", "README.md", false},
		{"nested normal file", "cmd/froggr/main.go", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.ignore, cfg.ShouldIgnore(tt.path))
		})
	}
}

func TestDefaults(t *testing.T) {
	cfg := Defaults()

	assert.NotNil(t, cfg.BranchPattern)
	assert.True(t, cfg.AutoDraftPR)
	assert.Equal(t, "anthropic/claude-sonnet-4", cfg.Model)
	assert.NotEmpty(t, cfg.IgnorePaths)
}
