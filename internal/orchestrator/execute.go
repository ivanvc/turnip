package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/ivanvc/turnip/internal/github"
	"github.com/ivanvc/turnip/internal/jobs"
	"github.com/ivanvc/turnip/internal/lock"
	"github.com/ivanvc/turnip/internal/metrics"
)

func projectKey(owner, repo, project string) string {
	return fmt.Sprintf("%s/%s/%s", owner, repo, project)
}

func pullRequestURL(owner, repo string, prNumber int) string {
	return fmt.Sprintf("https://github.com/%s/%s/pull/%d", owner, repo, prNumber)
}

// executeTargets runs one goroutine per Target (Requirement 2.4/17.1's
// concurrency requirement) and waits for all of them before returning.
func (o *Orchestrator) executeTargets(ctx context.Context, client github.GitHubClient, repo github.Repository, pr github.PullRequest, installationID int64, targets []Target) []github.ProjectResult {
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		results []github.ProjectResult
	)
	for _, t := range targets {
		wg.Add(1)
		go func(t Target) {
			defer wg.Done()
			r := o.executeOne(ctx, client, repo, pr, installationID, t)
			mu.Lock()
			results = append(results, r)
			mu.Unlock()
		}(t)
	}
	wg.Wait()
	return results
}

// executeOne runs the Lock → check run → Job flow for one Target
// (Requirements 6, 7, 9), blocking until the Operation is finalized (a
// genuine result or a sweep-detected timeout) and returning its
// ProjectResult.
func (o *Orchestrator) executeOne(ctx context.Context, client github.GitHubClient, repo github.Repository, pr github.PullRequest, installationID int64, t Target) github.ProjectResult {
	start := time.Now()
	defer func() {
		metrics.ObserveOperationDuration(t.Project.Tool, t.Operation, time.Since(start))
	}()

	p, ok := o.plugins[t.Project.Tool]
	if !ok {
		return rejectedResult(t, fmt.Sprintf("tool %q is not supported", t.Project.Tool))
	}
	isPlan := t.Operation == p.GetPlanOperation()
	isApply := t.Operation == p.GetApplyOperation()
	key := projectKey(repo.Owner, repo.Name, t.Project.Name)

	var planData []byte

	if isPlan {
		acquired, err := o.locks.AcquireLock(ctx, key, pr.Number, pullRequestURL(repo.Owner, repo.Name, pr.Number), t.TriggeredBy)
		if err != nil {
			metrics.LockAttempt("rejected")
			return rejectedResult(t, fmt.Sprintf("acquiring lock: %v", err))
		}
		if !acquired {
			metrics.LockAttempt("rejected")
			status, err := o.locks.GetLockStatus(ctx, key)
			if err != nil || !status.Locked {
				return rejectedResult(t, "locked by another PR")
			}
			return rejectedResult(t, fmt.Sprintf("locked by PR #%d", status.PRNumber))
		}
		metrics.LockAttempt("acquired")
	} else {
		locked, err := o.locks.IsLockedByPR(ctx, key, pr.Number)
		if err != nil || !locked {
			return rejectedResult(t, "no lock (or a different PR's lock) is held; a new plan is required")
		}
		if isApply {
			data, _, err := o.locks.GetPlanData(ctx, key, pr.Number)
			if err != nil {
				if errors.Is(err, lock.ErrNoPlanData) {
					return rejectedResult(t, "no plan data stored; a new plan is required")
				}
				return rejectedResult(t, fmt.Sprintf("retrieving plan data: %v", err))
			}
			planData = data
		}
	}

	checkRunID, err := client.CreateCheckRun(ctx, repo.Owner, repo.Name, github.CheckRunOptions{
		Name:    checkRunName(t.Project.Name, t.Operation),
		HeadSHA: pr.HeadSHA,
		Status:  "in_progress",
	})
	if err != nil {
		// Soft failure (Decision 3): logged and ignored, the Operation
		// still runs. checkRunID stays 0, so later UpdateCheckRun calls
		// for this Target are skipped.
		slog.ErrorContext(ctx, "creating check run", "owner", repo.Owner, "repo", repo.Name, "pr_number", pr.Number, "project", t.Project.Name, "error", err)
	}

	operationID := uuid.NewString()
	rec := &OperationRecord{
		OperationID:    operationID,
		ProjectKey:     key,
		Project:        t.Project,
		Owner:          repo.Owner,
		Repo:           repo.Name,
		InstallationID: installationID,
		PRNumber:       pr.Number,
		PRURL:          pullRequestURL(repo.Owner, repo.Name, pr.Number),
		HeadSHA:        pr.HeadSHA,
		Operation:      t.Operation,
		IsApply:        isApply,
		ExtraArgs:      t.ExtraArgs,
		PlanData:       planData,
		TriggeredBy:    t.TriggeredBy,
		CheckRunID:     checkRunID,
		StartDeadline:  time.Now().Add(o.startTimeout).Unix(),
		CreatedAt:      time.Now(),
	}
	if err := o.records.Create(ctx, rec); err != nil {
		return rejectedResult(t, fmt.Sprintf("creating operation record: %v", err))
	}

	// Subscribe before creating the Job: publishDone must never be able to
	// fire before a subscriber exists (Redis Pub/Sub has no replay). The
	// bound here is deliberately generous (matching operationTTL, not
	// o.startTimeout) — a Job that has genuinely started may still be
	// legitimately mid-apply for a long time; this exists only to stop
	// leaking a goroutine forever if a publish never arrives at all (a bug
	// elsewhere), not to police how long a real Operation is allowed to run.
	doneCtx, cancel := context.WithTimeout(ctx, operationTTL)
	defer cancel()
	done := make(chan struct{})
	var waitResult github.ProjectResult
	var waitErr error
	go func() {
		waitResult, waitErr = waitForDone(doneCtx, o.redis, operationID)
		close(done)
	}()

	token, err := client.GenerateInstallationToken(ctx)
	if err != nil {
		o.deleteRecord(ctx, operationID)
		return rejectedResult(t, fmt.Sprintf("generating installation token: %v", err))
	}

	job, err := jobs.BuildJob(t.Project, jobs.OperationParams{
		OperationID: operationID,
		Operation:   t.Operation,
		RepoURL:     repo.URL,
		CommitSHA:   pr.HeadSHA,
		BaseRef:     pr.BaseRef,
		GitHubToken: token,
		ServerAddr:  o.runnerServerAddr,
		ExtraArgs:   t.ExtraArgs,
		PlanData:    planData,
		RunnerImage: o.runnerImage,
	})
	if err != nil {
		o.deleteRecord(ctx, operationID)
		return rejectedResult(t, fmt.Sprintf("building job: %v", err))
	}

	created, err := o.jobs.Create(ctx, job)
	if err != nil {
		o.deleteRecord(ctx, operationID)
		if checkRunID != 0 {
			_ = client.UpdateCheckRun(ctx, repo.Owner, repo.Name, checkRunID, github.CheckRunOptions{
				Name: checkRunName(t.Project.Name, t.Operation), Status: "completed", Conclusion: "failure",
				Text: fmt.Sprintf("creating Runner Job: %v", err),
			})
		}
		return rejectedResult(t, fmt.Sprintf("creating job: %v", err))
	}
	if err := o.records.SetJobName(ctx, operationID, created.Name); err != nil {
		slog.ErrorContext(ctx, "recording job name for operation", "operation_id", operationID, "error", err)
	}

	<-done
	if waitErr != nil {
		return rejectedResult(t, fmt.Sprintf("waiting for result: %v", waitErr))
	}
	outcome := "failure"
	if waitResult.Success {
		outcome = "success"
	}
	metrics.OperationDispatched(t.Project.Tool, t.Operation, outcome)
	return waitResult
}

// deleteRecord is a best-effort cleanup for an Operation Record whose Job
// never ended up created — a failure here just means the 24h leak-
// prevention TTL (design.md's Redis Key Format) cleans it up eventually.
func (o *Orchestrator) deleteRecord(ctx context.Context, operationID string) {
	if err := o.records.Delete(ctx, operationID); err != nil {
		slog.ErrorContext(ctx, "deleting operation record", "operation_id", operationID, "error", err)
	}
}

func rejectedResult(t Target, reason string) github.ProjectResult {
	metrics.OperationDispatched(t.Project.Tool, t.Operation, "rejected")
	return github.ProjectResult{
		ProjectName: t.Project.Name,
		Tool:        t.Project.Tool,
		Operation:   t.Operation,
		Success:     false,
		Output:      reason,
	}
}

func checkRunName(projectName, operation string) string {
	return fmt.Sprintf("turnip/%s/%s", projectName, operation)
}
