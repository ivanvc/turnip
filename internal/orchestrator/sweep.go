package orchestrator

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"

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

// abandonedOutput is the line a Project carries in its trigger's comment
// when its Operation started and nothing came back within abandonAfter.
const abandonedOutput = "No result was received from the Runner within 24 hours."

// The summaries of a check the sweep completes: an Operation whose Runner
// never started, and one whose Runner started and ended without a result.
const (
	timeoutSummary = "The Runner never reported back within the start timeout."
	stoppedSummary = "The Runner stopped before reporting a result."
)

// noJobDiagnostic is the diagnostic for an Operation whose Job or Pod is
// gone, or whose status could not be read.
const noJobDiagnostic = "No Job/Pod found for this operation; it may have been deleted or never successfully scheduled"

// sweepOnce finds Operation Records whose start deadline has passed and
// which never started, claims each atomically, diagnoses it against the
// Kubernetes API, and reports it as a timeout failure (Requirement 8).
// A started record the timeout claim passes over has its Job read: once
// the Job ended or is gone, the Runner never will report, and the sweep
// claims it and finalizes it as stopped (reportStopped). Otherwise it
// tries the abandon claim, which only succeeds for a started record at
// least abandonAfter old.
//
// The scan and the claims stop with ctx. What follows a claim does not: a
// claimed Operation is finalized by nobody else, so a stop that lands in
// the middle of a tick must not skip its check and its Lock and still
// carry its trigger forward. Run returns only after the tick does, which
// is what a stop waits for.
func (o *Orchestrator) sweepOnce(ctx context.Context) {
	claimedCtx := context.WithoutCancel(ctx)
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
		if claimed {
			o.reportTimeout(claimedCtx, operationID, rec)
			continue
		}
		if status := o.endedRunner(ctx, operationID); status != nil {
			rec, claimed, err = o.records.ClaimForStopped(ctx, operationID)
			if err != nil {
				slog.ErrorContext(ctx, "sweep stopped claim", "operation_id", operationID, "error", err)
				continue
			}
			if claimed {
				o.reportStopped(claimedCtx, operationID, rec, status)
			}
			// Not claimed: a result, or another replica, got there first.
			continue
		}
		rec, claimed, err = o.records.ClaimForAbandon(ctx, operationID, now)
		if err != nil {
			slog.ErrorContext(ctx, "sweep abandon claim", "operation_id", operationID, "error", err)
			continue
		}
		if claimed {
			o.reportAbandoned(claimedCtx, operationID, rec)
		}
	}
}

// reportAbandoned gives up on an Operation that started and never
// reported. Its check stays in progress and its Lock stays held, as they
// were: the Runner may still be executing, and nothing here knows what it
// changed. Only the record goes, and the Project is completed in its
// trigger so the comment is not held back any longer.
func (o *Orchestrator) reportAbandoned(ctx context.Context, operationID string, rec *OperationRecord) {
	slog.WarnContext(ctx, "operation abandoned: no result within the abandonment window",
		"operation_id", operationID,
		"job", rec.JobName,
		"lock_key", rec.ProjectKey,
		"pr_number", rec.PRNumber,
		"operation", rec.Operation,
	)
	if err := o.records.Delete(ctx, operationID); err != nil {
		slog.ErrorContext(ctx, "deleting abandoned operation record", "operation_id", operationID, "error", err)
	}
	o.finishOperation(ctx, rec, github.ProjectResult{
		ProjectName: rec.Project.Name,
		Tool:        rec.Project.Tool,
		Operation:   rec.Operation,
		Success:     false,
		Output:      abandonedOutput,
		Locked:      true,
	})
}

