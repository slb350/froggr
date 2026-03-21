// Package config handles parsing and validation of .froggr.yml configuration.
package config

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	// defaultBranchPattern extracts issue numbers from branch names like
	// "42-add-auth" → issue #42. The first capture group must be the number.
	defaultBranchPattern = `^(\d+)-`
	defaultModel         = "anthropic/claude-sonnet-4.6"
)

// defaultBranchPatternRE is compiled once to avoid re-compiling on every
// Defaults() call (which runs on each push when .froggr.yml is missing).
var defaultBranchPatternRE = regexp.MustCompile(defaultBranchPattern)

// Provider identifies an AI provider.
type Provider string

// Valid provider names.
const (
	ProviderOpenRouter Provider = "openrouter"
	ProviderBedrock    Provider = "bedrock"
)

// Valid reports whether p is a known provider.
func (p Provider) Valid() bool {
	switch p {
	case ProviderOpenRouter, ProviderBedrock:
		return true
	}
	return false
}

// Config holds the parsed .froggr.yml configuration for a repository.
type Config struct {
	BranchPattern *regexp.Regexp
	AutoDraftPR   bool
	IgnorePaths   []string
	Model         string
	Provider      Provider
}

// rawConfig is the YAML-deserialized form before validation.
type rawConfig struct {
	BranchPattern string   `yaml:"branch_pattern"`
	AutoDraftPR   *bool    `yaml:"auto_draft_pr"`
	IgnorePaths   []string `yaml:"ignore_paths"`
	Model         string   `yaml:"model"`
	Provider      string   `yaml:"provider"`
}

// Defaults returns a Config with sensible default values.
func Defaults() Config {
	return Config{
		BranchPattern: defaultBranchPatternRE,
		AutoDraftPR:   true,
		IgnorePaths:   []string{"*.lock", "vendor/**", "generated/**"},
		Model:         defaultModel,
		Provider:      ProviderOpenRouter,
	}
}

// Parse reads YAML content and returns a validated Config. Missing fields are
// filled from Defaults(). Empty or nil content returns Defaults().
func Parse(content []byte) (Config, error) {
	cfg := Defaults()

	if len(content) == 0 {
		return cfg, nil
	}

	var raw rawConfig
	if err := yaml.Unmarshal(content, &raw); err != nil {
		return Config{}, fmt.Errorf("parsing .froggr.yml: %w", err)
	}

	if raw.BranchPattern != "" {
		re, err := compileBranchPattern(raw.BranchPattern)
		if err != nil {
			return Config{}, err
		}
		cfg.BranchPattern = re
	}

	if raw.AutoDraftPR != nil {
		cfg.AutoDraftPR = *raw.AutoDraftPR
	}

	if len(raw.IgnorePaths) > 0 {
		if err := validateIgnorePatterns(raw.IgnorePaths); err != nil {
			return Config{}, err
		}
		cfg.IgnorePaths = raw.IgnorePaths
	}

	if raw.Model != "" {
		cfg.Model = raw.Model
	}

	provider, err := resolveProvider(raw.Provider, raw.Model, cfg.Provider)
	if err != nil {
		return Config{}, err
	}
	cfg.Provider = provider

	return cfg, nil
}

// compileBranchPattern validates and compiles a branch pattern regex.
func compileBranchPattern(pattern string) (*regexp.Regexp, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid branch_pattern %q: %w", pattern, err)
	}
	if re.NumSubexp() < 1 {
		return nil, fmt.Errorf("branch_pattern %q must have at least one capture group for the issue number", pattern)
	}
	return re, nil
}

// resolveProvider determines the provider from explicit config, model ID
// auto-detection, or the default. Explicit provider takes precedence.
func resolveProvider(rawProvider, model string, defaultProvider Provider) (Provider, error) {
	if rawProvider != "" {
		p := Provider(rawProvider)
		if !p.Valid() {
			return "", fmt.Errorf("invalid provider %q: must be %q or %q", rawProvider, ProviderOpenRouter, ProviderBedrock)
		}
		return p, nil
	}
	if model != "" {
		return detectProvider(model)
	}
	return defaultProvider, nil
}

