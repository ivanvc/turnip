package github

import (
	"bufio"
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	gh "github.com/google/go-github/v90/github"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captureLogs points the process-wide slog default at a buffer for the
// rest of the test, restoring the previous default afterwards, and returns
// a function that decodes every record written so far.
//
// Safe only because no test in this package runs in parallel: the default
// logger is global.
func captureLogs(t *testing.T) func() []map[string]any {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	return func() []map[string]any {
		var records []map[string]any
		sc := bufio.NewScanner(bytes.NewReader(buf.Bytes()))
		for sc.Scan() {
			var rec map[string]any
			require.NoError(t, json.Unmarshal(sc.Bytes(), &rec))
			records = append(records, rec)
		}
		return records
	}
}

func findRecord(records []map[string]any, msg string) map[string]any {
	for _, r := range records {
		if r["msg"] == msg {
			return r
		}
	}
	return nil
}

// A 500 used to leave only a metric behind: the orchestrator returns its
// error rather than logging it, and the handler answered without a word.
func TestWebhookHandler_HandlerErrorIsLogged(t *testing.T) {
	logs := captureLogs(t)
	h := NewWebhookHandler(testWebhookSecret, &recordingEventHandler{returnErr: errBoom})

	req := signedWebhookRequest(t, "pull_request", testPullRequestPayload(t))
	req.Header.Set(gh.DeliveryIDHeader, "delivery-500")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	require.Equal(t, http.StatusInternalServerError, w.Code)

	rec := findRecord(logs(), "handling webhook delivery failed")
	require.NotNil(t, rec, "a 500 must be logged")
	assert.Equal(t, "ERROR", rec["level"])
	assert.Equal(t, "delivery-500", rec["delivery_id"])
	assert.Equal(t, "pull_request", rec["event"])
	assert.Equal(t, errBoom.Error(), rec["error"])
}

func TestWebhookHandler_DispatchIsLoggedAtInfo(t *testing.T) {
	logs := captureLogs(t)
	h := NewWebhookHandler(testWebhookSecret, &recordingEventHandler{})

	req := signedWebhookRequest(t, "pull_request", testPullRequestPayload(t))
	req.Header.Set(gh.DeliveryIDHeader, "delivery-ok")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	records := logs()
	rec := findRecord(records, "handling webhook delivery")
	require.NotNil(t, rec)
	assert.Equal(t, "INFO", rec["level"])
	assert.Equal(t, "delivery-ok", rec["delivery_id"])
	assert.Contains(t, rec, "pr_number")
	assert.Nil(t, findRecord(records, "handling webhook delivery failed"))
}

func TestWebhookHandler_InvalidSignatureIsLogged(t *testing.T) {
	logs := captureLogs(t)
	h := NewWebhookHandler(testWebhookSecret, &recordingEventHandler{})

	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(string(testPullRequestPayload(t))))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(gh.EventTypeHeader, "pull_request")
	req.Header.Set(gh.DeliveryIDHeader, "delivery-401")
	req.Header.Set(gh.SHA256SignatureHeader, "sha256=0000000000000000000000000000000000000000000000000000000000000000")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)

	rec := findRecord(logs(), "rejecting webhook delivery: signature verification failed")
	require.NotNil(t, rec, "a 401 must be logged")
	assert.Equal(t, "WARN", rec["level"])
	assert.Equal(t, "delivery-401", rec["delivery_id"])
}

func TestWebhookHandler_SkippedEventIsLoggedAtDebugOnly(t *testing.T) {
	logs := captureLogs(t)
	h := NewWebhookHandler(testWebhookSecret, &recordingEventHandler{})

	w := httptest.NewRecorder()
	h.ServeHTTP(w, signedWebhookRequest(t, "ping", []byte(`{"zen":"hello"}`)))
	require.Equal(t, http.StatusOK, w.Code)

	rec := findRecord(logs(), "skipping webhook delivery")
	require.NotNil(t, rec)
	assert.Equal(t, "DEBUG", rec["level"])
}
