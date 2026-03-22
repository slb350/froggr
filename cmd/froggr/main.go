// Package main is the entry point for the froggr GitHub App server.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/slb350/froggr/internal/bedrock"
	"github.com/slb350/froggr/internal/config"
	"github.com/slb350/froggr/internal/ghub"
	"github.com/slb350/froggr/internal/openrouter"
	"github.com/slb350/froggr/internal/review"
	"github.com/slb350/froggr/internal/server"
)

const (
	defaultPort = "8080"
	// debounceWindow controls how long froggr waits after the last push before
	// triggering a review. 30 seconds collapses rapid pushes into one review.
	debounceWindow = 30 * time.Second
	// shutdownTimeout is the maximum time to wait for in-flight HTTP requests
	// to complete during graceful shutdown.
	shutdownTimeout = 10 * time.Second
	// providerInitTimeout bounds AWS credential chain discovery so startup
	// doesn't hang when IMDS is unreachable (e.g. running locally).
	providerInitTimeout = 15 * time.Second
)

// newOpenRouterClient and newBedrockClient are constructor functions that can
// be replaced in tests to inject mock AI clients without network calls.
var (
	newOpenRouterClient = func(apiKey string) (review.AIClient, error) {
		return openrouter.NewClient(apiKey)
	}
	newBedrockClient = func(ctx context.Context, region string) (review.AIClient, error) {
		return bedrock.NewClient(ctx, region)
	}
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	appIDStr := requireEnv("GITHUB_APP_ID")
	appID, err := strconv.ParseInt(appIDStr, 10, 64)
	if err != nil {
		logger.Error("invalid GITHUB_APP_ID", "error", err)
		os.Exit(1)
	}

	appAuth, err := ghub.NewAppAuth(appID, []byte(requireEnv("GITHUB_PRIVATE_KEY")))
	if err != nil {
		logger.Error("failed to initialize GitHub App auth", "error", err)
		os.Exit(1)
	}

	webhookSecret := requireEnv("GITHUB_WEBHOOK_SECRET")
	port := envOr("PORT", defaultPort)

	clientFactory := func(installationID int64) review.GitHubClient {
		return ghub.NewClient(appAuth.ClientForInstallation(installationID))
	}

	providers, err := buildProviders(logger)
	if err != nil {
		logger.Error("failed to initialize AI providers", "error", err)
		os.Exit(1)
	}
	if len(providers) == 0 {
		logger.Error("no AI providers configured: set OPENROUTER_API_KEY and/or AWS_REGION")
		os.Exit(1)
	}
	engine := review.NewEngine(providers)
	defaultConfig := config.DefaultsForProviders(configuredProviders(providers)...)
	handler := server.NewHandler(clientFactory, engine, defaultConfig, debounceWindow, logger)
	srv := server.NewServer(handler, []byte(webhookSecret), logger)

	httpSrv := &http.Server{
		Addr:              ":" + port,
		Handler:           srv,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Graceful shutdown on SIGINT/SIGTERM.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)

	go func() {
		<-ctx.Done()
		logger.Info("shutting down")

		// Drain in-flight HTTP requests before canceling debounce timers,
		// so handlers don't Push into a stopped buffer.
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()

		shutdownErr := httpSrv.Shutdown(shutdownCtx)
		if shutdownErr != nil {
			logger.Error("shutdown error", "error", shutdownErr)
		}

		handler.Stop()
	}()

	logger.Info("starting froggr", "port", port)
	listenErr := httpSrv.ListenAndServe()
	stop() // Release signal handler resources.
	if listenErr != nil && !errors.Is(listenErr, http.ErrServerClosed) {
		logger.Error("server failed", "error", listenErr)
		os.Exit(1)
	}
}

func requireEnv(key string) string {
	val := os.Getenv(key)
	if val == "" {
		fmt.Fprintf(os.Stderr, "required environment variable %s is not set\n", key)
		os.Exit(1)
	}
	return val
}

func envOr(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}

// buildProviders creates AI clients from available environment variables.
// It tries all configured providers (OpenRouter via API key, Bedrock via AWS
// region), logging warnings for individual failures. Returns an empty map only
// if NO providers are configured. If some init and others fail, the working
// ones are returned — froggr runs with partial provider availability.
func buildProviders(logger *slog.Logger) (map[config.Provider]review.AIClient, error) {
	providers := make(map[config.Provider]review.AIClient)
	var initErrs []error

	if key := os.Getenv("OPENROUTER_API_KEY"); key != "" {
		if err := registerProvider(logger, providers, config.ProviderOpenRouter, func() (review.AIClient, error) {
			return newOpenRouterClient(key)
		}); err != nil {
			initErrs = append(initErrs, err)
		}
	}

	if region := awsRegion(); region != "" {
		if err := registerProvider(logger, providers, config.ProviderBedrock, func() (review.AIClient, error) {
			initCtx, initCancel := context.WithTimeout(context.Background(), providerInitTimeout)
			client, err := newBedrockClient(initCtx, region)
			initCancel()
			return client, err
		}, "region", region); err != nil {
			initErrs = append(initErrs, err)
		}
	}

	if len(providers) > 0 {
		return providers, nil
	}
	if len(initErrs) > 0 {
		return nil, errors.Join(initErrs...)
	}
	return providers, nil
}

// awsRegion returns the configured AWS region from environment variables.
func awsRegion() string {
	if r := os.Getenv("AWS_REGION"); r != "" {
		return r
	}
	return os.Getenv("AWS_DEFAULT_REGION")
}

func configuredProviders(providers map[config.Provider]review.AIClient) []config.Provider {
	keys := make([]config.Provider, 0, len(providers))
	for provider := range providers {
		keys = append(keys, provider)
	}
	return keys
}

func registerProvider(
	logger *slog.Logger,
	providers map[config.Provider]review.AIClient,
	provider config.Provider,
	init func() (review.AIClient, error),
	extraLogAttrs ...any,
) error {
	client, err := init()
	if err != nil {
		initErr := fmt.Errorf("initializing %s client: %w", providerDisplayName(provider), err)
		logAttrs := append([]any{"provider", provider, "error", err}, extraLogAttrs...)
		logger.Warn("failed to initialize AI provider", logAttrs...)
		return initErr
	}

	providers[provider] = client
	logAttrs := append([]any{"provider", provider}, extraLogAttrs...)
	logger.Info("registered AI provider", logAttrs...)
	return nil
}

func providerDisplayName(provider config.Provider) string {
	switch provider {
	case config.ProviderBedrock:
		return "Bedrock"
	default:
		return "OpenRouter"
	}
}
