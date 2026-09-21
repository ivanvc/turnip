package github

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	gh "github.com/google/go-github/v90/github"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	testWebhookSecret = []byte("test-secret")
	errBoom           = errors.New("boom")
)

type recordingEventHandler struct {
	mu                sync.Mutex
	pullRequestCalls  int
	issueCommentCalls int
	lastEvent         *WebhookEvent
	returnErr         error
}

func (r *recordingEventHandler) HandlePullRequest(ctx context.Context, event *WebhookEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pullRequestCalls++
	r.lastEvent = event
	return r.returnErr
}

func (r *recordingEventHandler) HandleIssueComment(ctx context.Context, event *WebhookEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.issueCommentCalls++
	r.lastEvent = event
	return r.returnErr
}

func (r *recordingEventHandler) calls() (pr, comment int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.pullRequestCalls, r.issueCommentCalls
}

func signedWebhookRequest(t *testing.T, eventType string, payload []byte) *http.Request {
	t.Helper()

	mac := hmac.New(sha256.New, testWebhookSecret)
	mac.Write(payload)
	signature := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(string(payload)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(gh.EventTypeHeader, eventType)
	req.Header.Set(gh.SHA256SignatureHeader, signature)
	return req
}

func testPullRequestPayload(t *testing.T) []byte {
	t.Helper()
	event := &gh.PullRequestEvent{
		Action: gh.Ptr("opened"),
		Number: gh.Ptr(42),
		PullRequest: &gh.PullRequest{
			Head: &gh.PullRequestBranch{SHA: gh.Ptr("abc123"), Ref: gh.Ptr("feature")},
			Base: &gh.PullRequestBranch{Ref: gh.Ptr("main")},
		},
		Repo: &gh.Repository{
			Name:    gh.Ptr("repo"),
			Owner:   &gh.User{Login: gh.Ptr("owner")},
			HTMLURL: gh.Ptr("https://github.com/owner/repo"),
		},
		Installation: &gh.Installation{ID: gh.Ptr(int64(555))},
	}
	payload, err := json.Marshal(event)
	require.NoError(t, err)
	return payload
}

// A pull_request payload's draft flag must survive parsing: it is the
// only thing distinguishing a draft from any other pull request, since
// GitHub sends the same "opened" and "synchronize" actions for both.
func TestWebhookHandler_PullRequestCarriesDraftFlag(t *testing.T) {
	for _, tc := range []struct {
		name  string
		draft bool
	}{
		{"draft", true},
		{"not a draft", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handler := &recordingEventHandler{}
			h := NewWebhookHandler(testWebhookSecret, handler)

			req := signedWebhookRequest(t, "pull_request", draftPullRequestPayload(t, tc.draft))
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)

			require.Equal(t, http.StatusOK, w.Code)
			require.NotNil(t, handler.lastEvent)
			require.NotNil(t, handler.lastEvent.PullRequest)
			assert.Equal(t, tc.draft, handler.lastEvent.PullRequest.Draft)
		})
	}
}

// The head repository must survive parsing, because refusing a fork's
// pull request depends entirely on knowing where its code came from. An
// unmapped field would read as empty, and IsForeign treats empty as
// foreign — so a mapping failure here does not fail open, it refuses
// every pull request in the repository.
func TestWebhookHandler_PullRequestCarriesHeadRepository(t *testing.T) {
	for _, tc := range []struct {
		name      string
		headOwner string
		foreign   bool
	}{
		{"same repository", "owner", false},
		{"fork", "contributor", true},
		{"head repository deleted", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handler := &recordingEventHandler{}
			h := NewWebhookHandler(testWebhookSecret, handler)

			req := signedWebhookRequest(t, "pull_request", headRepoPullRequestPayload(t, tc.headOwner))
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)

			require.Equal(t, http.StatusOK, w.Code)
			require.NotNil(t, handler.lastEvent)
			require.NotNil(t, handler.lastEvent.PullRequest)

			assert.Equal(t, tc.headOwner, handler.lastEvent.PullRequest.HeadRepo.Owner)
			assert.Equal(t, tc.foreign,
				handler.lastEvent.PullRequest.IsForeign(handler.lastEvent.Repository),
				"whether the pull request is treated as foreign")
		})
	}
}

