package ghub

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func generateTestPEM(t *testing.T) []byte {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	return pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
}

func TestNewAppAuth_ValidKey(t *testing.T) {
	pemKey := generateTestPEM(t)
	auth, err := NewAppAuth(12345, pemKey)
	require.NoError(t, err)
	assert.NotNil(t, auth)
}

func TestNewAppAuth_InvalidKey(t *testing.T) {
	_, err := NewAppAuth(12345, []byte("not a PEM key"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "private key")
}

func TestClientForInstallation_ReturnsClient(t *testing.T) {
	pemKey := generateTestPEM(t)
	auth, err := NewAppAuth(12345, pemKey)
	require.NoError(t, err)

	client := auth.ClientForInstallation(67890)
	assert.NotNil(t, client)
	assert.Equal(t, defaultGitHubTimeout, client.Client().Timeout)
}
