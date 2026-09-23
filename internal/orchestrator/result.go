package orchestrator

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/ivanvc/turnip/internal/github"
	"github.com/ivanvc/turnip/internal/metrics"
	"github.com/ivanvc/turnip/internal/plugin"
	"github.com/ivanvc/turnip/internal/rpc"
)

var _ rpc.OperationHandler = (*Orchestrator)(nil)

// HandleLog records that operationID has produced output (Requirement
// 7.7/8.2), and observes Runner Job Start Latency the one time this call
// is the one that actually performs the started transition (Decision 2
// in metrics' design.md).
//
// The line argument is deliberately unread. The stream itself is
// load-bearing — the first line arriving is what marks the Operation
// started, which drives the latency metric above and keeps the sweep from
// timing it out — while its payload has no consumer yet. That consumer is
// Slice 26, a real-time output view; the Runner streams every line now so
// the Server has something to accumulate when it exists.
//
// A dead-code sweep will flag this parameter, rpc.LogLine's three fields,
// and the Runner's 256 KiB ring buffer together. Deleting any of them
// forecloses that feature, which is why this note is here rather than the
// obvious tidy-up.
func (o *Orchestrator) HandleLog(ctx context.Context, operationID string, line rpc.LogLine) error {
	justStarted, createdAt, err := o.records.MarkStarted(ctx, operationID)
	if err != nil {
		return err
	}
	if justStarted {
		metrics.ObserveJobStartLatency(time.Since(createdAt))
	}
	return nil
}

