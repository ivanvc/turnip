package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"

	"github.com/charmbracelet/log"
)

// webhookHandler holds the HTTP endpoint to handle GitHub's webhook.
type webhookHandler struct{}

// Registers the handler to be used by an HTTP server.
func (h *webhookHandler) registerHandler() {
	http.HandleFunc("/webhooks/github/payload", h.handle)
}

// Handles the HTTP request.
func (h *webhookHandler) handle(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	dump, err := httputil.DumpRequest(req, true)
	if err != nil {
		log.Error("Error dumping request", "err", err)
		http.Error(w, fmt.Sprint(err), http.StatusInternalServerError)
		return
	}

	var v map[string]any
	var b bytes.Buffer
	if req.Body != nil {
		io.Copy(&b, req.Body)
	}

	if err := json.Unmarshal(b.Bytes(), &v); err != nil {
		log.Error("Error unmarshalling", "error", err)
	}
	log.Info("Dump of request", "dump", string(dump), "body", v)

	w.WriteHeader(http.StatusOK)
}
