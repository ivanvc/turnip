package http

import (
	"encoding/json"
	"net/http"

	"github.com/charmbracelet/log"

	"github.com/ivanvc/turnip/internal/adapters/github/handlers"
	"github.com/ivanvc/turnip/internal/adapters/github/objects"
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
			var ic objects.IssueComment
			if err := decoder.Decode(&ic); err != nil {
				log.Error("Error unmarshalling", "error", err)
				w.WriteHeader(http.StatusInternalServerError)
				return
			}

			if err := handlers.HandleIssueComment(s.Common, &ic); err != nil {
				log.Error("Error handling issue comment", "error", err)
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
		case "pull_request":
			decoder := json.NewDecoder(req.Body)
			var pr objects.PullRequestWebhook
			if err := decoder.Decode(&pr); err != nil {
				log.Error("Error unmarshalling", "error", err)
				w.WriteHeader(http.StatusInternalServerError)
				return
			}

			if err := handlers.HandlePullRequest(s.Common, &pr); err != nil {
				log.Error("Error handling pull request", "error", err)
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
		}

		w.WriteHeader(http.StatusOK)
	}
}
