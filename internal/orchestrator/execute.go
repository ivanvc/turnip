package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
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

// executeOne runs the Lock → check run → Job flow for one Target
// (Requirements 6, 7, 9) as part of command of trigger triggerID, and ends
// when the Job is created. It returns nil then: the Operation Record names
// the trigger and the command, and whichever replica finalizes the
// Operation completes the Project in its trigger. It returns the Project's
// refusal when no Job was created, which the caller completes instead. So
// each Project is completed exactly once, by one side or the other.
func (o *Orchestrator) executeOne(ctx context.Context, client github.GitHubClient, repo github.Repository, pr github.PullRequest, installationID int64, triggerID string, command int, t Target) (refused *github.ProjectResult) {
	// A dispatched Operation's duration is observed when it is finalized,
	// from its record's creation time. A refusal ends here, so it is
	// observed here.
	start := time.Now()
	defer func() {
		if refused != nil {
			metrics.ObserveOperationDuration(t.Project.Tool, t.Operation, time.Since(start))
		}
	}()

	// Every event below concerns this one Target, so its identity is
	// attached once rather than repeated (and left to drift) per call.
	log := slog.With(
		"owner", repo.Owner,
		"repo", repo.Name,
		"pr_number", pr.Number,
		"project", t.Project.Name,
		"operation", t.Operation,
	)
	// The check run's ID is declared ahead of reject so the closure sees a
	// Project_Check that already exists: a refusal after it was created
	// must complete it rather than leave it in progress (check-run-refusals
	// Requirement 4.4). Zero until then, and zero if creating it failed.
	var (
		checkRunID  int64
		checkRunErr error
	)

	// The reason is what the pull request comment shows. Logged as well,
	// because some of these are infrastructure failures (Redis, the
	// Kubernetes API) that otherwise reach only the comment.
	//
	// A refused plan also leaves the Project unplanned on this commit, and
	// the pull request's record has to say so, or the aggregate check could
	// pass without it (aggregate-check-run Requirement 4.4). A refused
	// Mutating_Operation ran nothing and changes nothing (4.7). Neither
	// applies once the Job exists: from then on the Operation's outcome is
	// HandleResult's or the sweep's to record, and nothing here refuses it.
	reject := func(r refusal) *github.ProjectResult {
		log.WarnContext(ctx, "operation rejected", "reason", r.reason)
		rejected := rejectedResult(t, r.reason)
		p, ok := o.plugins[t.Project.Tool]
		isPlan := ok && t.Operation == p.GetPlanOperation()
		// A running check that could not be created is already noted by the
		// deferred appendCheckRunNote; trying again here would only note the
		// same missing check twice.
		if checkRunErr == nil {
			action, err := o.refusalCheck(ctx, client, repo, pr, t, isPlan, checkRunID, r)
			rejected = appendCheckRunNote(rejected, action, err)
		}
		if isPlan {
			ref := prRef{Owner: repo.Owner, Repo: repo.Name, PRNumber: pr.Number, HeadSHA: pr.HeadSHA}
			if err := o.recordOutcome(ctx, client, ref, t.Project.Name, refusalEntry(t, r)); err != nil {
				log.ErrorContext(ctx, "recording refused plan for the aggregate check", "error", err)
				rejected = appendAggregateNote(rejected, err)
			}
		}
		return &rejected
	}

	// Every Target has a Plugin: Targets come only from a turnip.yaml that
	// config.Parse validated against these same tool names, or from a
	// comment whose selection already looked the Plugin up. A miss is a bug
	// in turnip, not something the author can fix, so it is reported as one
	// rather than refused like a user's mistake: no check run, and nothing
	// recorded against the Project.
	p, ok := o.plugins[t.Project.Tool]
	if !ok {
		log.ErrorContext(ctx, "no plugin registered for a target's tool", "tool", t.Project.Tool)
		rejected := rejectedResult(t, fmt.Sprintf("internal error: no plugin is registered for tool %q", t.Project.Tool))
		return &rejected
	}

	// Resolved before any Lock is acquired: a refused ServiceAccount must
	// not leave a Lock held for an Operation that never runs.
	serviceAccount, err := resolveServiceAccount(t.Project, o.runnerServiceAccount, o.allowedOverrides)
	if err != nil {
		return reject(overrideRefusal(err))
	}

	// Repository-scoped rather than per-Project, but resolved in the same
	// place and for the same reason: a refused override must not leave a
	// Lock held for an Operation that never runs.
	submodules, err := resolveSubmodules(t.Clone, o.cloneSubmodules, o.allowedOverrides)
	if err != nil {
		return reject(overrideRefusal(err))
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
	// indistinguishable from an honored one until the infrastructure
	// changes, by which point the author's belief about what they applied
	// is wrong with nothing on the page to correct it.
	if !isPlan && len(t.ExtraArgs) > 0 {
		return reject(refusal{reason: fmt.Sprintf(
			"%q does not accept arguments (got %s); it replays the scope the plan recorded. Pass arguments to %q instead.",
			t.Operation, strings.Join(t.ExtraArgs, " "), p.GetPlanOperation(),
		)})
	}

	var (
		planData []byte
		// recordedDigest is the image digest the plan ran, which every
		// Mutating_Operation runs in place of re-resolving the Tag_Spec.
		recordedDigest string
	)
	// runningTitle is what the Project_Check says while this Operation
	// runs. A plan says its scope; everything else says what it is about to
	// change, from the plan it replays (check-run-titles Requirement 3).
	title := runningTitle(t.ExtraArgs)
	// execArgs is what actually reaches the tool: the trigger's own
	// arguments for a plan, and the scope the plan recorded for everything
	// else. The refusal above is what makes this unambiguous — for a
	// non-plan Operation t.ExtraArgs is necessarily empty, so there is
	// exactly one candidate.
	execArgs := t.ExtraArgs

	if isPlan {
		// AcquireForPlan applies the dispatch edge in the same evaluation
		// as the acquisition, so a Lock is never observable as acquired
		// for a new plan while still reporting its old one as appliable.
		//
		// The transition is deliberately not reported to the reader here.
		// This Operation's own result is about to say what the new plan
		// found, and "your previous plan was superseded" immediately above
		// the plan that superseded it is noise. The case where it matters
		// — the re-plan failing, leaving nothing usable — is announced by
		// that failure instead.
		acquired, _, err := o.locks.AcquireForPlan(ctx, key, pr.Number, pullRequestURL(repo.Owner, repo.Name, pr.Number), t.TriggeredBy)
		if err != nil {
			metrics.LockAttempt("rejected")
			return reject(refusal{
				kind:   refusalInfrastructure,
				reason: fmt.Sprintf("acquiring lock: %v", err),
				title:  lockNotAcquiredTitle(),
			})
		}
		if !acquired {
			metrics.LockAttempt("rejected")
			status, err := o.locks.GetLockStatus(ctx, key)
			if err != nil || !status.Locked {
				// The holder could not be determined, so the message says
				// the Project is locked without inventing a reference.
				return reject(refusal{kind: refusalLockWait, reason: "locked by another PR"})
			}
			rejected := reject(refusal{
				kind:      refusalLockWait,
				reason:    fmt.Sprintf("locked by PR #%d", status.PRNumber),
				blockedBy: status.PRNumber,
			})
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
			return reject(refusal{reason: "no lock (or a different PR's lock) is held; a new plan is required"})
		}
		status, err := o.locks.GetLockStatus(ctx, key)
		if err != nil {
			return reject(refusal{reason: fmt.Sprintf("reading lock: %v", err)})
		}
		// One condition where there were two, and the refusals are worded
		// apart on purpose: "nothing was ever recorded" and "what was
		// recorded is no longer true" are different situations, and only
		// the second tells the author that something happened.
		switch status.State {
		case lock.StatePlanReady:
		case lock.StatePlanStale:
			return reject(refusal{reason: "the recorded plan is no longer valid: a new commit, a failed plan, or an operation that did not complete has superseded it. Re-plan before retrying."})
		default:
			return reject(refusal{reason: "no plan recorded; a new plan is required"})
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
				return reject(refusal{reason: "no plan recorded; a new plan is required"})
			}
			return reject(refusal{reason: fmt.Sprintf("retrieving plan: %v", err)})
		}
		// Without the digest the plan ran, the only image left to run is
		// the Tag_Spec resolved again, which may no longer be what was
		// reviewed. Refused before the check run exists, like the other
		// missing-plan refusals: nothing ran, and the record is unchanged.
		if plan.ImageDigest == "" {
			return reject(refusal{reason: "the image this plan ran could not be recorded; re-plan"})
		}
		planData = plan.Data
		execArgs = plan.Args
		recordedDigest = plan.ImageDigest
		planCounts := changeCounts(plan.Summary)
		planCounts.Unreported = plan.Unreported
		title = runningRecordedPlanTitle(planCounts, plan.Args)
	}

	// checkRunErr is deliberately its own variable, not the shared err:
	// the deferred closure below reads it after every later statement has
	// run, and `token, err := ...` / `job, err := ...` reassign err in
	// this same scope — so closing over err would report whatever the
	// last operation left behind (nil, on the success path) instead of
	// this failure.
	checkRunID, checkRunErr = client.CreateCheckRun(ctx, repo.Owner, repo.Name, github.CheckRunOptions{
		Name:    checkRunName(t.Project.Name, t.Operation),
		HeadSHA: pr.HeadSHA,
		Status:  "in_progress",
		Title:   title,
		Summary: fmt.Sprintf("Running `%s` for Project `%s`.", t.Operation, t.Project.Name),
	})
	if checkRunErr != nil {
		// Soft failure (Decision 3): the Operation still runs.
		// checkRunID stays 0, so later UpdateCheckRun calls for this
		// Target are skipped.
		slog.ErrorContext(ctx, "creating check run", "owner", repo.Owner, "repo", repo.Name, "pr_number", pr.Number, "project", t.Project.Name, "error", checkRunErr)
	}
	// ...but it is not silent. Whatever reaches the pull request for this
	// Project carries a note saying the check run is missing and why: a
	// refusal below gets it here, and the Runner's own result gets it at
	// finalization, from the error kept in the Operation Record. Without it
	// the PR shows a result comment and simply no check run, with the reason
	// reachable only from the Server's log.
	defer func() {
		if refused != nil {
			*refused = appendCheckRunNote(*refused, checkCreate, checkRunErr)
		}
	}()

	checkRunErrText := ""
	if checkRunErr != nil {
		checkRunErrText = checkRunErr.Error()
	}
	operationID := uuid.NewString()
	rec := &OperationRecord{
		OperationID:    operationID,
		ProjectKey:     key,
		Project:        t.Project,
		Owner:          repo.Owner,
		Repo:           repo.Name,
		RepositoryID:   repo.ID,
		InstallationID: installationID,
		PRNumber:       pr.Number,
		PRURL:          pullRequestURL(repo.Owner, repo.Name, pr.Number),
		HeadSHA:        pr.HeadSHA,
		Operation:      t.Operation,
		ExtraArgs:      execArgs,
		// Resolved here, where the Project, the commit and the submodule
		// mode are all in hand, and consumed when the Runner fetches. The
		// credential itself is minted then, not now, so it is not live
		// through queueing, scheduling and image pulls (Requirement 2.4).
		TokenRepositories: o.tokenScopeFor(ctx, client, repo, pr.HeadSHA, submodules).Repositories,
		TriggeredBy:       t.TriggeredBy,
		TriggerID:         triggerID,
		CommandIndex:      command,
		CheckRunID:        checkRunID,
		CheckRunError:     checkRunErrText,
		StartDeadline:     time.Now().Add(o.startTimeout).Unix(),
		CreatedAt:         time.Now(),
	}
	if err := o.records.Create(ctx, rec); err != nil {
		return reject(refusal{
			kind:   refusalInfrastructure,
			reason: fmt.Sprintf("creating operation record: %v", err),
			title:  recordNotSavedTitle(),
		})
	}

	imageRef, pullAlways := jobImage(t.Project, isPlan, recordedDigest)
	build := o.buildJob
	if build == nil {
		build = jobs.BuildJob
	}
	job, err := build(t.Project, jobs.OperationParams{
		OperationID:    operationID,
		Operation:      t.Operation,
		RepoURL:        repo.URL,
		CommitSHA:      pr.HeadSHA,
		BaseRef:        pr.BaseRef,
		ServerAddr:     o.runnerServerAddr,
		ExtraArgs:      execArgs,
		PlanData:       planData,
		RunnerImage:    o.runnerImage,
		ServiceAccount: serviceAccount,
		Submodules:     submodules,
		Provisioning:   jobProvisioning(t.Project, p.Provisioning()),
		ImageRef:       imageRef,
		PullAlways:     pullAlways,
		// The webhook's repository, never anything read from turnip.yaml or
		// the pull request: it alone names the Secret the Pod mounts keys
		// from. A zero id with keys listed fails the build, refusing the
		// Operation below.
		RepositoryID: repo.ID,
	})
	if err != nil {
		o.deleteRecord(ctx, operationID)
		return reject(refusal{
			kind:   refusalInfrastructure,
			reason: fmt.Sprintf("building job: %v", err),
			title:  jobNotBuiltTitle(),
		})
	}

	created, err := o.jobs.Create(ctx, job)
	if err != nil {
		o.deleteRecord(ctx, operationID)
		return reject(refusal{
			kind:   refusalInfrastructure,
			reason: fmt.Sprintf("creating job: %v", err),
			title:  jobNotCreatedTitle(),
		})
	}
	log.InfoContext(ctx, "runner job created", "operation_id", operationID, "job", created.Name)
	if err := o.records.SetJobName(ctx, operationID, created.Name); err != nil {
		slog.ErrorContext(ctx, "recording job name for operation", "operation_id", operationID, "error", err)
	}
	// The Job exists, so this Project is no longer the dispatcher's to
	// complete: HandleResult or the sweep finalizes the Operation and
	// completes the Project in its trigger.
	return nil
}

