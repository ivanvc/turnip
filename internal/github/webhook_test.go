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
