package orchestrator

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testCAPEM returns a self-signed CA certificate in PEM form, enough for
// x509.CertPool.AppendCertsFromPEM to accept.
func testCAPEM(t *testing.T) []byte {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "turnip-test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func writeTemp(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

// TestRedisOptions_AddressForms covers each accepted address form and the
// options/Transport it produces.
func TestRedisOptions_AddressForms(t *testing.T) {
	for _, tc := range []struct {
		name         string
		cfg          Config
		wantAddr     string
		wantTLS      bool
		wantAuth     string
		wantUsername string
		wantNilTLS   bool // opts.TLSConfig must be nil (no TLS negotiated)
	}{
		{
			name:       "bare host:port unchanged",
			cfg:        Config{RedisAddr: "localhost:6379"},
			wantAddr:   "localhost:6379",
			wantTLS:    false,
			wantAuth:   authNone,
			wantNilTLS: true,
		},
		{
			name:       "redis scheme, no TLS",
			cfg:        Config{RedisAddr: "redis://cache.example.com:6379"},
			wantAddr:   "cache.example.com:6379",
			wantTLS:    false,
			wantAuth:   authNone,
			wantNilTLS: true,
		},
		{
			name:     "rediss scheme, TLS",
			cfg:      Config{RedisAddr: "rediss://cache.example.com:6379"},
			wantAddr: "cache.example.com:6379",
			wantTLS:  true,
			wantAuth: authNone,
		},
		{
			name:         "username only",
			cfg:          Config{RedisAddr: "rediss://turnip@cache.example.com:6379"},
			wantAddr:     "cache.example.com:6379",
			wantTLS:      true,
			wantAuth:     authUsername,
			wantUsername: "turnip",
		},
		{
			name:     "password only, from value",
			cfg:      Config{RedisAddr: "rediss://cache.example.com:6379", RedisPassword: "s3cret"},
			wantAddr: "cache.example.com:6379",
			wantTLS:  true,
			wantAuth: authPassword,
		},
		{
			name:         "username and password",
			cfg:          Config{RedisAddr: "rediss://turnip@cache.example.com:6379", RedisPassword: "s3cret"},
			wantAddr:     "cache.example.com:6379",
			wantTLS:      true,
			wantAuth:     authUsernamePassword,
			wantUsername: "turnip",
		},
		{
			name:       "bare form ignores a password, keeping today's behavior",
			cfg:        Config{RedisAddr: "localhost:6379", RedisPassword: "s3cret"},
			wantAddr:   "localhost:6379",
			wantTLS:    false,
			wantAuth:   authNone,
			wantNilTLS: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts, transport, err := RedisOptions(tc.cfg)
			require.NoError(t, err)
			assert.Equal(t, tc.wantAddr, opts.Addr)
			assert.Equal(t, tc.wantAddr, transport.Addr)
			assert.Equal(t, tc.wantTLS, transport.TLS)
			assert.Equal(t, tc.wantAuth, transport.Auth)
			assert.Equal(t, tc.wantUsername, opts.Username)
			assert.False(t, transport.CustomCA)
			if tc.wantNilTLS {
				assert.Nil(t, opts.TLSConfig)
			} else if tc.wantTLS {
				require.NotNil(t, opts.TLSConfig)
				assert.False(t, opts.TLSConfig.InsecureSkipVerify, "verification is always on")
			}
			// A URL password is refused elsewhere, so ParseURL never sets one;
			// the provider supplies it instead.
			assert.Empty(t, opts.Password, "the credential never lives on opts.Password")
		})
	}
}

// TestRedisOptions_URLParametersReachOptions shows the database number and a
// tuning parameter pass through to go-redis.
func TestRedisOptions_URLParametersReachOptions(t *testing.T) {
	opts, _, err := RedisOptions(Config{
		RedisAddr: "redis://cache.example.com:6379/3?max_retries=5",
	})
	require.NoError(t, err)
	assert.Equal(t, 3, opts.DB)
	assert.Equal(t, 5, opts.MaxRetries)
}