// HandleResult finalizes an Operation: Lock (Requirement 7),
// check-run (Requirement 9.2/9.3), and comment (via the published
// ProjectResult, Requirement 10) actions.
func (o *Orchestrator) HandleResult(ctx context.Context, operationID string, result rpc.OperationResult) error {
	rec, claimed, err := o.records.ClaimForResult(ctx, operationID)
	if err != nil {
		return err
	}
	if !claimed {
		// Already finalized (by a sweep timeout claim) or already deleted
		// — a retried final-result delivery is a no-op (Requirement 7.9).
		return nil
	}

	pr := github.ProjectResult{
		ProjectName: rec.Project.Name,
		Tool:        rec.Project.Tool,
		Operation:   rec.Operation,
		Success:     result.Success,
		Output:      result.Output,
		Changes: github.ChangeCounts{
			Add:     int(result.Changes.Add),
			Change:  int(result.Changes.Change),
			Destroy: int(result.Changes.Destroy),
		},
	}
	// The comment prints per-Project commands, and operation names are
	// per-tool ("diff"/"apply" for Helmfile, "preview"/"up" for Pulumi).
	// internal/github has no Plugin registry to ask, so they are resolved
	// here and carried. An unregistered tool leaves them empty, which
	// omits those commands rather than guessing at a name.
	if p, ok := o.plugins[rec.Project.Tool]; ok {
		pr.PlanOperation = p.GetPlanOperation()
		pr.ApplyOperation = p.GetApplyOperation()
	}
	if !result.Success && result.ErrorMessage != "" {
		pr.Output = result.Output + "\n\n" + result.ErrorMessage
	}

	client := o.installationClient(rec.InstallationID)

	if rec.CheckRunID != 0 {
		opts := github.CheckRunOptions{
			Name:   checkRunName(rec.Project.Name, rec.Operation),
			Status: "completed",
			// The name is the check run's stable identity (required
			// status checks and branch protection match on it), so the
			// outcome goes in the title and summary — which is what the
			// checks tab renders under the name.
			Title:   checkRunResultTitle(result.Success),
			Summary: checkRunResultSummary(result.Success, changeSummaryText(result.Changes)),
			Text:    result.Output,
		}
		if result.Success {
			opts.Conclusion = "success"
		} else {
			opts.Conclusion = "failure"
		}
		if err := client.UpdateCheckRun(ctx, rec.Owner, rec.Repo, rec.CheckRunID, opts); err != nil {
			slog.ErrorContext(ctx, "updating check run", "operation_id", operationID, "error", err)
			// Surfaced in the comment, not just logged: this result is
			// what reaches the PR, and a check run stuck at "in progress"
			// with no explanation is worse than a noted failure
			// (Requirement 9.5).
			pr = appendCheckRunNote(pr, err)
		}
	}

	// Locked records whether this pull request still holds the Project's
	// Lock once this result has been handled, so the comment can say so
	// and offer the command that releases it. LockNote records why it
	// moved, when it moved.
	//
	// Both are set from the transition the Lock manager confirms, never
	// alongside the decision to attempt one. Announcing before Redis
	// agrees would tell an author a Project is free while it is still
	// held — and the next pull request's plan would then be rejected,
	// naming a pull request whose own comment claims it released.
	//
	// The governing requirement is 7 (Redis/Valkey-Based Locking). An
	// earlier comment here cited "Requirement 6.6-6.8", which describes no
	// Lock lifecycle at all: Requirement 6 is Plan with Destroy Flag and
	// has five criteria. The wrong citation is what made this behaviour
	// look specified when nothing specified it.
	// The scope this Operation ran with, for the summary line. Taken from
	// the record the Job was built from, which is what actually reached
	// the tool, rather than re-derived from the trigger line.
	pr.ScopeArgs = rec.ExtraArgs

	pr.Locked = true

	summary := plugin.ChangeSummary{
		Add:     int(result.Changes.Add),
		Change:  int(result.Changes.Change),
		Destroy: int(result.Changes.Destroy),
	}
	ev, planRec := o.lockEventFor(rec, result.Success, summary, result.PlanData)
	tr, lockErr := o.locks.Apply(ctx, rec.ProjectKey, rec.PRNumber, ev, planRec)
	if lockErr == nil {
		slog.InfoContext(ctx, "operation result received",
			"operation_id", operationID,
			"lock_key", rec.ProjectKey,
			"pr_number", rec.PRNumber,
			"operation", rec.Operation,
			"success", result.Success,
			"lock_event", string(ev),
			"lock_from", string(tr.From),
			"lock_to", string(tr.To),
			"lock_released", tr.Released,
		)
	}
	switch {
	case lockErr != nil:
		slog.ErrorContext(ctx, "applying lock event", "operation_id", operationID, "event", string(ev), "error", lockErr)
	case tr.Released:
		pr.Locked = false
		pr.LockNote = lockNoteFor(ev, tr)
	case tr.From == "":
		// No Lock existed. The pull request was closed while this
		// Operation was in flight — a race, not a failure, and the result
		// still has to reach the reader.
		pr.Locked = false
	default:
		pr.LockNote = lockNoteFor(ev, tr)
	}

	if err := o.records.Delete(ctx, operationID); err != nil {
		slog.ErrorContext(ctx, "deleting operation record", "operation_id", operationID, "error", err)
	}
	if err := publishDone(ctx, o.redis, operationID, pr); err != nil {
		slog.ErrorContext(ctx, "publishing done notification", "operation_id", operationID, "error", err)
	}

	return nil
}

// checkRunResultTitle and checkRunResultSummary render an Operation's
// outcome for the checks tab. GitHub shows a check run's title (and the
// first part of its summary) directly beneath the name, and the name
// itself must stay stable — it is what required status checks match on —
// so these two fields are where the outcome belongs.
func checkRunResultTitle(success bool) string {
	if success {
		return "success"
	}
	return "failure"
}

func checkRunResultSummary(success bool, changes string) string {
	switch {
	case success && changes != "":
		return changes
	case success:
		return "No changes."
	case changes != "":
		return "The operation failed. " + changes
	default:
		return "The operation failed — see the output below."
	}
}

func changeSummaryText(c rpc.ChangeSummary) string {
	if c.Add == 0 && c.Change == 0 && c.Destroy == 0 {
		return ""
	}
	return fmt.Sprintf("add: %d, change: %d, destroy: %d", c.Add, c.Change, c.Destroy)
}