// headRepoPullRequestPayload builds a pull_request payload whose head
// repository is owned by headOwner. An empty headOwner omits head.repo
// entirely, which is what GitHub sends once a fork has been deleted.
func headRepoPullRequestPayload(t *testing.T, headOwner string) []byte {
	t.Helper()

	head := map[string]any{"sha": "abc123", "ref": "feature"}
	if headOwner != "" {
		head["repo"] = map[string]any{
			"name":  "repo",
			"owner": map[string]string{"login": headOwner},
		}
	}

	payload, err := json.Marshal(map[string]any{
		"action": "opened",
		"number": 7,
		"pull_request": map[string]any{
			"head": head,
			"base": map[string]any{"ref": "main"},
		},
		"repository": map[string]any{
			"name":  "repo",
			"owner": map[string]string{"login": "owner"},
		},
		"installation": map[string]any{"id": 1},
	})
	require.NoError(t, err)
	return payload
}

// An issue_comment event populates Number alone, so its PullRequest can
// never carry a draft flag. That is what makes "only the automatic plan
// observes draft state" a property of the parsing rather than a rule the
// orchestrator has to remember.
func TestWebhookHandler_IssueCommentCarriesNoDraftFlag(t *testing.T) {
	handler := &recordingEventHandler{}
	h := NewWebhookHandler(testWebhookSecret, handler)

	req := signedWebhookRequest(t, "issue_comment", testIssueCommentPayload(t, true))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.NotNil(t, handler.lastEvent.PullRequest)
	assert.False(t, handler.lastEvent.PullRequest.Draft,
		"a comment event carries no draft state to consult")
}

func draftPullRequestPayload(t *testing.T, draft bool) []byte {
	t.Helper()
	event := &gh.PullRequestEvent{
		Action: gh.Ptr("opened"),
		Number: gh.Ptr(42),
		PullRequest: &gh.PullRequest{
			Head:  &gh.PullRequestBranch{SHA: gh.Ptr("abc123"), Ref: gh.Ptr("feature")},
			Base:  &gh.PullRequestBranch{Ref: gh.Ptr("main")},
			Draft: gh.Ptr(draft),
		},
		Repo: &gh.Repository{
			Name:    gh.Ptr("repo"),
			Owner:   &gh.User{Login: gh.Ptr("owner")},
			HTMLURL: gh.Ptr("https://github.com/owner/repo"),
		},
		Installation: &gh.Installation{ID: gh.Ptr(int64(555))},
	}
	payload, err := json.Marshal(event)
	require.NoError(t, err)
	return payload
}

func testIssueCommentPayload(t *testing.T, onPullRequest bool) []byte {
	t.Helper()
	issue := &gh.Issue{Number: gh.Ptr(42)}
	if onPullRequest {
		issue.PullRequestLinks = &gh.PullRequestLinks{URL: gh.Ptr("https://api.github.com/repos/owner/repo/pulls/42")}
	}
	event := &gh.IssueCommentEvent{
		Action: gh.Ptr("created"),
		Issue:  issue,
		Comment: &gh.IssueComment{
			ID:   gh.Ptr(int64(999)),
			Body: gh.Ptr("/turnip plan"),
			User: &gh.User{Login: gh.Ptr("alice")},
		},
		Repo: &gh.Repository{
			Name:    gh.Ptr("repo"),
			Owner:   &gh.User{Login: gh.Ptr("owner")},
			HTMLURL: gh.Ptr("https://github.com/owner/repo"),
		},
		Installation: &gh.Installation{ID: gh.Ptr(int64(555))},
	}
	payload, err := json.Marshal(event)
	require.NoError(t, err)
	return payload
}

