package health

import (
	"context"
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

// Readyz returns a handler that responds 200 only when ping currently
// succeeds, and 503 otherwise.
func Readyz(ping func(context.Context) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := ping(r.Context()); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}
}