// detectProvider infers the AI provider from the model ID format.
// OpenRouter model IDs contain a slash (e.g. "anthropic/claude-sonnet-4.6").
// Bedrock model IDs contain a dot but no slash (e.g. "anthropic.claude-sonnet-4-6").
// Model IDs containing both (e.g. "provider/model-v1.0") are classified as
// OpenRouter because the slash check takes precedence.
// Ambiguous model IDs (no slash and no dot) return an error — set provider
// explicitly in .froggr.yml.
func detectProvider(model string) (Provider, error) {
	if strings.Contains(model, "/") {
		return ProviderOpenRouter, nil
	}
	if strings.Contains(model, ".") {
		return ProviderBedrock, nil
	}
	return "", fmt.Errorf("cannot auto-detect provider for model %q: set provider explicitly in .froggr.yml", model)
}

// MatchBranch extracts an issue number from a branch name using the configured
// pattern. The first capture group must contain the issue number. Returns the
// issue number and true if matched, or 0 and false if not.
func (c Config) MatchBranch(name string) (int, bool) {
	matches := c.BranchPattern.FindStringSubmatch(name)
	if len(matches) < 2 {
		return 0, false
	}

	num, err := strconv.Atoi(matches[1])
	if err != nil || num <= 0 {
		return 0, false
	}

	return num, true
}

// ShouldIgnore returns true if the given file path matches any of the
// configured ignore patterns. Supports ** for recursive directory matching
// and * for single-segment globs.
func (c Config) ShouldIgnore(path string) bool {
	for _, pattern := range c.IgnorePaths {
		if matchIgnorePattern(pattern, path) {
			return true
		}
	}
	return false
}

// matchIgnorePattern checks if a path matches a single ignore pattern.
// It handles ** for recursive matching and falls back to checking just
// the filename for simple glob patterns (e.g., "*.lock").
func matchIgnorePattern(pattern, path string) bool {
	// Handle "dir/**" — match anything under that prefix.
	if strings.HasSuffix(pattern, "/**") {
		prefix := strings.TrimSuffix(pattern, "/**")
		return strings.HasPrefix(path, prefix+"/") || path == prefix
	}

	// Handle patterns with no path separator — match against basename.
	if !strings.Contains(pattern, "/") {
		return globMatch(pattern, path) || globMatch(pattern, basename(path))
	}

	return globMatch(pattern, path)
}

// globMatch does simple glob matching. We use a custom implementation instead
// of filepath.Match because filepath.Match doesn't support ** for recursive
// directory matching and uses OS-specific path separators.
// * matches any non-separator sequence,
// ? matches a single non-separator character. Does not support **.
func globMatch(pattern, name string) bool {
	for len(pattern) > 0 {
		switch pattern[0] {
		case '*':
			return globMatchStar(pattern[1:], name)
		case '?':
			if len(name) == 0 || name[0] == '/' {
				return false
			}
			pattern = pattern[1:]
			name = name[1:]
		default:
			if len(name) == 0 || pattern[0] != name[0] {
				return false
			}
			pattern = pattern[1:]
			name = name[1:]
		}
	}
	return len(name) == 0
}

// globMatchStar handles the * wildcard: tries matching the rest of the pattern
// against progressively longer prefixes consumed by the star, stopping at path
// separators.
func globMatchStar(restPattern, name string) bool {
	if len(restPattern) == 0 {
		return !strings.Contains(name, "/")
	}
	for i := 0; i <= len(name); i++ {
		if globMatch(restPattern, name[i:]) {
			return true
		}
		if i < len(name) && name[i] == '/' {
			break
		}
	}
	return false
}

// validateIgnorePatterns checks that all ignore patterns are syntactically
// valid glob patterns.
func validateIgnorePatterns(patterns []string) error {
	for _, p := range patterns {
		// Strip ** suffix — we handle that separately.
		check := strings.TrimSuffix(p, "/**")
		if _, err := filepath.Match(check, "probe"); err != nil {
			return fmt.Errorf("invalid ignore_paths pattern %q: %w", p, err)
		}
	}
	return nil
}

// basename returns the last element of the path.
func basename(path string) string {
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[i+1:]
	}
	return path
}
