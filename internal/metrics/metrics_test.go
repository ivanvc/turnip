package metrics

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// histogramSampleCount reads a Histogram's current observation count by
// writing its protobuf representation — the same mechanism
// testutil.ToFloat64 uses internally for single-value collectors, which
// doesn't apply to histograms (they expose no single float value).
func histogramSampleCount(t *testing.T, c any) uint64 {
	t.Helper()
	writer, ok := c.(interface{ Write(*dto.Metric) error })
	require.True(t, ok, "%T does not implement Write(*dto.Metric) error", c)
	var m dto.Metric
	require.NoError(t, writer.Write(&m))
	return m.GetHistogram().GetSampleCount()
}

func TestWebhookEvent(t *testing.T) {
	before := testutil.ToFloat64(webhookEventsTotal.WithLabelValues("pull_request", "dispatched"))

	WebhookEvent("pull_request", "dispatched")

	assert.InDelta(t, before+1, testutil.ToFloat64(webhookEventsTotal.WithLabelValues("pull_request", "dispatched")), 0.0001)
}

func TestOperationDispatched(t *testing.T) {
	before := testutil.ToFloat64(operationsDispatchedTotal.WithLabelValues("helmfile", "diff", "success"))

	OperationDispatched("helmfile", "diff", "success")

	assert.InDelta(t, before+1, testutil.ToFloat64(operationsDispatchedTotal.WithLabelValues("helmfile", "diff", "success")), 0.0001)
}

func TestLockAttempt(t *testing.T) {
	before := testutil.ToFloat64(lockAttemptsTotal.WithLabelValues("acquired"))

	LockAttempt("acquired")

	assert.InDelta(t, before+1, testutil.ToFloat64(lockAttemptsTotal.WithLabelValues("acquired")), 0.0001)
}

func TestObserveOperationDuration(t *testing.T) {
	metric := operationDurationSeconds.WithLabelValues("helmfile", "apply")
	before := histogramSampleCount(t, metric)

	ObserveOperationDuration("helmfile", "apply", 2*time.Second)

	assert.Equal(t, before+1, histogramSampleCount(t, metric))
}

func TestObserveJobStartLatency(t *testing.T) {
	before := histogramSampleCount(t, runnerJobStartLatencySeconds)

	ObserveJobStartLatency(500 * time.Millisecond)

	assert.Equal(t, before+1, histogramSampleCount(t, runnerJobStartLatencySeconds))
}

func TestHandler(t *testing.T) {
	WebhookEvent("pull_request", "dispatched")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)

	Handler().ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "text/plain")

	body, err := io.ReadAll(rec.Body)
	require.NoError(t, err)
	assert.Contains(t, string(body), "turnip_webhook_events_total")
}
