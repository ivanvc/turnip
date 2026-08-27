package orchestrator

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/ivanvc/turnip/internal/github"
	"github.com/ivanvc/turnip/internal/jobs"
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
		log.Printf("orchestrator: sweep scan: %v", err)
		return
	}

	now := time.Now()
	for _, key := range keys {
		operationID := strings.TrimPrefix(key, "operation:")
		rec, claimed, err := o.records.ClaimForTimeout(ctx, operationID, now)
		if err != nil {
			log.Printf("orchestrator: sweep claim for %q: %v", operationID, err)
			continue
		}
		if !claimed {
			continue
		}
		o.reportTimeout(ctx, operationID, rec)
	}
}

func (o *Orchestrator) reportTimeout(ctx context.Context, operationID string, rec *OperationRecord) {
	var status *jobs.JobStatus
	if rec.JobName != "" {
		s, err := o.jobs.Status(ctx, rec.JobName)
		if err != nil {
			log.Printf("orchestrator: diagnosing timeout for operation %q: %v", operationID, err)
		} else {
			status = s
		}
	}

	pr := github.ProjectResult{
		ProjectName: rec.Project.Name,
		Tool:        rec.Project.Tool,
		Operation:   rec.Operation,
		Success:     false,
		Output:      timeoutDiagnostic(rec.JobName, status),
	}

	if rec.CheckRunID != 0 {
		client := o.installationClient(rec.InstallationID)
		err := client.UpdateCheckRun(ctx, rec.Owner, rec.Repo, rec.CheckRunID, github.CheckRunOptions{
			Name:       checkRunName(rec.Project.Name, rec.Operation),
			Status:     "completed",
			Conclusion: "failure",
			Text:       pr.Output,
		})
		if err != nil {
			log.Printf("orchestrator: updating check run for timed-out operation %q: %v", operationID, err)
		}
	}

	// No ReleaseLock call (Requirement 6.8/8.5): a timed-out Operation
	// leaves its Lock held. No jobs.Client.Delete call either (Requirement
	// 8.6): a Runner that connects after this point may still be running
	// real tool work — the Job's TTL is this path's cleanup mechanism.

	if err := o.records.Delete(ctx, operationID); err != nil {
		log.Printf("orchestrator: deleting timed-out operation record %q: %v", operationID, err)
	}
	if err := publishDone(ctx, o.redis, operationID, pr); err != nil {
		log.Printf("orchestrator: publishing timeout notification for %q: %v", operationID, err)
	}
}

// timeoutDiagnostic renders a JobStatus into readable text (Requirement
// 8.4), rather than a bare "timed out" message.
func timeoutDiagnostic(jobName string, status *jobs.JobStatus) string {
	if status == nil || !status.JobFound {
		return "No Job/Pod found for this Operation — it may have been deleted or never successfully scheduled"
	}
	if status.PodReason != "" {
		return fmt.Sprintf("Job %s: container stuck (%s)", jobName, status.PodReason)
	}
	if status.PodPhase != "" {
		return fmt.Sprintf("Job %s: Pod still %s after 5 minutes", jobName, status.PodPhase)
	}
	return fmt.Sprintf("Job %s: no Pod found yet after 5 minutes", jobName)
}
