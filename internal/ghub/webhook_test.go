package ghub

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/go-github/v84/github"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testSecret = "test-webhook-secret" //nolint:gosec // test fixture, not a real credential

func signPayload(payload []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func newWebhookRequest(t *testing.T, eventType string, payload []byte, secret string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, "/webhook", strings.NewReader(string(payload)))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Github-Event", eventType)
	req.Header.Set("X-Hub-Signature-256", signPayload(payload, secret))
	return req
}

func TestValidateAndParse_PushEvent(t *testing.T) {
	payload := pushEventPayload(t)
	req := newWebhookRequest(t, "push", payload, testSecret)

	eventType, event, err := ValidateAndParse(req, []byte(testSecret))
	require.NoError(t, err)
	assert.Equal(t, "push", eventType)

	pushEvent, ok := event.(*github.PushEvent)
	require.True(t, ok)
	assert.Equal(t, "refs/heads/42-add-auth", pushEvent.GetRef())
}

func TestValidateAndParse_IssuesEvent(t *testing.T) {
	payload := issuesEventPayload(t)
	req := newWebhookRequest(t, "issues", payload, testSecret)

	eventType, event, err := ValidateAndParse(req, []byte(testSecret))
	require.NoError(t, err)
	assert.Equal(t, "issues", eventType)

	issuesEvent, ok := event.(*github.IssuesEvent)
	require.True(t, ok)
	assert.Equal(t, "closed", issuesEvent.GetAction())
}

func TestValidateAndParse_InvalidSignature(t *testing.T) {
	payload := pushEventPayload(t)
	req := newWebhookRequest(t, "push", payload, "wrong-secret")

	_, _, err := ValidateAndParse(req, []byte(testSecret))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "signature")
}

func TestValidateAndParse_MissingSignature(t *testing.T) {
	payload := pushEventPayload(t)
	req, err := http.NewRequest(http.MethodPost, "/webhook", strings.NewReader(string(payload)))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Github-Event", "push")
	// No signature header

	_, _, err = ValidateAndParse(req, []byte(testSecret))
	require.Error(t, err)
}

func TestExtractPushContext_ValidEvent(t *testing.T) {
	var event github.PushEvent
	require.NoError(t, json.Unmarshal(pushEventPayload(t), &event))

	ctx, err := ExtractPushContext(&event)
	require.NoError(t, err)

	assert.Equal(t, int64(12345), ctx.InstallationID)
	assert.Equal(t, "octocat", ctx.Owner)
	assert.Equal(t, "hello-world", ctx.Repo)
	assert.Equal(t, "42-add-auth", ctx.Branch)
	assert.Equal(t, "abc1234def5678", ctx.HeadSHA)
	assert.Equal(t, "main", ctx.DefaultBranch)
}

func TestExtractPushContext_TagPush(t *testing.T) {
	payload := []byte(`{
		"ref": "refs/tags/v1.0.0",
		"after": "abc123",
		"repository": {
			"name": "hello-world",
			"owner": {"login": "octocat"},
			"default_branch": "main"
		},
		"installation": {"id": 1}
	}`)
	var event github.PushEvent
	require.NoError(t, json.Unmarshal(payload, &event))

	_, err := ExtractPushContext(&event)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tag")
}

func TestExtractPushContext_DefaultBranch(t *testing.T) {
	payload := []byte(`{
		"ref": "refs/heads/main",
		"after": "abc123",
		"repository": {
			"name": "hello-world",
			"owner": {"login": "octocat"},
			"default_branch": "main"
		},
		"installation": {"id": 1}
	}`)
	var event github.PushEvent
	require.NoError(t, json.Unmarshal(payload, &event))

	ctx, err := ExtractPushContext(&event)
	require.NoError(t, err)
	assert.Equal(t, "main", ctx.Branch)
	assert.Equal(t, "main", ctx.DefaultBranch)
}

// --- test fixtures ---

func pushEventPayload(t *testing.T) []byte {
	t.Helper()
	return []byte(`{
		"ref": "refs/heads/42-add-auth",
		"after": "abc1234def5678",
		"repository": {
			"name": "hello-world",
			"owner": {"login": "octocat"},
			"default_branch": "main"
		},
		"installation": {"id": 12345}
	}`)
}

func issuesEventPayload(t *testing.T) []byte {
	t.Helper()
	return []byte(`{
		"action": "closed",
		"issue": {
			"number": 42,
			"title": "Add authentication",
			"state": "closed"
		},
		"repository": {
			"name": "hello-world",
			"owner": {"login": "octocat"}
		},
		"installation": {"id": 12345}
	}`)
}
