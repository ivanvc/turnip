package orchestrator

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/redis/go-redis/v9"
)

// The kinds of AUTH a Transport can report. They describe what the Server
// presents to Redis, never the secret itself.
const (
	authNone             = "none"
	authUsername         = "username"
	authPassword         = "password"
	authUsernamePassword = "username+password"
)

// Transport is the loggable, credential-free description of how the Server
// reached Redis: the address (host:port, no userinfo or query), whether TLS
// was negotiated, which AUTH shape was used, and whether a private CA bundle
// replaced the system roots. It is what the startup log line and /readyz
// report (design Decision 6). It deliberately holds no password: RedisOptions
// must never copy a credential into it.
type Transport struct {
	// Addr is host:port, rebuilt from the parsed URL (or the bare setting),
	// never the raw TURNIP_REDIS_ADDR text, so no userinfo can leak through.
	Addr string
	// TLS is true for a rediss:// address.
	TLS bool
	// Auth is one of authNone, authUsername, authPassword or
	// authUsernamePassword.
	Auth string
	// CustomCA is true when TURNIP_REDIS_CA_PATH replaced the system roots.
	CustomCA bool
}

// RedisOptions turns the Server's Config into go-redis options plus a
// credential-free Transport record, or a startup error naming the fix. It is
// exported for cmd/server, which builds the client and logs the Transport.
//
// Behavior, in order (design Decisions 1-6):
//   - a bare host:port (no "://") keeps turnip's original Options{Addr: value}
//     exactly: no TLS, no AUTH. A CA path alongside it is a startup error.
//   - otherwise the scheme must be redis or rediss; unix:// and anything else
//     are refused (a Server Pod has no Redis socket to reach).
//   - skip_verify is refused by its mere presence on the query, whatever its
//     value, before ParseURL could act on it: turnip always verifies the
//     server's certificate.
//   - a password in the URL userinfo is refused, naming the password settings:
//     TURNIP_REDIS_ADDR is delivered through a ConfigMap, not a Secret.
//   - ParseURL then reads the username, database number and tuning parameters,
//     and itself refuses any query key it does not recognize.
//   - for rediss, RootCAs come from TURNIP_REDIS_CA_PATH when set (an
//     unreadable or certificate-free bundle is a startup error) and the system
//     roots otherwise; verification is always on.
//   - the password, from the file or the value, is supplied through go-redis's
//     per-connection CredentialsProviderContext, which re-reads the file each
//     call so a rotated token needs no restart (trailing newline trimmed); the
//     file is also read once here so a missing file fails at startup.
func RedisOptions(cfg Config) (*redis.Options, Transport, error) {
	addr := cfg.RedisAddr

	// The bare host:port form predates this change and is left untouched.
	if !strings.Contains(addr, "://") {
		// A bare host:port never carries userinfo. One that does is
		// credentials pasted without a scheme; passing it through would put
		// them in the Transport, and so in /readyz and the startup line.
		// The address is not echoed, since it may hold a password.
		if strings.Contains(addr, "@") {
			return nil, Transport{}, fmt.Errorf(
				"orchestrator: TURNIP_REDIS_ADDR looks like it carries credentials but has no redis:// or rediss:// scheme; put the password in TURNIP_REDIS_PASSWORD_PATH (a file) or TURNIP_REDIS_PASSWORD (a value) and use a URL for a username",
			)
		}
		if cfg.RedisCAPath != "" {
			return nil, Transport{}, fmt.Errorf(
				"orchestrator: TURNIP_REDIS_CA_PATH is set but TURNIP_REDIS_ADDR %q is not a rediss:// URL; the bundle would verify nothing. Use a rediss:// address or unset TURNIP_REDIS_CA_PATH",
				addr,
			)
		}
		return &redis.Options{Addr: addr}, Transport{Addr: addr, Auth: authNone}, nil
	}

	u, err := url.Parse(addr)
	if err != nil {
		// Do not wrap the *url.Error: its Error() embeds the raw address,
		// which may hold a password. A malformed password (e.g. one with a
		// bare "%", a space or a control character) is exactly what makes
		// url.Parse fail here, before the password-in-URL refusal below can
		// run, so wrapping it would log the credential. Return a
		// credential-free message naming the likely fix instead.
		return nil, Transport{}, fmt.Errorf(
			"orchestrator: TURNIP_REDIS_ADDR is not a valid URL; if it carries a password, set TURNIP_REDIS_PASSWORD_PATH (a file) or TURNIP_REDIS_PASSWORD (a value) instead",
		)
	}

	switch u.Scheme {
	case "redis", "rediss":
	default:
		return nil, Transport{}, fmt.Errorf(
			"orchestrator: TURNIP_REDIS_ADDR scheme %q is not supported; use redis://, rediss:// or a bare host:port",
			u.Scheme,
		)
	}
	tlsEnabled := u.Scheme == "rediss"

	// Refuse skip_verify by presence, whatever its value, before ParseURL
	// would read it into InsecureSkipVerify.
	if q, err := url.ParseQuery(u.RawQuery); err == nil {
		if _, ok := q["skip_verify"]; ok {
			return nil, Transport{}, fmt.Errorf(
				"orchestrator: TURNIP_REDIS_ADDR must not set skip_verify; turnip always verifies the Redis server's certificate. Remove skip_verify from the address",
			)
		}
	}

	// Refuse a password in the URL: it would land in a ConfigMap and be echoed.
	if u.User != nil {
		if _, hasPassword := u.User.Password(); hasPassword {
			return nil, Transport{}, fmt.Errorf(
				"orchestrator: TURNIP_REDIS_ADDR must not carry a password in the URL; set TURNIP_REDIS_PASSWORD_PATH (a file) or TURNIP_REDIS_PASSWORD (a value) instead",
			)
		}
	}

	opts, err := redis.ParseURL(addr)
	if err != nil {
		return nil, Transport{}, fmt.Errorf("orchestrator: parsing TURNIP_REDIS_ADDR: %w", err)
	}

	customCA := false
	if tlsEnabled {
		if opts.TLSConfig == nil {
			opts.TLSConfig = &tls.Config{ServerName: u.Hostname()}
		}
		opts.TLSConfig.InsecureSkipVerify = false
		if cfg.RedisCAPath != "" {
			pool, err := loadRedisCABundle(cfg.RedisCAPath)
			if err != nil {
				return nil, Transport{}, err
			}
			opts.TLSConfig.RootCAs = pool
			customCA = true
		}
	} else if cfg.RedisCAPath != "" {
		return nil, Transport{}, fmt.Errorf(
			"orchestrator: TURNIP_REDIS_CA_PATH is set but TURNIP_REDIS_ADDR %q does not use rediss://; the bundle would verify nothing. Use a rediss:// address or unset TURNIP_REDIS_CA_PATH",
			addr,
		)
	}

	// ParseURL set opts.Username from the userinfo; opts.Password is empty
	// (a URL password was refused above). The credential comes from the file
	// or the value, never from the address.
	username := opts.Username
	hasPassword := cfg.RedisPassword != "" || cfg.RedisPasswordPath != ""
	if hasPassword {
		read := redisPasswordReader(cfg)
		// Read once now so a missing or unreadable file fails at startup
		// rather than only when the first connection is opened.
		if _, err := read(); err != nil {
			return nil, Transport{}, err
		}
		opts.CredentialsProviderContext = func(ctx context.Context) (string, string, error) {
			password, err := read()
			if err != nil {
				return "", "", err
			}
			return username, password, nil
		}
	}

	transport := Transport{
		Addr:     opts.Addr,
		TLS:      tlsEnabled,
		Auth:     redisAuthKind(username, hasPassword),
		CustomCA: customCA,
	}
	return opts, transport, nil
}