func TestWebhookHandler_PullRequestDispatches(t *testing.T) {
	handler := &recordingEventHandler{}
	h := NewWebhookHandler(testWebhookSecret, handler)

	before := scrapeMetric(t, "turnip_webhook_events_total", map[string]string{"event_type": "pull_request", "outcome": "dispatched"})

	req := signedWebhookRequest(t, "pull_request", testPullRequestPayload(t))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	pr, comment := handler.calls()
	assert.Equal(t, 1, pr)
	assert.Equal(t, 0, comment)
	assert.InDelta(t, before+1, scrapeMetric(t, "turnip_webhook_events_total", map[string]string{"event_type": "pull_request", "outcome": "dispatched"}), 0.0001)

	event := handler.lastEvent
	require.NotNil(t, event)
	assert.Equal(t, "pull_request", event.Type)
	assert.Equal(t, "opened", event.Action)
	assert.Equal(t, "owner", event.Repository.Owner)
	assert.Equal(t, "repo", event.Repository.Name)
	require.NotNil(t, event.PullRequest)
	assert.Equal(t, 42, event.PullRequest.Number)
	assert.Equal(t, "abc123", event.PullRequest.HeadSHA)
	assert.EqualValues(t, 555, event.Installation.ID)
}

func TestWebhookHandler_IssueCommentOnPRDispatches(t *testing.T) {
	handler := &recordingEventHandler{}
	h := NewWebhookHandler(testWebhookSecret, handler)

	req := signedWebhookRequest(t, "issue_comment", testIssueCommentPayload(t, true))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	pr, comment := handler.calls()
	assert.Equal(t, 0, pr)
	assert.Equal(t, 1, comment)

	event := handler.lastEvent
	require.NotNil(t, event.Comment)
	assert.Equal(t, "/turnip plan", event.Comment.Body)
	assert.Equal(t, "alice", event.Comment.Author)
	require.NotNil(t, event.PullRequest)
	assert.Equal(t, 42, event.PullRequest.Number)
}

func TestWebhookHandler_IssueCommentOnPlainIssueDoesNotDispatch(t *testing.T) {
	handler := &recordingEventHandler{}
	h := NewWebhookHandler(testWebhookSecret, handler)

	before := scrapeMetric(t, "turnip_webhook_events_total", map[string]string{"event_type": "issue_comment", "outcome": "skipped"})

	req := signedWebhookRequest(t, "issue_comment", testIssueCommentPayload(t, false))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	pr, comment := handler.calls()
	assert.Equal(t, 0, pr)
	assert.Equal(t, 0, comment)
	assert.InDelta(t, before+1, scrapeMetric(t, "turnip_webhook_events_total", map[string]string{"event_type": "issue_comment", "outcome": "skipped"}), 0.0001)
}