// TestRedisOptions_Refusals covers every startup refusal and its message.
func TestRedisOptions_Refusals(t *testing.T) {
	for _, tc := range []struct {
		name        string
		cfg         Config
		wantInError []string
	}{
		{
			name:        "password in the URL",
			cfg:         Config{RedisAddr: "rediss://user:secret@cache.example.com:6379"},
			wantInError: []string{"TURNIP_REDIS_PASSWORD_PATH", "TURNIP_REDIS_PASSWORD"},
		},
		{
			name:        "skip_verify true",
			cfg:         Config{RedisAddr: "rediss://cache.example.com:6379?skip_verify=true"},
			wantInError: []string{"skip_verify"},
		},
		{
			name:        "skip_verify false is still refused by presence",
			cfg:         Config{RedisAddr: "rediss://cache.example.com:6379?skip_verify=false"},
			wantInError: []string{"skip_verify"},
		},
		{
			name:        "unix scheme",
			cfg:         Config{RedisAddr: "unix:///var/run/redis.sock"},
			wantInError: []string{"unix", "not supported"},
		},
		{
			name:        "http scheme",
			cfg:         Config{RedisAddr: "http://cache.example.com:6379"},
			wantInError: []string{"http", "not supported"},
		},
		{
			name:        "unknown query parameter",
			cfg:         Config{RedisAddr: "redis://cache.example.com:6379?bogus=1"},
			wantInError: []string{"bogus"},
		},
		{
			name:        "CA path with a bare address",
			cfg:         Config{RedisAddr: "localhost:6379", RedisCAPath: "/tmp/ca.pem"},
			wantInError: []string{"TURNIP_REDIS_CA_PATH", "rediss"},
		},
		{
			name:        "CA path with a redis (non-TLS) address",
			cfg:         Config{RedisAddr: "redis://cache.example.com:6379", RedisCAPath: "/tmp/ca.pem"},
			wantInError: []string{"TURNIP_REDIS_CA_PATH", "rediss"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := RedisOptions(tc.cfg)
			require.Error(t, err)
			for _, want := range tc.wantInError {
				assert.Contains(t, err.Error(), want)
			}
		})
	}
}

