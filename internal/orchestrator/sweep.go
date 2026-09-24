package orchestrator

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/ivanvc/turnip/internal/github"
	"github.com/ivanvc/turnip/internal/jobs"
	"github.com/ivanvc/turnip/internal/lock"
)

// Run starts the periodic timeout sweep (Requirement 8.3) and blocks
// until ctx is canceled.
func (o *Orchestrator) Run(ctx context.Context) error {
	ticker := time.NewTicker(o.sweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			o.sweepOnce(ctx)
		}
	}
}

// sweepOnce finds Operation Records whose start deadline has passed and
// which never started, claims each atomically, diagnoses it against the
// Kubernetes API, and reports it as a timeout failure (Requirement 8).
func (o *Orchestrator) sweepOnce(ctx context.Context) {
	keys, err := o.records.ScanOperationKeys(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "sweep scan", "error", err)
		return
	}

	now := time.Now()
	for _, key := range keys {
		operationID := strings.TrimPrefix(key, "operation:")
		rec, claimed, err := o.records.ClaimForTimeout(ctx, operationID, now)
		if err != nil {
			slog.ErrorContext(ctx, "sweep claim", "operation_id", operationID, "error", err)
			continue
		}
		if !claimed {
			continue
		}
		o.reportTimeout(ctx, operationID, rec)
	}
}

func (o *Orchestrator) reportTimeout(ctx context.Context, operationID string, rec *OperationRecord) {
	slog.WarnContext(ctx, "operation timed out before its runner reported",
		"operation_id", operationID,
		"job", rec.JobName,
		"lock_key", rec.ProjectKey,
		"pr_number", rec.PRNumber,
		"operation", rec.Operation,
	)
	var status *jobs.JobStatus
	if rec.JobName != "" {
		s, err := o.jobs.Status(ctx, rec.JobName)
		if err != nil {
			slog.ErrorContext(ctx, "diagnosing timeout", "operation_id", operationID, "error", err)
		} else {
			status = s
		}
	}

	diagnostic := timeoutDiagnostic(rec.JobName, status)
	pr := github.ProjectResult{
		ProjectName: rec.Project.Name,
		Tool:        rec.Project.Tool,
		Operation:   rec.Operation,
		Success:     false,
		Output:      diagnostic,
		// A timed-out Operation leaves its Lock held, and until this slice
		// nothing here said so: Locked was never set, so it defaulted to
		// false and the Project vanished from the comment's footer and was
		// never offered unlock. The Lock was held and invisible at once.
		Locked: true,
	}

	// A timed-out plan changes nothing; a timed-out mutating Operation
	// invalidates the stored plan, because the Runner may still be
	// executing and infrastructure may already have moved.
	ev := lock.EventMutatingTimedOut
	if p, ok := o.plugins[rec.Project.Tool]; ok && rec.Operation == p.GetPlanOperation() {
		ev = lock.EventPlanTimedOut
	}
	switch tr, err := o.locks.Apply(ctx, rec.ProjectKey, rec.PRNumber, ev, nil); {
	case err != nil:
		slog.ErrorContext(ctx, "applying lock event for timed-out operation", "operation_id", operationID, "event", string(ev), "error", err)
	case tr.Released:
		pr.Locked = false
		pr.LockNote = lockNoteFor(ev, tr)
	case tr.From == "":
		pr.Locked = false
	default:
		pr.LockNote = lockNoteFor(ev, tr)
	}

	client := o.installationClient(rec.InstallationID)
	if rec.CheckRunID != 0 {
		err := client.UpdateCheckRun(ctx, rec.Owner, rec.Repo, rec.CheckRunID, github.CheckRunOptions{
			Name:       checkRunName(rec.Project.Name, rec.Operation),
			Status:     "completed",
			Conclusion: "failure",
			Title:      timeoutTitle(diagnostic),
			Summary:    "The Runner never reported back within the start timeout.",
			Text:       pr.Output,
		})
		if err != nil {
			slog.ErrorContext(ctx, "updating check run for timed-out operation", "operation_id", operationID, "error", err)
			pr = appendCheckRunNote(pr, err)
		}
	}

	// The Lock is left held by the transition applied above: the governing
	// requirement is 7 (Redis/Valkey-Based Locking), not the "6.8/8.5"
	// this comment once cited — Requirement 6 is Plan with Destroy Flag
	// and has five criteria, none about Lock lifecycle.
	//
	// No jobs.Client.Delete call either (Requirement 8.6): a Runner that
	// connects after this point may still be running real tool work — the
	// Job's TTL is this path's cleanup mechanism.

	pr = o.recordFinishedOutcome(ctx, client, rec, ev, pr)

	if err := o.records.Delete(ctx, operationID); err != nil {
		slog.ErrorContext(ctx, "deleting timed-out operation record", "operation_id", operationID, "error", err)
	}
	if err := publishDone(ctx, o.redis, operationID, pr); err != nil {
		slog.ErrorContext(ctx, "publishing timeout notification", "operation_id", operationID, "error", err)
	}
}

// timeoutDiagnostic renders a JobStatus into readable text (Requirement
// 8.4), rather than a bare "timed out" message.
func timeoutDiagnostic(jobName string, status *jobs.JobStatus) string {
	if status == nil || !status.JobFound {
		return "No Job/Pod found for this Operation; it may have been deleted or never successfully scheduled"
	}
	if status.PodReason != "" {
		return fmt.Sprintf("Job %s: container stuck (%s)", jobName, status.PodReason)
	}
	if status.PodPhase != "" {
		return fmt.Sprintf("Job %s: Pod still %s after 5 minutes", jobName, status.PodPhase)
	}
	return fmt.Sprintf("Job %s: no Pod found yet after 5 minutes", jobName)
}
