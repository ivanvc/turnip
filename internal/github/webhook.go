package github

import (
	"context"
	"errors"
	"log/slog"
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

// ErrRefused reports that a handler declined to act on a well-formed,
// correctly-signed delivery. Nothing failed — turnip understood the
// delivery and chose not to execute it.
//
// The distinction is load-bearing at the HTTP boundary. A handler error
// answers 500, which makes GitHub retry; a refusal answers 200, because a
// retry would reach the same decision. It also changes how the delivery
// is counted: rejected rather than dispatched, so one delivery remains
// one counted outcome.
var ErrRefused = errors.New("github: delivery refused")

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
	ctx := r.Context()
	// GitHub's own ID for this delivery: what an operator searches for in
	// the App's "Recent Deliveries" page to match a log line to a payload.
	deliveryID := gh.DeliveryID(r)

	payload, err := gh.ValidatePayload(r, h.secret)
	if err != nil {
		// eventType isn't known yet at this point — signature
		// verification happens before we even look at the event type.
		metrics.WebhookEvent("unknown", "rejected")
		slog.WarnContext(ctx, "rejecting webhook delivery: signature verification failed",
			"delivery_id", deliveryID, "error", err)
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	eventType := gh.WebHookType(r)
	if eventType != "pull_request" && eventType != "issue_comment" {
		metrics.WebhookEvent(eventType, "skipped")
		slog.DebugContext(ctx, "skipping webhook delivery", "event", eventType, "delivery_id", deliveryID)
		w.WriteHeader(http.StatusOK)
		return
	}

	raw, err := gh.ParseWebHook(eventType, payload)
	if err != nil {
		metrics.WebhookEvent(eventType, "rejected")
		slog.WarnContext(ctx, "rejecting webhook delivery: unparseable payload",
			"event", eventType, "delivery_id", deliveryID, "error", err)
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
		slog.DebugContext(ctx, "skipping webhook delivery", "event", eventType, "delivery_id", deliveryID)
		w.WriteHeader(http.StatusOK)
		return
	}

	attrs := []any{
		"event", eventType,
		"action", event.Action,
		"owner", event.Repository.Owner,
		"repo", event.Repository.Name,
		"delivery_id", deliveryID,
	}
	if event.PullRequest != nil {
		attrs = append(attrs, "pr_number", event.PullRequest.Number)
	}
	slog.InfoContext(ctx, "handling webhook delivery", attrs...)

	var handleErr error
	switch eventType {
	case "pull_request":
		handleErr = h.handler.HandlePullRequest(ctx, event)
	case "issue_comment":
		handleErr = h.handler.HandleIssueComment(ctx, event)
	}

	// Checked before the error branch below, and instead of the
	// "dispatched" count: a refused delivery is one delivery with one
	// outcome, so counting it both ways would break the arithmetic that
	// makes a rate of refusals meaningful.
	if errors.Is(handleErr, ErrRefused) {
		metrics.WebhookEvent(eventType, "rejected")
		// 200, deliberately. The delivery arrived intact and was
		// understood; turnip declined to act on it, and a retry would
		// reach the same decision.
		w.WriteHeader(http.StatusOK)
		return
	}

	metrics.WebhookEvent(eventType, "dispatched")
	if handleErr != nil {
		// The one place this error is logged: the orchestrator returns it
		// rather than logging it, and without this line a 500 left only
		// a metric behind.
		slog.ErrorContext(ctx, "handling webhook delivery failed", append(attrs, "error", handleErr)...)
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
			Author:  e.GetPullRequest().GetUser().GetLogin(),
			// Nil-safe through go-github's accessors: a fork deleted
			// after the pull request was opened yields a zero
			// Repository, which IsForeign treats as foreign.
			HeadRepo: repositoryFrom(e.GetPullRequest().GetHead().GetRepo()),
			Draft:    e.GetPullRequest().GetDraft(),
			// GetMerged is deliberately not read: GitHub reports a merged
			// pull request as "closed", so treating merged and closed
			// alike needs no rule of turnip's own.
			Open: e.GetPullRequest().GetState() == "open",
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
