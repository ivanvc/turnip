package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivanvc/turnip/internal/github"
)

// recordingHandler stands in for the webhook handler and records whether
// it was reached.
//
// The record is the point. "An unmatched path does not reach the webhook
// handler" is a claim about what did *not* happen, and a status code
// cannot express it: a handler that ran and refused the request can
// return 404 just as a mux that never routed it does. Only the flag
// distinguishes the two.
type recordingHandler struct {
	called bool
}

func (h *recordingHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.called = true
	w.WriteHeader(http.StatusOK)
}

func readyAlways(context.Context) error { return nil }

// TestNewMux_Routing is design.md's routing table, row by row.
//
// It exercises newMux directly rather than a running server: run dials
// Redis and Kubernetes and then blocks, and none of that is required to
// assert which path reaches which handler.
func TestNewMux_Routing(t *testing.T) {
	tests := []struct {
		name        string
		method      string
		path        string
		wantStatus  int
		wantHandler bool
	}{
		{
			name:   "the webhook is served at its own path",
			method: http.MethodPost, path: github.WebhookPath,
			wantStatus: http.StatusOK, wantHandler: true,
		},
		{
			// The handler performs no method check of its own — ServeHTTP
			// goes straight to signature validation — so without the
			// method in the pattern this would be answered 401.
			name:   "a wrong method on the webhook path is 405, not 401",
			method: http.MethodGet, path: github.WebhookPath,
			wantStatus: http.StatusMethodNotAllowed, wantHandler: false,
		},
		{
			name:   "the root path no longer routes to the webhook",
			method: http.MethodPost, path: "/",
			wantStatus: http.StatusNotFound, wantHandler: false,
		},
		{
			name:   "the root path is not served at all",
			method: http.MethodGet, path: "/",
			wantStatus: http.StatusNotFound, wantHandler: false,
		},
		{
			name:   "an unmatched path 404s without reaching the handler",
			method: http.MethodPost, path: "/anything-else",
			wantStatus: http.StatusNotFound, wantHandler: false,
		},
		{
			// No trailing slash in the pattern, so it matches that exact
			// path only — a subtree match would quietly re-create a
			// smaller catch-all.
			name:   "the webhook path does not match a subtree",
			method: http.MethodPost, path: github.WebhookPath + "/",
			wantStatus: http.StatusNotFound, wantHandler: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			webhook := &recordingHandler{}
			mux := newMux(webhook, readyAlways)

			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, httptest.NewRequest(tt.method, tt.path, nil))

			assert.Equal(t, tt.wantStatus, rec.Code)
			assert.Equal(t, tt.wantHandler, webhook.called,
				"whether the webhook handler was reached")
		})
	}
}

// The operational paths are untouched by this slice, and a regression
// there would be far quieter than a webhook one — nothing posts a comment
// when a liveness probe starts 404ing.
func TestNewMux_OperationalPathsUnchanged(t *testing.T) {
	webhook := &recordingHandler{}
	mux := newMux(webhook, readyAlways)

	for _, path := range []string{"/healthz", "/readyz", "/metrics"} {
		t.Run(path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))

			require.Equal(t, http.StatusOK, rec.Code)
			assert.False(t, webhook.called, "an operational path must never reach the webhook handler")
		})
	}
}