// A refusal is not a failure. Answering 500 would make GitHub retry a
// delivery turnip declined on purpose, and every retry would reach the
// same decision.
func TestWebhookHandler_RefusedDeliveryReturns200(t *testing.T) {
	handler := &recordingEventHandler{returnErr: ErrRefused}
	h := NewWebhookHandler(testWebhookSecret, handler)

	req := signedWebhookRequest(t, "pull_request", testPullRequestPayload(t))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

// One delivery, one counted outcome. Recording a refusal at its own site
// while the dispatch path also counted it would make every ratio built on
// this counter wrong — quietly, which is the worst way for a metric to be
// wrong.
func TestWebhookHandler_RefusedDeliveryCountedOnceAsRejected(t *testing.T) {
	rejectedLabels := map[string]string{"event_type": "pull_request", "outcome": "rejected"}
	dispatchedLabels := map[string]string{"event_type": "pull_request", "outcome": "dispatched"}

	beforeRejected := scrapeMetric(t, "turnip_webhook_events_total", rejectedLabels)
	beforeDispatched := scrapeMetric(t, "turnip_webhook_events_total", dispatchedLabels)

	handler := &recordingEventHandler{returnErr: ErrRefused}
	h := NewWebhookHandler(testWebhookSecret, handler)

	req := signedWebhookRequest(t, "pull_request", testPullRequestPayload(t))
	h.ServeHTTP(httptest.NewRecorder(), req)

	assert.InDelta(t, beforeRejected+1,
		scrapeMetric(t, "turnip_webhook_events_total", rejectedLabels), 0.0001,
		"a refused delivery is counted once as rejected")
	assert.InDelta(t, beforeDispatched,
		scrapeMetric(t, "turnip_webhook_events_total", dispatchedLabels), 0.0001,
		"and must not also be counted as dispatched")
}

// An ordinary handler error still answers 500 and still counts as
// dispatched: the refusal branch must not swallow real failures, which is
// the regression a reader "simplifying" the two branches together would
// introduce.
func TestWebhookHandler_OrdinaryErrorStillReturns500(t *testing.T) {
	handler := &recordingEventHandler{returnErr: errBoom}
	h := NewWebhookHandler(testWebhookSecret, handler)

	req := signedWebhookRequest(t, "pull_request", testPullRequestPayload(t))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestWebhookHandler_InvalidSignatureRejected(t *testing.T) {
	handler := &recordingEventHandler{}
	h := NewWebhookHandler(testWebhookSecret, handler)

	before := scrapeMetric(t, "turnip_webhook_events_total", map[string]string{"event_type": "unknown", "outcome": "rejected"})

	payload := testPullRequestPayload(t)
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(string(payload)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(gh.EventTypeHeader, "pull_request")
	req.Header.Set(gh.SHA256SignatureHeader, "sha256=0000000000000000000000000000000000000000000000000000000000000000")

	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	require.Equal(t, http.StatusUnauthorized, w.Code)
	pr, comment := handler.calls()
	assert.Equal(t, 0, pr)
	assert.Equal(t, 0, comment)
	assert.InDelta(t, before+1, scrapeMetric(t, "turnip_webhook_events_total", map[string]string{"event_type": "unknown", "outcome": "rejected"}), 0.0001)
}

func TestWebhookHandler_UnrelatedEventTypeIgnored(t *testing.T) {
	handler := &recordingEventHandler{}
	h := NewWebhookHandler(testWebhookSecret, handler)

	before := scrapeMetric(t, "turnip_webhook_events_total", map[string]string{"event_type": "ping", "outcome": "skipped"})

	req := signedWebhookRequest(t, "ping", []byte(`{"zen":"hello"}`))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	pr, comment := handler.calls()
	assert.Equal(t, 0, pr)
	assert.Equal(t, 0, comment)
	assert.InDelta(t, before+1, scrapeMetric(t, "turnip_webhook_events_total", map[string]string{"event_type": "ping", "outcome": "skipped"}), 0.0001)
}

func TestWebhookHandler_HandlerErrorReturns500(t *testing.T) {
	handler := &recordingEventHandler{returnErr: errBoom}
	h := NewWebhookHandler(testWebhookSecret, handler)

	req := signedWebhookRequest(t, "pull_request", testPullRequestPayload(t))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func statePullRequestPayload(t *testing.T, state string, merged bool) []byte {
	t.Helper()

	pr := map[string]any{
		"head":   map[string]any{"sha": "abc123", "ref": "feature", "repo": map[string]any{"name": "repo", "owner": map[string]string{"login": "owner"}}},
		"base":   map[string]any{"ref": "main"},
		"merged": merged,
	}
	if state != "" {
		pr["state"] = state
	}

	payload, err := json.Marshal(map[string]any{
		"action":       "opened",
		"number":       7,
		"pull_request": pr,
		"repository":   map[string]any{"name": "repo", "owner": map[string]string{"login": "owner"}},
		"installation": map[string]any{"id": 1},
	})
	require.NoError(t, err)
	return payload
}

// A merged pull request reports state "closed", so treating merged and
// closed alike needs no rule of turnip's own — and an absent state maps
// to not-open, which is the direction that refuses rather than the one
// that silently restores the bug.
func TestWebhookHandler_PullRequestCarriesOpenState(t *testing.T) {
	for _, tc := range []struct {
		name   string
		state  string
		merged bool
		want   bool
	}{
		{"open", "open", false, true},
		{"closed without merging", "closed", false, false},
		{"merged", "closed", true, false},
		{"state absent from the payload", "", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handler := &recordingEventHandler{}
			h := NewWebhookHandler(testWebhookSecret, handler)

			req := signedWebhookRequest(t, "pull_request", statePullRequestPayload(t, tc.state, tc.merged))
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)

			require.Equal(t, http.StatusOK, w.Code)
			require.NotNil(t, handler.lastEvent)
			require.NotNil(t, handler.lastEvent.PullRequest)

			assert.Equal(t, tc.want, handler.lastEvent.PullRequest.Open)
		})
	}
}
