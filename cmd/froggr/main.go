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

	providers := buildProviders(logger)
	if len(providers) == 0 {
		logger.Error("no AI providers configured: set OPENROUTER_API_KEY and/or AWS_REGION")
		os.Exit(1)
	}
	engine := review.NewEngine(providers)
	handler := server.NewHandler(clientFactory, engine, debounceWindow, logger)
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

// buildProviders creates AI clients based on available environment variables.
// At least one provider must be configured.
func buildProviders(logger *slog.Logger) map[string]review.AIClient {
	providers := make(map[string]review.AIClient)

	if key := os.Getenv("OPENROUTER_API_KEY"); key != "" {
		providers[config.ProviderOpenRouter] = openrouter.NewClient(key)
		logger.Info("registered AI provider", "provider", config.ProviderOpenRouter)
	}

	if region := awsRegion(); region != "" {
		client, err := bedrock.NewClient(context.Background(), region)
		if err != nil {
			logger.Error("failed to initialize Bedrock client", "error", err)
			os.Exit(1)
		}
		providers[config.ProviderBedrock] = client
		logger.Info("registered AI provider", "provider", config.ProviderBedrock, "region", region)
	}

	return providers
}

// awsRegion returns the configured AWS region from environment variables.
func awsRegion() string {
	if r := os.Getenv("AWS_REGION"); r != "" {
		return r
	}
	return os.Getenv("AWS_DEFAULT_REGION")
}
