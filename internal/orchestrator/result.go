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

	if result.Success {
		p, ok := o.plugins[rec.Project.Tool]
		switch {
		case ok && rec.Operation == p.GetPlanOperation() && len(result.PlanData) > 0:
			summary := plugin.ChangeSummary{
				Add:     int(result.Changes.Add),
				Change:  int(result.Changes.Change),
				Destroy: int(result.Changes.Destroy),
			}
			if err := o.locks.StorePlanData(ctx, rec.ProjectKey, rec.PRNumber, result.PlanData, summary); err != nil {
				slog.ErrorContext(ctx, "storing plan data", "operation_id", operationID, "error", err)
			}
		case rec.IsApply:
			if err := o.locks.ReleaseLock(ctx, rec.ProjectKey, rec.PRNumber); err != nil {
				slog.ErrorContext(ctx, "releasing lock", "operation_id", operationID, "error", err)
			} else {
				pr.Output += "\n\nLock released — this Project is now free for another PR to plan against."
			}
		}
	}
	// A failed or timed-out Operation triggers no Lock mutation
	// (Requirement 6.8): a failed plan leaves the Lock held with no new
	// plan data, a failed apply leaves the Lock held rather than released.

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