// refusalKind is what a refusal means for the Project_Check and the
// Pull_Request_Record. Each call site states it; nothing infers it from the
// reason's text, which is written for people and reworded freely.
type refusalKind int

const (
	refusalOther refusalKind = iota // no check; today's behavior
	refusalLockWait
	refusalConfiguration
	refusalInfrastructure
)

type refusal struct {
	kind      refusalKind
	reason    string // what the comment shows, as today
	blockedBy int    // Lock_Wait: the holder's PR number, 0 when unknown
	setting   string // Configuration_Refusal: "runner.serviceAccount", …
	title     string // Infrastructure_Error: its Title, from titles.go
}

// overrideRefusal classifies a refused override by its error type. Both
// resolvers return only their "not permitted" error, so anything else is
// left as other rather than guessed at. The setting is the same constant
// TURNIP_ALLOWED_OVERRIDES is parsed against, so the Title and the fix
// spell it the same way.
func overrideRefusal(err error) refusal {
	r := refusal{reason: err.Error()}
	var serviceAccount *ServiceAccountNotPermittedError
	var submodules *SubmodulesNotPermittedError
	switch {
	case errors.As(err, &serviceAccount):
		r.kind, r.setting = refusalConfiguration, overrideServiceAccount
	case errors.As(err, &submodules):
		r.kind, r.setting = refusalConfiguration, overrideCloneSubmodules
	}
	return r
}

