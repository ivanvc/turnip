package orchestrator

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/ivanvc/turnip/internal/github"
	"github.com/ivanvc/turnip/internal/lock"
	"github.com/ivanvc/turnip/internal/metrics"
	"github.com/ivanvc/turnip/internal/plugin"
	"github.com/ivanvc/turnip/internal/rpc"
)

var _ rpc.OperationHandler = (*Orchestrator)(nil)

// HandleLog records that operationID has produced output (Requirement
// 7.7/8.2), and observes Runner Job Start Latency the one time this call
// is the one that actually performs the started transition (Decision 2
// in metrics' design.md).
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

// HandleResult finalizes an Operation: Lock (Requirement 6.6-6.8),
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
	// and offer the command that releases it.
	//
	// It is deliberately not derived from Success: which outcomes leave a
	// Lock held is the lock lifecycle's business, and stating the fact
	// rather than inferring it keeps the comment honest when that
	// lifecycle changes. Every path below sets it explicitly.
	pr.Locked = true

	if result.Success {
		p, ok := o.plugins[rec.Project.Tool]
		switch {
		case ok && rec.Operation == p.GetPlanOperation():
			summary := plugin.ChangeSummary{
				Add:     int(result.Changes.Add),
				Change:  int(result.Changes.Change),
				Destroy: int(result.Changes.Destroy),
			}
			// Recorded because the plan succeeded, not because the tool
			// produced an artifact — Helmfile never does, and keying this
			// off len(result.PlanData) is what made its apply unreachable.
			//
			// The arguments stored are the Operation's own, taken from the
			// record the Job was built from rather than re-derived from the
			// trigger line, so whatever turnip normalised is what a later
			// mutating Operation replays.
			if err := o.locks.StorePlan(ctx, rec.ProjectKey, rec.PRNumber, lock.PlanRecord{
				Data:    result.PlanData,
				Args:    rec.ExtraArgs,
				Summary: summary,
			}); err != nil {
				slog.ErrorContext(ctx, "storing plan", "operation_id", operationID, "error", err)
			}
		default:
			// Every successful Operation that is not the plan discharges the
			// Lock: apply, sync, and anything else a Plugin exposes.
			// Narrowing this back to rec.IsApply is the regression worth
			// guarding — it reads like the rule, and it silently strands a
			// Project after a successful sync.
			//
			// An unregistered tool cannot reach here. executeOne rejects one
			// before any record exists, so the !ok case never releases a
			// Lock for a Plugin turnip does not have.
			if err := o.locks.ReleaseLock(ctx, rec.ProjectKey, rec.PRNumber); err != nil {
				slog.ErrorContext(ctx, "releasing lock", "operation_id", operationID, "error", err)
			} else {
				pr.Locked = false
				pr.Output += "\n\nLock released — this Project is now free for another PR to plan against."
			}
		}
	}
	// A failed or timed-out Operation triggers no Lock mutation: a failed
	// plan leaves the Lock held with no plan recorded, and a failed apply
	// leaves it held rather than released, because the infrastructure may
	// be partly changed and another PR must not apply on top of it.
	//
	// The governing requirement is 7 (Redis/Valkey-Based Locking). An
	// earlier comment here cited "Requirement 6.8", which does not exist —
	// Requirement 6 is Plan with Destroy Flag and has five criteria, none
	// about Lock lifecycle. The wrong citation is what made this behaviour
	// look specified when nothing specified it.
	//
	// Slice 18 revisits the first case — a failed plan holds a Lock that
	// protects nothing — and pr.Locked will follow it without this code
	// changing, because it reports the state rather than deducing it.

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
