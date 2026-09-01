package github

import (
	"net/http"
	"net/http/httptest"
	"testing"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/ivanvc/turnip/internal/metrics"
)

// scrapeMetric reads name's current value for the given label set off
// metrics.Handler()'s live exposition — the package-level Prometheus
// collectors in internal/metrics are deliberately unexported (design.md's
// narrow-seam preference), so cross-package tests observe them the same
// way a real Prometheus server would: by scraping /metrics. Returns 0 if
// the metric or label combination hasn't been observed yet.
func scrapeMetric(t *testing.T, name string, labels map[string]string) float64 {
	t.Helper()
	rec := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	parser := expfmt.NewTextParser(model.UTF8Validation)
	mfs, err := parser.TextToMetricFamilies(rec.Body)
	require.NoError(t, err)

	mf, ok := mfs[name]
	if !ok {
		return 0
	}
	for _, m := range mf.GetMetric() {
		if labelsMatch(m.GetLabel(), labels) {
			if m.Counter != nil {
				return m.GetCounter().GetValue()
			}
		}
	}
	return 0
}

func labelsMatch(pairs []*dto.LabelPair, want map[string]string) bool {
	if len(pairs) != len(want) {
		return false
	}
	for _, p := range pairs {
		if want[p.GetName()] != p.GetValue() {
			return false
		}
	}
	return true
}
