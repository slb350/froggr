package ghub

import (
	"fmt"
	"net/http"

	ghinstallation "github.com/bradleyfalzon/ghinstallation/v2"
	"github.com/google/go-github/v84/github"
)

// AppAuth handles GitHub App authentication using a private key.
// It creates per-installation GitHub API clients.
type AppAuth struct {
	appsTransport *ghinstallation.AppsTransport
}

// NewAppAuth creates an AppAuth from a GitHub App ID and PEM-encoded private key.
// The key is validated immediately; an error is returned if parsing fails.
func NewAppAuth(appID int64, privateKeyPEM []byte) (*AppAuth, error) {
	atr, err := ghinstallation.NewAppsTransport(http.DefaultTransport, appID, privateKeyPEM)
	if err != nil {
		return nil, fmt.Errorf("parsing GitHub App private key: %w", err)
	}

	return &AppAuth{appsTransport: atr}, nil
}

// ClientForInstallation returns a GitHub API client authenticated as the given
// installation. The underlying transport handles token refresh automatically.
func (a *AppAuth) ClientForInstallation(installationID int64) *github.Client {
	itr := ghinstallation.NewFromAppsTransport(a.appsTransport, installationID)
	return github.NewClient(&http.Client{Transport: itr})
}
