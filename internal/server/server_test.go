package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/slb350/froggr/internal/review"
	"github.com/slb350/froggr/internal/testutil"
	"github.com/stretchr/testify/assert"
)

const testWebhookSecret = "test-webhook-secret" //nolint:gosec // test fixture

// --- Helpers ---

func pushPayload() []byte {
	return []byte(`{
		"ref": "refs/heads/42-add-auth",
		"after": "abc1234",
		"repository": {
			"name": "hello-world",
			"owner": {"login": "octocat"},
			"default_branch": "main"
		},
		"installation": {"id": 12345}
	}`)
}

func webhookRequest(eventType string, payload []byte, secret string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(string(payload)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Github-Event", eventType)
	req.Header.Set("X-Hub-Signature-256", testutil.SignWebhookPayload(payload, secret))
	return req
}

func newTestServer(gh *mockGHClient, eng *mockReviewer) *Server {
	factory := func(_ int64) review.GitHubClient { return gh }
	handler := NewHandler(factory, eng, testDebounceWindow, testLogger())
	return NewServer(handler, []byte(testWebhookSecret), testLogger())
}

// --- Tests ---

func TestServer_ValidPushWebhook(t *testing.T) {
	gh := &mockGHClient{fileErr: testutil.NotFoundError()}
	eng := &mockReviewer{}
	srv := newTestServer(gh, eng)
	defer srv.Stop()

	req := webhookRequest("push", pushPayload(), testWebhookSecret)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	call := waitForReview(t, eng, 500*time.Millisecond)
	assert.Equal(t, "42-add-auth", call.push.Branch)
	assert.Equal(t, 42, call.issueNum)
}

func TestServer_InvalidSignature(t *testing.T) {
	gh := &mockGHClient{fileErr: testutil.NotFoundError()}
	eng := &mockReviewer{}
	srv := newTestServer(gh, eng)
	defer srv.Stop()

	req := webhookRequest("push", pushPayload(), "wrong-secret")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	noReview(t, eng, testDebounceWindow*2)
}

func TestServer_MalformedPayload(t *testing.T) {
	gh := &mockGHClient{fileErr: testutil.NotFoundError()}
	eng := &mockReviewer{}
	srv := newTestServer(gh, eng)
	defer srv.Stop()

	malformed := []byte(`{not valid json}`)
	req := webhookRequest("push", malformed, testWebhookSecret)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestServer_UnknownEventType(t *testing.T) {
	gh := &mockGHClient{fileErr: testutil.NotFoundError()}
	eng := &mockReviewer{}
	srv := newTestServer(gh, eng)
	defer srv.Stop()

	payload := []byte(`{"zen": "Testing.", "hook_id": 1}`)
	req := webhookRequest("ping", payload, testWebhookSecret)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	noReview(t, eng, testDebounceWindow*2)
}

func TestServer_HealthEndpoint(t *testing.T) {
	gh := &mockGHClient{fileErr: testutil.NotFoundError()}
	eng := &mockReviewer{}
	srv := newTestServer(gh, eng)
	defer srv.Stop()

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "ok", rec.Body.String())
}

// Compile-time check: mockGHClient also used in handler_test.go; no need
// to duplicate the interface check since handler_test.go already has it.
// Similarly for mockReviewer.
// We only add a check that *Server satisfies http.Handler.
var _ http.Handler = (*Server)(nil)
