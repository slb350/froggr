package ghub

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/google/go-github/v84/github"
)

// ValidateAndParse validates the webhook signature and parses the event payload.
// Returns the event type string and the parsed event (use a type switch to
// determine the concrete type, e.g. *github.PushEvent).
func ValidateAndParse(r *http.Request, secret []byte) (string, any, error) {
	payload, err := github.ValidatePayload(r, secret)
	if err != nil {
		return "", nil, fmt.Errorf("validating webhook signature: %w", err)
	}

	eventType := github.WebHookType(r)
	event, err := github.ParseWebHook(eventType, payload)
	if err != nil {
		return "", nil, fmt.Errorf("parsing webhook payload: %w", err)
	}

	return eventType, event, nil
}

// ExtractPushContext extracts the fields needed for review from a PushEvent.
// Returns an error for tag pushes (refs/tags/).
func ExtractPushContext(event *github.PushEvent) (PushContext, error) {
	ref := event.GetRef()

	if strings.HasPrefix(ref, "refs/tags/") {
		return PushContext{}, fmt.Errorf("ignoring tag push: %s", ref)
	}

	branch := strings.TrimPrefix(ref, "refs/heads/")

	repo := event.GetRepo()
	if repo == nil {
		return PushContext{}, fmt.Errorf("push event missing repository")
	}

	owner := ""
	if repo.Owner != nil {
		owner = repo.GetOwner().GetLogin()
	}
	if owner == "" {
		return PushContext{}, fmt.Errorf("push event missing repository owner")
	}

	return PushContext{
		InstallationID: event.GetInstallation().GetID(),
		Owner:          owner,
		Repo:           repo.GetName(),
		Branch:         branch,
		HeadSHA:        event.GetAfter(),
		DefaultBranch:  repo.GetDefaultBranch(),
	}, nil
}
