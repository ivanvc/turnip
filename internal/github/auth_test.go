package github

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func generateTestPrivateKeyPEM(t *testing.T) []byte {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	der := x509.MarshalPKCS1PrivateKey(key)
	return pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: der})
}

func TestNewAppAuth_ValidKey(t *testing.T) {
	auth, err := NewAppAuth(12345, generateTestPrivateKeyPEM(t))
	require.NoError(t, err)
	assert.NotNil(t, auth)
}

func TestNewAppAuth_MalformedKey(t *testing.T) {
	_, err := NewAppAuth(12345, []byte("not a valid PEM key"))
	assert.Error(t, err)
}

func TestAppAuth_InstallationClient(t *testing.T) {
	auth, err := NewAppAuth(12345, generateTestPrivateKeyPEM(t))
	require.NoError(t, err)

	client := auth.InstallationClient(67890)
	require.NotNil(t, client)
	assert.NotNil(t, client.gh)
	assert.NotNil(t, client.itr)
}
