package metrics

import (
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	webhookEventsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "turnip_webhook_events_total",
		Help: "Total number of GitHub webhook events received, by event type and outcome.",
	}, []string{"event_type", "outcome"})

	operationsDispatchedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "turnip_operations_dispatched_total",
		Help: "Total number of Operations dispatched, by Project tool, Operation, and outcome.",
	}, []string{"tool", "operation", "outcome"})

	lockAttemptsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "turnip_lock_attempts_total",
		Help: "Total number of lock acquisition attempts, by outcome.",
	}, []string{"outcome"})

	operationDurationSeconds = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name: "turnip_operation_duration_seconds",
		Help: "Webhook-to-Operation-result latency, by Project tool and Operation.",
	}, []string{"tool", "operation"})

	runnerJobStartLatencySeconds = promauto.NewHistogram(prometheus.HistogramOpts{
		Name: "turnip_runner_job_start_latency_seconds",
		Help: "Time between a Runner Job's creation and the Server first hearing from its Runner.",
	})
)

// Handler returns an http.Handler serving the default Prometheus
// registry in text-exposition format.
func Handler() http.Handler {
	return promhttp.Handler()
}

// WebhookEvent records a received GitHub webhook event of eventType,
// with outcome one of "dispatched", "skipped", or "rejected".
func WebhookEvent(eventType, outcome string) {
	webhookEventsTotal.WithLabelValues(eventType, outcome).Inc()
}

// OperationDispatched records a dispatched Operation for tool/operation,
// with outcome one of "success", "failure", or "rejected".
func OperationDispatched(tool, operation, outcome string) {
	operationsDispatchedTotal.WithLabelValues(tool, operation, outcome).Inc()
}

// LockAttempt records a lock acquisition attempt, with outcome one of
// "acquired" or "rejected".
func LockAttempt(outcome string) {
	lockAttemptsTotal.WithLabelValues(outcome).Inc()
}

// ObserveOperationDuration records the webhook-to-Operation-result
// latency d for tool/operation.
func ObserveOperationDuration(tool, operation string, d time.Duration) {
	operationDurationSeconds.WithLabelValues(tool, operation).Observe(d.Seconds())
}

// ObserveJobStartLatency records the Runner Job Start Latency d.
func ObserveJobStartLatency(d time.Duration) {
	runnerJobStartLatencySeconds.Observe(d.Seconds())
}
