package orchestrator

import (
	"context"
	"fmt"
	"log"

	"github.com/ivanvc/turnip/internal/github"
	"github.com/ivanvc/turnip/internal/plugin"
	"github.com/ivanvc/turnip/internal/rpc"
)

var _ rpc.OperationHandler = (*Orchestrator)(nil)

// HandleLog records that operationID has produced output (Requirement
// 7.7/8.2) — nothing else reacts to a log line at this layer.
func (o *Orchestrator) HandleLog(ctx context.Context, operationID string, line rpc.LogLine) error {
	return o.records.MarkStarted(ctx, operationID)
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
			Name:    checkRunName(rec.Project.Name, rec.Operation),
			Status:  "completed",
			Summary: changeSummaryText(result.Changes),
			Text:    result.Output,
		}
		if result.Success {
			opts.Conclusion = "success"
		} else {
			opts.Conclusion = "failure"
		}
		if err := client.UpdateCheckRun(ctx, rec.Owner, rec.Repo, rec.CheckRunID, opts); err != nil {
			log.Printf("orchestrator: updating check run for operation %q: %v", operationID, err)
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
				log.Printf("orchestrator: storing plan data for operation %q: %v", operationID, err)
			}
		case rec.IsApply:
			if err := o.locks.ReleaseLock(ctx, rec.ProjectKey, rec.PRNumber); err != nil {
				log.Printf("orchestrator: releasing lock for operation %q: %v", operationID, err)
			} else {
				pr.Output += "\n\nLock released — this Project is now free for another PR to plan against."
			}
		}
	}
	// A failed or timed-out Operation triggers no Lock mutation
	// (Requirement 6.8): a failed plan leaves the Lock held with no new
	// plan data, a failed apply leaves the Lock held rather than released.

	if err := o.records.Delete(ctx, operationID); err != nil {
		log.Printf("orchestrator: deleting operation record %q: %v", operationID, err)
	}
	if err := publishDone(ctx, o.redis, operationID, pr); err != nil {
		log.Printf("orchestrator: publishing done notification for %q: %v", operationID, err)
	}

	return nil
}

func changeSummaryText(c rpc.ChangeSummary) string {
	if c.Add == 0 && c.Change == 0 && c.Destroy == 0 {
		return ""
	}
	return fmt.Sprintf("add: %d, change: %d, destroy: %d", c.Add, c.Change, c.Destroy)
}
