package github

import (
	"context"
	"net/http"

	gh "github.com/google/go-github/v90/github"

	"github.com/ivanvc/turnip/internal/metrics"
)

// WebhookPath is where the Server serves GitHub webhook deliveries.
//
// It is published — an operator types it into the GitHub App's settings —
// so it is defined once and referenced, never spelled out at a call site.
// Two literals that must agree are exactly what a constant is for, and
// here the cost of them disagreeing lands on an operator rather than
// being caught by a compiler.
//
// Namespaced by forge rather than by resource (`/webhook`) so that a
// second forge, or another GitHub-specific endpoint such as an OAuth
// callback, needs no further migration of a published URL.
const WebhookPath = "/github/webhook"

// EventHandler reacts to parsed, signature-verified webhook events. Slice 6
// implements this interface; NewWebhookHandler only dispatches to it.
type EventHandler interface {
	HandlePullRequest(ctx context.Context, event *WebhookEvent) error
	HandleIssueComment(ctx context.Context, event *WebhookEvent) error
}

// NewWebhookHandler returns an http.Handler that verifies the
// X-Hub-Signature-256 header against secret, parses pull_request and
// issue_comment payloads into a WebhookEvent, and dispatches to handler.
// Every other event type gets HTTP 200 with no dispatch.
func NewWebhookHandler(secret []byte, handler EventHandler) http.Handler {
	return &webhookHandler{secret: secret, handler: handler}
}

type webhookHandler struct {
	secret  []byte
	handler EventHandler
}

func (h *webhookHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	payload, err := gh.ValidatePayload(r, h.secret)
	if err != nil {
		// eventType isn't known yet at this point — signature
		// verification happens before we even look at the event type.
		metrics.WebhookEvent("unknown", "rejected")
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	eventType := gh.WebHookType(r)
	if eventType != "pull_request" && eventType != "issue_comment" {
		metrics.WebhookEvent(eventType, "skipped")
		w.WriteHeader(http.StatusOK)
		return
	}

	raw, err := gh.ParseWebHook(eventType, payload)
	if err != nil {
		metrics.WebhookEvent(eventType, "rejected")
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	var event *WebhookEvent
	var dispatch bool
	switch eventType {
	case "pull_request":
		event, dispatch = pullRequestWebhookEvent(raw.(*gh.PullRequestEvent)), true
	case "issue_comment":
		event, dispatch = issueCommentWebhookEvent(raw.(*gh.IssueCommentEvent))
	}
	if !dispatch {
		metrics.WebhookEvent(eventType, "skipped")
		w.WriteHeader(http.StatusOK)
		return
	}

	var handleErr error
	switch eventType {
	case "pull_request":
		handleErr = h.handler.HandlePullRequest(r.Context(), event)
	case "issue_comment":
		handleErr = h.handler.HandleIssueComment(r.Context(), event)
	}

	metrics.WebhookEvent(eventType, "dispatched")
	if handleErr != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func pullRequestWebhookEvent(e *gh.PullRequestEvent) *WebhookEvent {
	return &WebhookEvent{
		Type:       "pull_request",
		Action:     e.GetAction(),
		Repository: repositoryFrom(e.GetRepo()),
		PullRequest: &PullRequest{
			Number:  e.GetNumber(),
			HeadSHA: e.GetPullRequest().GetHead().GetSHA(),
			BaseRef: e.GetPullRequest().GetBase().GetRef(),
			HeadRef: e.GetPullRequest().GetHead().GetRef(),
			Draft:   e.GetPullRequest().GetDraft(),
		},
		Installation: Installation{ID: e.GetInstallation().GetID()},
	}
}

// issueCommentWebhookEvent maps e into a WebhookEvent. The second return
// value is false when the comment is on a plain issue, not a PR — not a
// Trigger Comment candidate, so the caller should not dispatch it.
func issueCommentWebhookEvent(e *gh.IssueCommentEvent) (*WebhookEvent, bool) {
	if !e.GetIssue().IsPullRequest() {
		return nil, false
	}
	return &WebhookEvent{
		Type:       "issue_comment",
		Action:     e.GetAction(),
		Repository: repositoryFrom(e.GetRepo()),
		PullRequest: &PullRequest{
			Number: e.GetIssue().GetNumber(),
		},
		Comment: &Comment{
			ID:     e.GetComment().GetID(),
			Body:   e.GetComment().GetBody(),
			Author: e.GetComment().GetUser().GetLogin(),
		},
		Installation: Installation{ID: e.GetInstallation().GetID()},
	}, true
}

func repositoryFrom(r *gh.Repository) Repository {
	return Repository{
		Owner: r.GetOwner().GetLogin(),
		Name:  r.GetName(),
		URL:   r.GetHTMLURL(),
	}
}
