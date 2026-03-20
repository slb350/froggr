package testutil

import (
	"net/http"

	"github.com/google/go-github/v84/github"
)

// NotFoundError returns a github.ErrorResponse that represents a 404,
// suitable for use in tests that need to simulate a missing resource.
func NotFoundError() error {
	return &github.ErrorResponse{Response: &http.Response{StatusCode: http.StatusNotFound}}
}