// endedRunner reads the Job of operationID's record when the record is
// started and unfinalized, and returns its status when the Job ended (a
// Failed or Complete condition, with its Pod no longer Pending or
// Running) or its Job or Pod is gone: the Runner never will report. A
// Job that failed while its Pod is still terminating is left for a later
// tick, since the Runner may be inside its grace period, about to report. It returns nil for every other record, for a Job
// still running, and when the status cannot be read, which the next tick
// reads again rather than taking a Runner that may still be running for
// one that ended. A record with no Job name is left to abandonment, for
// the same reason. The returned status reports JobFound false when the
// Job is gone.
func (o *Orchestrator) endedRunner(ctx context.Context, operationID string) *jobs.JobStatus {
	rec, err := o.records.get(ctx, operationID)
	if err != nil {
		slog.ErrorContext(ctx, "sweep reading record", "operation_id", operationID, "error", err)
		return nil
	}
	if rec == nil || !rec.Started || rec.Finalized || rec.JobName == "" {
		return nil
	}
	status, err := o.jobs.Status(ctx, rec.JobName)
	if err != nil {
		slog.ErrorContext(ctx, "reading the job of a started operation", "operation_id", operationID, "job", rec.JobName, "error", err)
		return nil
	}
	if status == nil {
		return nil
	}
	if !status.JobFound || status.PodPhase == "" {
		return status
	}
	if status.Finished && status.PodPhase != corev1.PodPending && status.PodPhase != corev1.PodRunning {
		return status
	}
	return nil
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

	ev := sweepEvent(o, rec)
	diagnostic := timeoutDiagnostic(rec.JobName, status, ev == lock.EventMutatingTimedOut)
	o.finalizeSwept(ctx, operationID, rec, ev, diagnostic, diagnostic+o.repositorySecretHint(rec, status), timeoutSummary)
}

// reportStopped finalizes a started Operation whose Runner ended without
// reporting a result, through the start timeout's path: the check fails
// titled with a diagnostic read from the Job, the Lock takes the plan or
// mutating timeout event, the outcome is recorded and the trigger gets the
// diagnostic as the Project's output.
func (o *Orchestrator) reportStopped(ctx context.Context, operationID string, rec *OperationRecord, status *jobs.JobStatus) {
	slog.WarnContext(ctx, "operation's runner stopped before reporting",
		"operation_id", operationID,
		"job", rec.JobName,
		"lock_key", rec.ProjectKey,
		"pr_number", rec.PRNumber,
		"operation", rec.Operation,
	)
	ev := sweepEvent(o, rec)
	diagnostic := stoppedDiagnostic(rec.JobName, status, o.runnerDeadline)
	o.finalizeSwept(ctx, operationID, rec, ev, diagnostic, diagnostic, stoppedSummary)
}

// sweepEvent is the Lock event for an Operation the sweep finalizes
// without a result. A plan changes nothing; a mutating Operation
// invalidates the stored plan, because the Runner may have executed part
// of it and infrastructure may already have moved.
func sweepEvent(o *Orchestrator, rec *OperationRecord) lock.Event {
	if p, ok := o.plugins[rec.Project.Tool]; ok && rec.Operation == p.GetPlanOperation() {
		return lock.EventPlanTimedOut
	}
	return lock.EventMutatingTimedOut
}

// finalizeSwept finalizes an Operation the sweep claimed without a
// result: it applies ev to the Lock, completes the check as a failure
// titled diagnostic with summary, records the outcome, deletes the record
// and adds output to the trigger as the Project's.
func (o *Orchestrator) finalizeSwept(ctx context.Context, operationID string, rec *OperationRecord, ev lock.Event, diagnostic, output, summary string) {
	pr := github.ProjectResult{
		ProjectName: rec.Project.Name,
		Tool:        rec.Project.Tool,
		Operation:   rec.Operation,
		Success:     false,
		Output:      output,
		// A timed-out Operation leaves its Lock held, and until this slice
		// nothing here said so: Locked was never set, so it defaulted to
		// false and the Project vanished from the comment's footer and was
		// never offered unlock. The Lock was held and invisible at once.
		Locked: true,
	}

	switch tr, err := o.locks.Apply(ctx, rec.ProjectKey, rec.PRNumber, ev, nil); {
	case err != nil:
		slog.ErrorContext(ctx, "applying lock event for swept operation", "operation_id", operationID, "event", string(ev), "error", err)
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
			Summary:    summary,
		})
		if err != nil {
			slog.ErrorContext(ctx, "updating check run for swept operation", "operation_id", operationID, "error", err)
			pr = appendCheckRunNote(pr, checkComplete, err)
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
		slog.ErrorContext(ctx, "deleting swept operation record", "operation_id", operationID, "error", err)
	}
	o.finishOperation(ctx, rec, pr)
}

