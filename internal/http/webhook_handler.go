package http

import (
	"encoding/json"
	"net/http"

	"github.com/charmbracelet/log"

	"github.com/ivanvc/turnip/internal/adapters/github"
)

// webhookHandler holds the HTTP endpoint to handle GitHub's webhook.
type webhookHandler struct{}

// Registers the handler to be used by an HTTP server.
func (h *webhookHandler) registerHandler(s *Server) {
	http.HandleFunc("/webhooks/github/payload", h.handle(s))
}

// Handles the HTTP request.
func (h *webhookHandler) handle(s *Server) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		switch req.Header.Get("X-Github-Event") {
		case "issue_comment":
			decoder := json.NewDecoder(req.Body)
			var ic github.IssueComment
			if err := decoder.Decode(&ic); err != nil {
				log.Error("Error unmarshalling", "error", err)
			}
			log.Info("Payload", "payload", ic)
			ic.PullRequest = *s.Common.GitHubClient.GetPullRequestFromIssueComment(&ic)
			log.Info("After PR", "payload", ic)
			if err := s.Common.KubernetesClient.CreateJob(&ic); err != nil {
				log.Error("Error creating job", "error", err)
			}
		}

		w.WriteHeader(http.StatusOK)
	}
}
