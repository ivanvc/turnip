package http

import (
	"net/http"
)

// statusHandler holds the endpoint to query the status of the service.
type statusHandler struct{}

// Registers the handler to be used by an HTTP server.
func (h *statusHandler) registerHandler() {
	http.HandleFunc("/healthz", func(res http.ResponseWriter, req *http.Request) {
		res.WriteHeader(http.StatusOK)
	})
}
