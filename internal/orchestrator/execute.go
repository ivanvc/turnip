package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
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
func (o *Orchestrator) executeOne(ctx context.Context, client github.GitHubClient, repo github.Repository, pr github.PullRequest, installationID int64, t Target) (result github.ProjectResult) {
	start := time.Now()
	defer func() {
		metrics.ObserveOperationDuration(t.Project.Tool, t.Operation, time.Since(start))
	}()

	p, ok := o.plugins[t.Project.Tool]
	if !ok {
		return rejectedResult(t, fmt.Sprintf("tool %q is not supported", t.Project.Tool))
	}

	// Resolved before any Lock is acquired: a refused ServiceAccount must
	// not leave a Lock held for an Operation that never runs.
	serviceAccount, err := resolveServiceAccount(t.Project, o.runnerServiceAccount, o.allowedOverrides)
	if err != nil {
		return rejectedResult(t, err.Error())
	}

	// Repository-scoped rather than per-Project, but resolved in the same
	// place and for the same reason: a refused override must not leave a
	// Lock held for an Operation that never runs.
	submodules, err := resolveSubmodules(t.Clone, o.cloneSubmodules, o.allowedOverrides)
	if err != nil {
		return rejectedResult(t, err.Error())
	}

	isPlan := t.Operation == p.GetPlanOperation()
	key := projectKey(repo.Owner, repo.Name, t.Project.Name)

	// Only the plan chooses a scope, because it is the only Operation whose
	// output a human reviews. Everything else inherits the scope recorded on
	// the Lock, so arguments of its own could only contradict what was
	// reviewed.
	//
	// Keyed off !isPlan rather than a list of operation names, so an
	// Operation a future Plugin adds is covered without anyone remembering
	// to add it here. Refused before the Lock is touched, alongside the
	// ServiceAccount and submodule refusals above and for the same reason:
	// an Operation that was never going to run should leave no Lock, no
	// check run and no Job behind.
	//
	// Refused rather than ignored: a silently discarded argument is
	// indistinguishable from an honoured one until the infrastructure
	// changes, by which point the author's belief about what they applied
	// is wrong with nothing on the page to correct it.
	if !isPlan && len(t.ExtraArgs) > 0 {
		return rejectedResult(t, fmt.Sprintf(
			"%q does not accept arguments (got %s); it replays the scope the plan recorded. Pass arguments to %q instead.",
			t.Operation, strings.Join(t.ExtraArgs, " "), p.GetPlanOperation(),
		))
	}

	var planData []byte
	// execArgs is what actually reaches the tool: the trigger's own
	// arguments for a plan, and the scope the plan recorded for everything
	// else. The refusal above is what makes this unambiguous — for a
	// non-plan Operation t.ExtraArgs is necessarily empty, so there is
	// exactly one candidate.
	execArgs := t.ExtraArgs

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
				// The holder could not be determined, so the message says
				// the Project is locked without inventing a reference.
				return rejectedResult(t, "locked by another PR")
			}
			rejected := rejectedResult(t, fmt.Sprintf("locked by PR #%d", status.PRNumber))
			// The URL was already in the status being read; carrying it
			// lets the comment link to the blocking pull request rather
			// than merely naming a number (Requirement 7.2).
			rejected.BlockedBy = &github.BlockingPullRequest{
				Number: status.PRNumber,
				URL:    status.PullRequestURL,
			}
			return rejected
		}
		metrics.LockAttempt("acquired")
	} else {
		locked, err := o.locks.IsLockedByPR(ctx, key, pr.Number)
		if err != nil || !locked {
			return rejectedResult(t, "no lock (or a different PR's lock) is held; a new plan is required")
		}
		// Every mutating Operation replays the recorded plan, not just the
		// apply: a sync that ran unscoped after a scoped diff would change
		// more than anyone reviewed. This branch is already the "not a
		// plan" case, so the fetch needs no further condition.
		//
		// The Lock requirement above already existed; what this adds is
		// that the Lock must carry a recorded plan, which is also what
		// turns a pre-upgrade Lock into a request to re-plan.
		plan, err := o.locks.GetPlan(ctx, key, pr.Number)
		if err != nil {
			if errors.Is(err, lock.ErrNoPlan) {
				return rejectedResult(t, "no plan recorded; a new plan is required")
			}
			return rejectedResult(t, fmt.Sprintf("retrieving plan: %v", err))
		}
		planData = plan.Data
		execArgs = plan.Args
	}

	// checkRunErr is deliberately its own variable, not the shared err:
	// the deferred closure below reads it after every later statement has
	// run, and `token, err := ...` / `job, err := ...` reassign err in
	// this same scope — so closing over err would report whatever the
	// last operation left behind (nil, on the success path) instead of
	// this failure.
	checkRunID, checkRunErr := client.CreateCheckRun(ctx, repo.Owner, repo.Name, github.CheckRunOptions{
		Name:    checkRunName(t.Project.Name, t.Operation),
		HeadSHA: pr.HeadSHA,
		Status:  "in_progress",
		Title:   "in progress",
		Summary: fmt.Sprintf("Running `%s` for Project `%s`.", t.Operation, t.Project.Name),
	})
	if checkRunErr != nil {
		// Soft failure (Decision 3): the Operation still runs.
		// checkRunID stays 0, so later UpdateCheckRun calls for this
		// Target are skipped.
		slog.ErrorContext(ctx, "creating check run", "owner", repo.Owner, "repo", repo.Name, "pr_number", pr.Number, "project", t.Project.Name, "error", checkRunErr)
	}
	// ...but it is not silent. Whatever this function ends up returning —
	// a rejection below, or the Runner's own result — carries a note
	// saying the check run is missing and why. Without it the PR shows a
	// result comment and simply no check run, with the reason reachable
	// only from the Server's log.
	defer func() { result = appendCheckRunNote(result, checkRunErr) }()

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
		ExtraArgs:      execArgs,
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
		OperationID:    operationID,
		Operation:      t.Operation,
		RepoURL:        repo.URL,
		CommitSHA:      pr.HeadSHA,
		BaseRef:        pr.BaseRef,
		GitHubToken:    token,
		ServerAddr:     o.runnerServerAddr,
		ExtraArgs:      execArgs,
		PlanData:       planData,
		RunnerImage:    o.runnerImage,
		ServiceAccount: serviceAccount,
		Submodules:     submodules,
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
				Title:   "failure",
				Summary: "The Runner Job could not be created, so this operation never ran.",
				Text:    fmt.Sprintf("creating Runner Job: %v", err),
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

// appendCheckRunNote adds a short note to a result's Output when this
// Target's check run couldn't be created or updated. A check run that
// never appeared is otherwise invisible on the pull request: the result
// comment says nothing about it, and the underlying GitHub error reaches
// only the Server log (Requirement 9.5).
func appendCheckRunNote(result github.ProjectResult, err error) github.ProjectResult {
	if err == nil {
		return result
	}
	result.Output += fmt.Sprintf(
		"\n\nNote: this Project's GitHub check run could not be recorded (%v). "+
			"The operation itself is unaffected — only its entry in the PR's checks tab is missing.",
		err,
	)
	return result
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
