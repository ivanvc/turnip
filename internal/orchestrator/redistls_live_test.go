package orchestrator

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This is the checkpoint test (tasks 3.1): a real client, built from
// RedisOptions, against a Redis that genuinely requires TLS and AUTH. The
// unit tests in redisoptions_test.go show the options are configured; only
// standing up miniredis over TLS with a required password, and connecting to
// it, shows the connection is actually encrypted and authenticated. It is
// refused without the CA (certificate verification fails), with a wrong
// password, and over a plaintext redis:// address to the TLS port.

// liveTLSCerts returns a freshly generated CA (PEM) and a server certificate
// signed by that CA, valid for 127.0.0.1 and localhost. The CA's own key is
// discarded: a test needs only the CA certificate (to trust) and the server
// certificate (to serve). This is distinct from testCAPEM in
// redisoptions_test.go, which generates a self-signed CA that signs nothing.
func liveTLSCerts(t *testing.T) (caPEM []byte, serverCert tls.Certificate) {
	t.Helper()

	caKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	caTmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "turnip-test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	require.NoError(t, err)
	caCert, err := x509.ParseCertificate(caDER)
	require.NoError(t, err)
	caPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})

	srvKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	srvTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "127.0.0.1"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:     []string{"localhost"},
	}
	srvDER, err := x509.CreateCertificate(rand.Reader, srvTmpl, caCert, &srvKey.PublicKey, caKey)
	require.NoError(t, err)
	serverCert = tls.Certificate{
		Certificate: [][]byte{srvDER},
		PrivateKey:  srvKey,
	}
	return caPEM, serverCert
}

// startTLSRedis stands up a miniredis serving TLS with the given server
// certificate, returning the instance and the path to a temp file holding the
// CA that signed it (for TURNIP_REDIS_CA_PATH).
func startTLSRedis(t *testing.T) (*miniredis.Miniredis, string) {
	t.Helper()
	caPEM, serverCert := liveTLSCerts(t)
	mr, err := miniredis.RunTLS(&tls.Config{Certificates: []tls.Certificate{serverCert}})
	require.NoError(t, err)
	t.Cleanup(mr.Close)
	caPath := writeTemp(t, "ca.pem", string(caPEM))
	return mr, caPath
}

// pingClient builds the real go-redis client from RedisOptions and pings it,
// closing it on cleanup and returning the ping error (nil on success).
func pingClient(t *testing.T, cfg Config) (Transport, error) {
	t.Helper()
	opts, transport, err := RedisOptions(cfg)
	require.NoError(t, err)
	client := redis.NewClient(opts)
	t.Cleanup(func() { _ = client.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return transport, client.Ping(ctx).Err()
}

// TestRedisLive_TLSPasswordAuth is the checkpoint for a password-only AUTH
// (miniredis RequireAuth): a successful encrypted, authenticated ping, and the
// three refusals.
func TestRedisLive_TLSPasswordAuth(t *testing.T) {
	const password = "s3cret-redis-token-9f3a"
	mr, caPath := startTLSRedis(t)
	mr.RequireAuth(password)
	tlsAddr := "rediss://" + mr.Host() + ":" + mr.Port()

	t.Run("connects and pings with CA and password", func(t *testing.T) {
		transport, err := pingClient(t, Config{
			RedisAddr:     tlsAddr,
			RedisCAPath:   caPath,
			RedisPassword: password,
		})
		require.NoError(t, err, "the encrypted, authenticated connection must succeed")
		assert.True(t, transport.TLS, "transport reports TLS")
		assert.Equal(t, authPassword, transport.Auth, "transport reports password auth")
		assert.True(t, transport.CustomCA, "transport reports the custom CA")
		// Security: the Transport that is logged and served must never hold
		// the credential.
		assert.NotContains(t, fmt.Sprintf("%+v", transport), password)
	})

	t.Run("refused without the CA (verification fails)", func(t *testing.T) {
		_, err := pingClient(t, Config{
			RedisAddr:     tlsAddr,
			RedisPassword: password,
		})
		require.Error(t, err, "a server cert from an untrusted CA must be rejected")
		assert.NotContains(t, err.Error(), password, "no credential in the error")
	})

	t.Run("refused with a wrong password", func(t *testing.T) {
		_, err := pingClient(t, Config{
			RedisAddr:     tlsAddr,
			RedisCAPath:   caPath,
			RedisPassword: "not-the-password",
		})
		require.Error(t, err, "a wrong password must be rejected")
		assert.NotContains(t, err.Error(), password, "no credential in the error")
	})

	t.Run("refused over plaintext redis:// to the TLS port", func(t *testing.T) {
		plainAddr := "redis://" + mr.Host() + ":" + mr.Port()
		transport, err := pingClient(t, Config{
			RedisAddr:     plainAddr,
			RedisPassword: password,
		})
		assert.False(t, transport.TLS, "redis:// negotiates no TLS")
		require.Error(t, err, "a plaintext client must not reach a TLS-only port")
		assert.NotContains(t, err.Error(), password, "no credential in the error")
	})
}

// TestRedisLive_TLSUsernamePasswordAuth is the checkpoint for username+password
// AUTH (miniredis RequireUserAuth): the username comes from the URL, the
// password from TURNIP_REDIS_PASSWORD.
func TestRedisLive_TLSUsernamePasswordAuth(t *testing.T) {
	const (
		username = "turnip"
		password = "user-s3cret-token-b71c"
	)
	mr, caPath := startTLSRedis(t)
	mr.RequireUserAuth(username, password)
	tlsAddr := "rediss://" + username + "@" + mr.Host() + ":" + mr.Port()

	t.Run("connects and pings with username and password", func(t *testing.T) {
		transport, err := pingClient(t, Config{
			RedisAddr:     tlsAddr,
			RedisCAPath:   caPath,
			RedisPassword: password,
		})
		require.NoError(t, err)
		assert.True(t, transport.TLS)
		assert.Equal(t, authUsernamePassword, transport.Auth)
		assert.NotContains(t, fmt.Sprintf("%+v", transport), password)
	})

	t.Run("refused with a wrong password", func(t *testing.T) {
		_, err := pingClient(t, Config{
			RedisAddr:     tlsAddr,
			RedisCAPath:   caPath,
			RedisPassword: "not-the-password",
		})
		require.Error(t, err)
		assert.NotContains(t, err.Error(), password)
	})
}