// refusalEntry is what a refused plan leaves in the Pull_Request_Record.
// Only a Configuration_Refusal is the author's to fix, so only it is
// recorded as refused; a Lock_Wait or turnip's own failure leaves the
// Project not planned, which keeps the aggregate check in progress.
func refusalEntry(t Target, r refusal) ProjectEntry {
	entry := ProjectEntry{Outcome: OutcomeNotPlanned, Operation: t.Operation}
	switch r.kind {
	case refusalLockWait:
		entry.BlockedBy = r.blockedBy
	case refusalConfiguration:
		entry.Outcome, entry.Setting = OutcomeRefused, r.setting
	}
	return entry
}

// refusalCheck makes the Project_Check say what the refusal was. A plan
// refused before its check existed gets one created for it, queued for a
// Lock_Wait and failed otherwise, because nothing else would put the
// refusal in the checks list. A check that already exists is completed as
// failed for any Operation and any kind: leaving it in progress would claim
// a run that is not happening (check-run-refusals Requirement 4.4). The
// kind only chooses its Title there, so a refusal added later that forgets
// to pick one still cannot leave the check stuck.
//
// It returns the call it made, so a failure can be noted as a check that
// could not be created or one that could not be completed.
//
// The summary carries the full reason, as the comment does: GitHub rejects
// check-run output without one, and for a refused override it holds the
// instruction to permit it in TURNIP_ALLOWED_OVERRIDES.
func (o *Orchestrator) refusalCheck(ctx context.Context, client github.GitHubClient, repo github.Repository, pr github.PullRequest, t Target, isPlan bool, checkRunID int64, r refusal) (checkAction, error) {
	opts := github.CheckRunOptions{
		Name:    checkRunName(t.Project.Name, t.Operation),
		Summary: r.reason,
	}

	if checkRunID != 0 {
		// Never requeued, even for a Lock_Wait: GitHub does not document a
		// check moving from in progress back to queued (design Decision 1).
		opts.Status, opts.Conclusion = "completed", "failure"
		switch {
		case r.title != "":
			opts.Title = r.title
		case r.kind == refusalConfiguration:
			opts.Title = notPermittedTitle(r.setting)
		default:
			opts.Title = notStartedTitle()
		}
		if err := client.UpdateCheckRun(ctx, repo.Owner, repo.Name, checkRunID, opts); err != nil {
			slog.ErrorContext(ctx, "completing check run for refused operation", "owner", repo.Owner, "repo", repo.Name, "pr_number", pr.Number, "project", t.Project.Name, "error", err)
			return checkComplete, err
		}
		return checkComplete, nil
	}

	switch r.kind {
	case refusalLockWait:
		opts.Status, opts.Title = "queued", lockWaitTitle(r.blockedBy)
	case refusalConfiguration:
		opts.Status, opts.Conclusion, opts.Title = "completed", "failure", notPermittedTitle(r.setting)
	case refusalInfrastructure:
		opts.Status, opts.Conclusion, opts.Title = "completed", "failure", r.title
	default:
		return checkCreate, nil
	}

	// A refused Mutating_Operation ran nothing and changed nothing, so it
	// gets no check of its own (aggregate-check-run Requirement 4.7).
	if !isPlan {
		return checkCreate, nil
	}
	opts.HeadSHA = pr.HeadSHA
	if _, err := client.CreateCheckRun(ctx, repo.Owner, repo.Name, opts); err != nil {
		slog.ErrorContext(ctx, "creating check run for refused plan", "owner", repo.Owner, "repo", repo.Name, "pr_number", pr.Number, "project", t.Project.Name, "error", err)
		return checkCreate, err
	}
	return checkCreate, nil
}