// stoppedDiagnostic renders the status of a started Runner's Job that
// ended without a result, the first row that applies. deadline is the
// Server's configured Job deadline, named when the Job's own could not be
// read.
//
// A Job found failed at its deadline is named so even when its Pod is
// gone: the Job controller deletes the Pod of a Job past its deadline,
// and sets the Failed condition only once it has terminated, so by the
// time the sweep can read the condition the Pod is usually gone.
func stoppedDiagnostic(jobName string, status *jobs.JobStatus, deadline time.Duration) string {
	switch {
	case status == nil || !status.JobFound:
		return noJobDiagnostic
	case status.FailedReason == "DeadlineExceeded":
		if status.Deadline > 0 {
			deadline = status.Deadline
		}
		return fmt.Sprintf("Job %s: stopped at its deadline of %s", jobName, deadline)
	case status.PodPhase == "":
		return noJobDiagnostic
	case status.TerminatedReason != "":
		return fmt.Sprintf("Job %s: container %s terminated (%s)", jobName, status.TerminatedContainer, status.TerminatedReason)
	case status.Evicted:
		return fmt.Sprintf("Job %s: Pod evicted", jobName)
	default:
		return fmt.Sprintf("Job %s: finished without reporting a result", jobName)
	}
}

// timeoutDiagnostic renders a JobStatus into readable text (Requirement
// 8.4), rather than a bare "timed out" message. mutating reports whether
// the Operation was a Mutating_Operation, which runs the image digest its
// plan recorded: a tool image it cannot pull most likely means that digest
// is gone from the registry, and only a re-plan records one that exists.
// A pull failure on a container running turnip's own Runner image is not
// the recorded digest's doing, and gets the generic diagnostic.
func timeoutDiagnostic(jobName string, status *jobs.JobStatus, mutating bool) string {
	if status == nil || !status.JobFound {
		return noJobDiagnostic
	}
	if mutating && status.ToolWaiting && isImagePullFailure(status.PodReason) {
		return fmt.Sprintf("Job %s: the image could not be pulled (%s). The image digest the plan recorded may no longer exist in the registry; re-plan", jobName, status.PodReason)
	}
	if status.PodReason != "" {
		return fmt.Sprintf("Job %s: container stuck (%s)", jobName, status.PodReason)
	}
	if status.PodPhase != "" {
		return fmt.Sprintf("Job %s: Pod still %s after 5 minutes", jobName, status.PodPhase)
	}
	return fmt.Sprintf("Job %s: no Pod found yet after 5 minutes", jobName)
}

// repositorySecretHint is the line the results comment adds under a
// start-timeout diagnostic when the Project lists keys of the repository's
// Secret and its Pod is still Pending: a missing Secret or key keeps the
// Pod there, and nothing else the Server can read says so. It is worded
// as a condition because a Pod can stay Pending for other reasons too.
// Empty, so the output is the diagnostic alone, in every other case. The
// check's title is built from the diagnostic without it.
func (o *Orchestrator) repositorySecretHint(rec *OperationRecord, status *jobs.JobStatus) string {
	keys := rec.Project.Runner.Secrets
	if len(keys) == 0 || status == nil || !status.JobFound || status.PodPhase != corev1.PodPending {
		return ""
	}
	return fmt.Sprintf(
		"\nThis project mounts keys %s of Secret %s in namespace %s; if that Secret or one of those keys is missing, the Pod cannot start.",
		strings.Join(keys, ", "), jobs.RepositorySecretName(rec.RepositoryID), o.runnerNamespace,
	)
}

// isImagePullFailure reports whether a container's waiting reason means
// its image could not be pulled.
func isImagePullFailure(reason string) bool {
	return reason == "ErrImagePull" || reason == "ImagePullBackOff"
}