// TestRedisOptions_CABundle covers a loaded bundle, an empty one, and an
// unreadable path.
func TestRedisOptions_CABundle(t *testing.T) {
	t.Run("loaded", func(t *testing.T) {
		path := writeTemp(t, "ca.pem", string(testCAPEM(t)))
		opts, transport, err := RedisOptions(Config{
			RedisAddr:   "rediss://cache.example.com:6379",
			RedisCAPath: path,
		})
		require.NoError(t, err)
		require.NotNil(t, opts.TLSConfig)
		assert.NotNil(t, opts.TLSConfig.RootCAs, "the bundle replaced the system roots")
		assert.True(t, transport.CustomCA)
	})

	t.Run("empty of certificates", func(t *testing.T) {
		path := writeTemp(t, "ca.pem", "not a certificate\n")
		_, _, err := RedisOptions(Config{
			RedisAddr:   "rediss://cache.example.com:6379",
			RedisCAPath: path,
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "TURNIP_REDIS_CA_PATH")
		assert.Contains(t, err.Error(), "no PEM certificate")
	})

	t.Run("unreadable", func(t *testing.T) {
		_, _, err := RedisOptions(Config{
			RedisAddr:   "rediss://cache.example.com:6379",
			RedisCAPath: filepath.Join(t.TempDir(), "does-not-exist.pem"),
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "TURNIP_REDIS_CA_PATH")
	})
}

// TestRedisOptions_PasswordProvider covers the per-connection credentials
// provider: a value, a file, a rewritten file, a trimmed trailing newline,
// and a missing file failing at startup.
func TestRedisOptions_PasswordProvider(t *testing.T) {
	t.Run("value", func(t *testing.T) {
		opts, _, err := RedisOptions(Config{
			RedisAddr:     "rediss://turnip@cache.example.com:6379",
			RedisPassword: "s3cret",
		})
		require.NoError(t, err)
		require.NotNil(t, opts.CredentialsProviderContext)
		user, pass, err := opts.CredentialsProviderContext(context.Background())
		require.NoError(t, err)
		assert.Equal(t, "turnip", user)
		assert.Equal(t, "s3cret", pass)
	})

	t.Run("file, trailing newline trimmed", func(t *testing.T) {
		path := writeTemp(t, "pw", "from-file\n")
		opts, _, err := RedisOptions(Config{
			RedisAddr:         "rediss://cache.example.com:6379",
			RedisPasswordPath: path,
		})
		require.NoError(t, err)
		require.NotNil(t, opts.CredentialsProviderContext)
		_, pass, err := opts.CredentialsProviderContext(context.Background())
		require.NoError(t, err)
		assert.Equal(t, "from-file", pass)
	})

	t.Run("file re-read on each call", func(t *testing.T) {
		path := writeTemp(t, "pw", "v1\n")
		opts, _, err := RedisOptions(Config{
			RedisAddr:         "rediss://cache.example.com:6379",
			RedisPasswordPath: path,
		})
		require.NoError(t, err)
		_, pass, err := opts.CredentialsProviderContext(context.Background())
		require.NoError(t, err)
		assert.Equal(t, "v1", pass)

		require.NoError(t, os.WriteFile(path, []byte("v2\n"), 0o600))
		_, pass, err = opts.CredentialsProviderContext(context.Background())
		require.NoError(t, err)
		assert.Equal(t, "v2", pass, "a rotated password is picked up without a restart")
	})

	t.Run("missing file fails at startup", func(t *testing.T) {
		_, _, err := RedisOptions(Config{
			RedisAddr:         "rediss://cache.example.com:6379",
			RedisPasswordPath: filepath.Join(t.TempDir(), "absent"),
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "TURNIP_REDIS_PASSWORD_PATH")
	})
}

// TestRedisOptions_NoCredentialInTransport asserts the Transport never carries
// the secret, only its shape.
func TestRedisOptions_NoCredentialInTransport(t *testing.T) {
	const secret = "top-secret-token"
	_, transport, err := RedisOptions(Config{
		RedisAddr:     "rediss://turnip@cache.example.com:6379",
		RedisPassword: secret,
	})
	require.NoError(t, err)
	rendered := fmt.Sprintf("%+v", transport)
	assert.NotContains(t, rendered, secret, "the Transport must not render the credential")
	assert.Equal(t, "cache.example.com:6379", transport.Addr, "no userinfo in the reported address")
	assert.Equal(t, authUsernamePassword, transport.Auth)
}

// TestRedisOptions_MalformedURLDoesNotLeakPassword guards the security
// requirement that no credential reaches the error returned to the caller
// (which cmd/server logs). A password with a character url.Parse rejects (a
// bare "%") makes url.Parse fail before the password-in-URL refusal can run;
// wrapping the *url.Error would embed the raw address, and with it the
// password, in the error. The refusal must still be credential-free.
func TestRedisOptions_MalformedURLDoesNotLeakPassword(t *testing.T) {
	const secret = "sup3rSECRET"
	_, _, err := RedisOptions(Config{
		RedisAddr: "rediss://user:" + secret + "%@cache.example.com:6379",
	})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), secret, "a malformed address must not leak the password in the error")
	assert.Contains(t, err.Error(), "TURNIP_REDIS_ADDR")
}

// TestRedisOptions_SchemelessCredentialsDoNotLeak guards the same
// requirement for an address with credentials but no scheme. Taken as a
// bare host:port, it would reach the Transport, and so /readyz (on 503
// too) and the startup line. It must be refused without echoing it.
func TestRedisOptions_SchemelessCredentialsDoNotLeak(t *testing.T) {
	const secret = "sup3rSECRET"
	opts, transport, err := RedisOptions(Config{
		RedisAddr: "user:" + secret + "@cache.example.com:6379",
	})
	require.Error(t, err)
	assert.Nil(t, opts)
	assert.Empty(t, transport.Addr, "nothing about the address may reach the Transport")
	assert.NotContains(t, err.Error(), secret, "the refusal must not echo the password")
	assert.Contains(t, err.Error(), "TURNIP_REDIS_PASSWORD_PATH")
}

// TestConfigFromEnv_RedisPasswordForms covers reading the three settings and
// the both-password-forms startup error.
func TestConfigFromEnv_RedisPasswordForms(t *testing.T) {
	t.Run("path read into Config", func(t *testing.T) {
		cfg, err := ConfigFromEnv(envMap(map[string]string{
			"TURNIP_REDIS_PASSWORD_PATH": "/run/secrets/redis",
			"TURNIP_REDIS_CA_PATH":       "/run/secrets/ca.pem",
		}), NewPluginRegistry())
		require.NoError(t, err)
		assert.Equal(t, "/run/secrets/redis", cfg.RedisPasswordPath)
		assert.Empty(t, cfg.RedisPassword)
		assert.Equal(t, "/run/secrets/ca.pem", cfg.RedisCAPath)
	})

	t.Run("value read into Config", func(t *testing.T) {
		cfg, err := ConfigFromEnv(envMap(map[string]string{
			"TURNIP_REDIS_PASSWORD": "inline",
		}), NewPluginRegistry())
		require.NoError(t, err)
		assert.Equal(t, "inline", cfg.RedisPassword)
		assert.Empty(t, cfg.RedisPasswordPath)
	})

	t.Run("both forms is a startup error", func(t *testing.T) {
		_, err := ConfigFromEnv(envMap(map[string]string{
			"TURNIP_REDIS_PASSWORD":      "inline",
			"TURNIP_REDIS_PASSWORD_PATH": "/run/secrets/redis",
		}), NewPluginRegistry())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "TURNIP_REDIS_PASSWORD_PATH")
		assert.Contains(t, err.Error(), "TURNIP_REDIS_PASSWORD")
		assert.Contains(t, err.Error(), "not both")
	})

	t.Run("neither set is fine", func(t *testing.T) {
		cfg, err := ConfigFromEnv(envMap(nil), NewPluginRegistry())
		require.NoError(t, err)
		assert.Empty(t, cfg.RedisPassword)
		assert.Empty(t, cfg.RedisPasswordPath)
		assert.Empty(t, cfg.RedisCAPath)
	})
}