// deleteRecord is a best-effort cleanup for an Operation Record whose Job
// never ended up created; a failure here just means the leak-prevention
// expiry (operationTTL) cleans it up eventually.
func (o *Orchestrator) deleteRecord(ctx context.Context, operationID string) {
	if err := o.records.Delete(ctx, operationID); err != nil {
		slog.ErrorContext(ctx, "deleting operation record", "operation_id", operationID, "error", err)
	}
}

// checkAction is the check-run call that failed, which decides what the
// note tells the reader the failure left behind.
type checkAction string

const (
	// checkCreate: the check never existed, so it is missing.
	checkCreate checkAction = "created"
	// checkComplete: the check exists and was left in progress.
	checkComplete checkAction = "completed"
)

// appendCheckRunNote adds a short note to a result's Output when this
// Target's check run couldn't be created or completed. Either failure is
// otherwise invisible on the pull request: the result comment says
// nothing about it, and the underlying GitHub error reaches only the
// Server log (Requirement 9.5). The note says which call failed, because
// a missing check and one stuck in progress (which can block a merge)
// call for different reactions.
func appendCheckRunNote(result github.ProjectResult, action checkAction, err error) github.ProjectResult {
	if err == nil {
		return result
	}
	consequence := "only its entry in the PR's checks tab is missing."
	if action == checkComplete {
		consequence = "but its check stays in progress in the PR's checks tab until the operation runs again."
	}
	joiner := "; "
	if action == checkComplete {
		joiner = ", "
	}
	result.Output += fmt.Sprintf(
		"\n\nNote: this Project's GitHub check run could not be %s (%v). "+
			"The operation itself is unaffected%s%s",
		action, err, joiner, consequence,
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

// checkRunName is a Project_Check's name. Operation first, so every diff —
// or every sync — lists together in the checks tab. The name is the check
// run's identity (branch protection matches on it), which is why the
// operator is pointed at the aggregate `turnip` check instead: this one
// only exists on pull requests that touch its Project.
func checkRunName(projectName, operation string) string {
	return fmt.Sprintf("turnip/%s/%s", operation, projectName)
}
