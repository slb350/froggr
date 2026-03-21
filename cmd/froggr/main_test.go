package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/slb350/froggr/internal/ai"
	"github.com/slb350/froggr/internal/config"
	"github.com/slb350/froggr/internal/review"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAwsRegion(t *testing.T) {
	tests := []struct {
		name          string
		awsRegion     string
		defaultRegion string
		want          string
	}{
		{"prefers AWS_REGION", "us-west-2", "eu-west-1", "us-west-2"},
		{"falls back to AWS_DEFAULT_REGION", "", "eu-west-1", "eu-west-1"},
		{"empty when neither set", "", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("AWS_REGION", tt.awsRegion)
			t.Setenv("AWS_DEFAULT_REGION", tt.defaultRegion)
			assert.Equal(t, tt.want, awsRegion())
		})
	}
}

type stubAIClient struct{}

func (stubAIClient) Complete(context.Context, ai.CompletionRequest) (string, error) {
	return "[]", nil
}

func TestBuildProviders_BedrockFailureDoesNotBlockOpenRouter(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "sk-test")
	t.Setenv("AWS_REGION", "us-west-2")

	swapProviderFactories(t,
		func(string) (review.AIClient, error) { return stubAIClient{}, nil },
		func(context.Context, string) (review.AIClient, error) { return nil, errors.New("bad aws config") },
	)

	providers, err := buildProviders(testLogger())
	require.NoError(t, err)
	assert.Contains(t, providers, config.ProviderOpenRouter)
	assert.NotContains(t, providers, config.ProviderBedrock)
}

func TestBuildProviders_OpenRouterFailureFallsBackToBedrock(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "sk-test")
	t.Setenv("AWS_REGION", "us-west-2")

	swapProviderFactories(t,
		func(string) (review.AIClient, error) { return nil, errors.New("bad api key") },
		func(context.Context, string) (review.AIClient, error) { return stubAIClient{}, nil },
	)

	providers, err := buildProviders(testLogger())
	require.NoError(t, err)
	assert.NotContains(t, providers, config.ProviderOpenRouter)
	assert.Contains(t, providers, config.ProviderBedrock)
}

func TestBuildProviders_AllConfiguredProvidersFailReturnsJoinedError(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "sk-test")
	t.Setenv("AWS_REGION", "us-west-2")

	swapProviderFactories(t,
		func(string) (review.AIClient, error) { return nil, errors.New("openrouter failed") },
		func(context.Context, string) (review.AIClient, error) { return nil, errors.New("bedrock failed") },
	)

	providers, err := buildProviders(testLogger())
	require.Error(t, err)
	assert.Nil(t, providers)
	assert.Contains(t, err.Error(), "initializing OpenRouter client")
	assert.Contains(t, err.Error(), "initializing Bedrock client")
}

func swapProviderFactories(
	t *testing.T,
	openRouter func(string) (review.AIClient, error),
	bedrockFn func(context.Context, string) (review.AIClient, error),
) {
	t.Helper()

	prevOpenRouter := newOpenRouterClient
	prevBedrock := newBedrockClient
	newOpenRouterClient = openRouter
	newBedrockClient = bedrockFn

	t.Cleanup(func() {
		newOpenRouterClient = prevOpenRouter
		newBedrockClient = prevBedrock
	})
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