// redisPasswordReader returns a function that yields the current AUTH
// password. For a file it re-reads on every call, so a Secret rotated in
// place is picked up without a restart, trimming a trailing newline (how a
// mounted Secret file is commonly written). For a literal value it returns a
// constant.
func redisPasswordReader(cfg Config) func() (string, error) {
	if cfg.RedisPasswordPath != "" {
		path := cfg.RedisPasswordPath
		return func() (string, error) {
			b, err := os.ReadFile(path)
			if err != nil {
				return "", fmt.Errorf("orchestrator: reading TURNIP_REDIS_PASSWORD_PATH: %w", err)
			}
			return strings.TrimRight(string(b), "\r\n"), nil
		}
	}
	value := cfg.RedisPassword
	return func() (string, error) { return value, nil }
}

// redisAuthKind names the AUTH shape for the Transport record.
func redisAuthKind(username string, hasPassword bool) string {
	switch {
	case username != "" && hasPassword:
		return authUsernamePassword
	case username != "":
		return authUsername
	case hasPassword:
		return authPassword
	default:
		return authNone
	}
}

// loadRedisCABundle reads a PEM bundle into a cert pool, failing at startup if
// it is unreadable or holds no certificate (a bundle that parsed to nothing
// would silently fall back to trusting nothing).
func loadRedisCABundle(path string) (*x509.CertPool, error) {
	pem, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("orchestrator: reading TURNIP_REDIS_CA_PATH: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf(
			"orchestrator: TURNIP_REDIS_CA_PATH %q holds no PEM certificate; provide a bundle with at least one certificate",
			path,
		)
	}
	return pool, nil
}
