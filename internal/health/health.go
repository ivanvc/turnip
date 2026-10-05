package health

import (
	"context"
	"encoding/json"
	"net/http"
)

// Healthz returns a handler that always responds 200 once the process is
// serving HTTP at all — "the process is up," not "the process can serve a
// webhook." It deliberately does not check Redis or Kubernetes
// reachability, so a transient dependency blip doesn't restart an
// otherwise-fine Server Pod; that distinction belongs to Readyz.
func Healthz() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}
}

// RedisTransport describes how the Server reaches Redis. It carries no
// credential, only facts about the connection, so it is safe to serve.
type RedisTransport struct {
	// Addr is host:port, never a URL and never with userinfo.
	Addr string `json:"addr"`
	// TLS reports whether the connection is encrypted.
	TLS bool `json:"tls"`
	// Auth is one of none, username, password, username+password.
	Auth string `json:"auth"`
	// CustomCA reports whether a custom CA bundle is in use.
	CustomCA bool `json:"custom_ca"`
}

type readyzBody struct {
	Redis RedisTransport `json:"redis"`
}

// Readyz returns a handler that responds 200 only when ping currently
// succeeds, and 503 otherwise. Both responses carry a JSON body reporting
// the Redis transport under "redis". The body never includes a credential
// or the ping error.
func Readyz(ping func(context.Context) error, transport RedisTransport) http.HandlerFunc {
	body, _ := json.Marshal(readyzBody{Redis: transport})
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := ping(r.Context()); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
		} else {
			w.WriteHeader(http.StatusOK)
		}
		_, _ = w.Write(body)
	}
}
