package ghub

import (
	"fmt"
	"net/http"
	"sync"
	"time"

	ghinstallation "github.com/bradleyfalzon/ghinstallation/v2"
	"github.com/google/go-github/v84/github"
)

const defaultGitHubTimeout = 30 * time.Second

// AppAuth handles GitHub App authentication using a private key.
// It creates per-installation GitHub API clients and caches them so that
// repeat pushes from the same installation reuse TCP connections and
// TLS sessions instead of re-handshaking on every webhook.
type AppAuth struct {
	appsTransport *ghinstallation.AppsTransport

	mu      sync.Mutex
	clients map[int64]*github.Client
}

// NewAppAuth creates an AppAuth from a GitHub App ID and PEM-encoded private key.
// The key is validated immediately; an error is returned if parsing fails.
func NewAppAuth(appID int64, privateKeyPEM []byte) (*AppAuth, error) {
	atr, err := ghinstallation.NewAppsTransport(http.DefaultTransport, appID, privateKeyPEM)
	if err != nil {
		return nil, fmt.Errorf("parsing GitHub App private key: %w", err)
	}

	return &AppAuth{
		appsTransport: atr,
		clients:       make(map[int64]*github.Client),
	}, nil
}

// ClientForInstallation returns a GitHub API client authenticated as the given
// installation. Clients are cached per installation ID so that repeat calls
// reuse the same HTTP transport (and its connection pool). The underlying
// ghinstallation transport handles token refresh automatically.
func (a *AppAuth) ClientForInstallation(installationID int64) *github.Client {
	a.mu.Lock()
	defer a.mu.Unlock()

	if c, ok := a.clients[installationID]; ok {
		return c
	}

	itr := ghinstallation.NewFromAppsTransport(a.appsTransport, installationID)
	c := github.NewClient(&http.Client{
		Transport: itr,
		Timeout:   defaultGitHubTimeout,
	})
	a.clients[installationID] = c
	return c
}
