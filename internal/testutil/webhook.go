// Package testutil provides shared test helpers used across froggr's packages.
package testutil

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
)

// SignWebhookPayload computes the HMAC-SHA256 signature for a GitHub webhook
// payload in the format GitHub uses (sha256=<hex>).
func SignWebhookPayload(payload []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}
