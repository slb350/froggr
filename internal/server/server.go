package server

import (
	"log/slog"
	"net/http"

	"github.com/google/go-github/v84/github"
	"github.com/slb350/froggr/internal/ghub"
)

// Server is the HTTP server for froggr's webhook endpoint and health check.
type Server struct {
	handler       *Handler
	webhookSecret []byte
	logger        *slog.Logger
	mux           *http.ServeMux
}

// NewServer creates a Server that routes webhooks to the given Handler.
func NewServer(handler *Handler, webhookSecret []byte, logger *slog.Logger) *Server {
	s := &Server{
		handler:       handler,
		webhookSecret: webhookSecret,
		logger:        logger,
		mux:           http.NewServeMux(),
	}
	s.mux.HandleFunc("POST /webhook", s.handleWebhook)
	s.mux.HandleFunc("GET /health", s.handleHealth)
	return s
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

// Stop cancels all pending reviews.
func (s *Server) Stop() {
	s.handler.Stop()
}

// handleWebhook validates the webhook signature, parses the event, and routes
// it to the appropriate handler method.
func (s *Server) handleWebhook(w http.ResponseWriter, r *http.Request) {
	payload, err := github.ValidatePayload(r, s.webhookSecret)
	if err != nil {
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		s.logger.Warn("webhook signature validation failed", "error", err)
		return
	}

	eventType := github.WebHookType(r)
	event, err := github.ParseWebHook(eventType, payload)
	if err != nil {
		http.Error(w, "malformed payload", http.StatusBadRequest)
		s.logger.Warn("webhook payload parse failed", "error", err, "type", eventType)
		return
	}

	s.routeEvent(r, eventType, event)
	w.WriteHeader(http.StatusOK)
}

// handleHealth returns a simple 200 OK for liveness/readiness probes.
func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	_, _ = w.Write([]byte("ok"))
}

// routeEvent dispatches a parsed webhook event to the appropriate handler method.
func (s *Server) routeEvent(r *http.Request, eventType string, event any) {
	switch e := event.(type) {
	case *github.PushEvent:
		push, err := ghub.ExtractPushContext(e)
		if err != nil {
			s.logger.Info("ignoring push event", "error", err)
			return
		}
		s.handler.HandlePush(r.Context(), push)

	case *github.IssuesEvent:
		if e.GetAction() == "closed" {
			repo := e.GetRepo()
			if repo == nil || repo.GetOwner() == nil || e.GetIssue() == nil {
				s.logger.Warn("issues event missing required fields")
				return
			}
			s.handler.HandleIssuesClosed(
				repo.GetOwner().GetLogin(),
				repo.GetName(),
				e.GetIssue().GetNumber(),
			)
		}

	default:
		s.logger.Info("ignoring unhandled event type", "type", eventType)
	}
}
