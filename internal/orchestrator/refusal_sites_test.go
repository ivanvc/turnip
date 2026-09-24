package orchestrator

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivanvc/turnip/internal/config"
	"github.com/ivanvc/turnip/internal/github"
	"github.com/ivanvc/turnip/internal/lock"
)

// These tests walk check-run-refusals design.md's sites table, one test per
// row of execute.go's refusals. A misclassified site still looks plausible
// on its own — a Lock_Wait reported as a failure still produces a check —
// so each asserts the exact status, conclusion and Title, and the Outcome
// the Pull_Request_Record ends up with.

// sitesClient records every check run created and updated, rather than
// fakeExecuteClient's last one: a refused plan also publishes the aggregate
// `turnip` check, which would otherwise overwrite the Project_Check under
// test.
type sitesClient struct {
	*fakeExecuteClient

	mu      sync.Mutex
	created []github.CheckRunOptions
	updated []github.CheckRunOptions
}

func newSitesClient() *sitesClient {
	return &sitesClient{fakeExecuteClient: &fakeExecuteClient{}}
}

func (c *sitesClient) CreateCheckRun(_ context.Context, _, _ string, opts github.CheckRunOptions) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.created = append(c.created, opts)
	return 555, nil
}

func (c *sitesClient) UpdateCheckRun(_ context.Context, _, _ string, _ int64, opts github.CheckRunOptions) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.updated = append(c.updated, opts)
	return nil
}

// projectChecks returns what was created and updated for one Target's
// Project_Check, leaving out the aggregate check.
func (c *sitesClient) projectChecks(t Target) (created, updated []github.CheckRunOptions) {
	c.mu.Lock()
	defer c.mu.Unlock()
	name := checkRunName(t.Project.Name, t.Operation)
	for _, opts := range c.created {
		if opts.Name == name {
			created = append(created, opts)
		}
	}
	for _, opts := range c.updated {
		if opts.Name == name {
			updated = append(updated, opts)
		}
	}
	return created, updated
}

var sitesRef = prRef{Owner: testRepo.Owner, Repo: testRepo.Name, PRNumber: testPR.Number, HeadSHA: testPR.HeadSHA}

func sitesOrchestrator(t *testing.T, locks lock.LockManager) (*Orchestrator, *fakeJobCreator) {
	t.Helper()
	jobsClient := &fakeJobCreator{t: t, result: github.ProjectResult{Success: true}}
	o, _ := testOrchestrator(t, locks, jobsClient)
	return o, jobsClient
}

func readSitesRecord(t *testing.T, o *Orchestrator) prStatus {
	t.Helper()
	st, err := o.records.ReadPRStatus(context.Background(), sitesRef)
	require.NoError(t, err)
	return st
}

// seedSitesAwaitingApply gives a Mutating_Operation a record worth
// protecting: whatever refuses it ran nothing, so the record must read the
// same afterwards.
func seedSitesAwaitingApply(t *testing.T, o *Orchestrator) prStatus {
	t.Helper()
	require.NoError(t, o.records.WriteOutcome(context.Background(), sitesRef, "helm-a",
		ProjectEntry{Outcome: OutcomeAwaitingApply, Operation: "diff", Tool: "helmfile"}))
	return readSitesRecord(t, o)
}

func planEntry(outcome Outcome) ProjectEntry {
	return ProjectEntry{Outcome: outcome, Operation: "diff", Tool: "helmfile"}
}

// requireOneCreated is the Project_Check a refusal created for a plan that
// had none, and that nothing updated afterwards.
func requireOneCreated(t *testing.T, client *sitesClient, target Target) github.CheckRunOptions {
	t.Helper()
	created, updated := client.projectChecks(target)
	require.Len(t, created, 1, "the refusal must create the Project_Check")
	assert.Empty(t, updated)
	assert.Equal(t, testPR.HeadSHA, created[0].HeadSHA)
	return created[0]
}

// requireCompletedByUpdate is a Project_Check that was created running and
// then completed by the refusal, rather than left in progress.
func requireCompletedByUpdate(t *testing.T, client *sitesClient, target Target) github.CheckRunOptions {
	t.Helper()
	created, updated := client.projectChecks(target)
	require.Len(t, created, 1)
	assert.Equal(t, "in_progress", created[0].Status, "precondition: the check was running")
	require.Len(t, updated, 1, "the refusal must complete the existing check")
	return updated[0]
}

func assertNoProjectCheck(t *testing.T, client *sitesClient, target Target) {
	t.Helper()
	created, updated := client.projectChecks(target)
	assert.Empty(t, created, "this refusal creates no Project_Check")
	assert.Empty(t, updated)
}

// :97 — a tool with no Plugin. Nothing can say which of its Operations is
// the plan, so it is neither given a check nor recorded here.
func TestRefusalSite_ToolNotSupported(t *testing.T) {
	o, jobsClient := sitesOrchestrator(t, &fakeLockManager{})
	client := newSitesClient()
	target := testHelmfileTarget()
	target.Project.Tool = "terraform"
	target.Operation = "plan"

	result := o.executeOne(context.Background(), client, testRepo, testPR, 1, target)

	assert.False(t, result.Success)
	assert.Contains(t, result.Output, `tool "terraform" is not supported`)
	assertNoProjectCheck(t, client, target)
	assert.Empty(t, readSitesRecord(t, o).Projects)
	assert.Zero(t, jobsClient.createCount())
}

// :104 — a Project asking for a ServiceAccount the Server does not permit.
func TestRefusalSite_ServiceAccountNotPermitted(t *testing.T) {
	o, jobsClient := sitesOrchestrator(t, &fakeLockManager{})
	client := newSitesClient()
	target := testHelmfileTarget()
	target.Project.Runner.ServiceAccount = "deployer"

	result := o.executeOne(context.Background(), client, testRepo, testPR, 1, target)

	assert.False(t, result.Success)
	check := requireOneCreated(t, client, target)
	assert.Equal(t, "completed", check.Status)
	assert.Equal(t, "failure", check.Conclusion)
	assert.Equal(t, "runner.serviceAccount is not permitted", check.Title)
	assert.Equal(t, notPermittedTitle(overrideServiceAccount), check.Title)
	assert.Contains(t, check.Summary, "TURNIP_ALLOWED_OVERRIDES",
		"the summary carries the full reason, including how to permit it")

	entry := planEntry(OutcomeRefused)
	entry.Setting = overrideServiceAccount
	assert.Equal(t, map[string]ProjectEntry{"helm-a": entry}, readSitesRecord(t, o).Projects)
	assert.Zero(t, jobsClient.createCount())
}

// :112 — a repository asking for a submodule mode the Server does not
// permit.
func TestRefusalSite_SubmodulesNotPermitted(t *testing.T) {
	o, jobsClient := sitesOrchestrator(t, &fakeLockManager{})
	client := newSitesClient()
	target := testHelmfileTarget()
	target.Clone = config.CloneSpec{Submodules: config.SubmodulesRecursive}

	result := o.executeOne(context.Background(), client, testRepo, testPR, 1, target)

	assert.False(t, result.Success)
	check := requireOneCreated(t, client, target)
	assert.Equal(t, "completed", check.Status)
	assert.Equal(t, "failure", check.Conclusion)
	assert.Equal(t, "clone.submodules is not permitted", check.Title)
	assert.Equal(t, notPermittedTitle(overrideCloneSubmodules), check.Title)
	assert.Contains(t, check.Summary, "TURNIP_ALLOWED_OVERRIDES")

	entry := planEntry(OutcomeRefused)
	entry.Setting = overrideCloneSubmodules
	assert.Equal(t, map[string]ProjectEntry{"helm-a": entry}, readSitesRecord(t, o).Projects)
	assert.Zero(t, jobsClient.createCount())
}

// :135 — a Mutating_Operation given arguments of its own.
func TestRefusalSite_MutatingWithArguments(t *testing.T) {
	o, jobsClient := sitesOrchestrator(t, &fakeLockManager{})
	before := seedSitesAwaitingApply(t, o)
	client := newSitesClient()
	target := testHelmfileTarget()
	target.Operation = "apply"
	target.ExtraArgs = []string{"-l", "name=web"}

	result := o.executeOne(context.Background(), client, testRepo, testPR, 1, target)

	assert.False(t, result.Success)
	assert.Contains(t, result.Output, "does not accept arguments")
	assertNoProjectCheck(t, client, target)
	assert.Equal(t, before, readSitesRecord(t, o), "a refused Mutating_Operation changes nothing")
	assert.Zero(t, jobsClient.createCount())
}

// :167 — acquiring the Lock failed: turnip's own failure, not the author's,
// so the check fails and the Project is left not planned rather than
// refused.
func TestRefusalSite_LockNotAcquired(t *testing.T) {
	o, jobsClient := sitesOrchestrator(t, &fakeLockManager{
		acquireForPlanFunc: func(context.Context, string, int, string, string) (bool, lock.Transition, error) {
			return false, lock.Transition{}, errors.New("redis unavailable")
		},
	})
	client := newSitesClient()
	target := testHelmfileTarget()

	result := o.executeOne(context.Background(), client, testRepo, testPR, 1, target)

	assert.False(t, result.Success)
	check := requireOneCreated(t, client, target)
	assert.Equal(t, "completed", check.Status)
	assert.Equal(t, "failure", check.Conclusion)
	assert.Equal(t, "lock could not be acquired", check.Title)
	assert.Equal(t, lockNotAcquiredTitle(), check.Title)
	assert.Contains(t, check.Summary, "redis unavailable")

	assert.Equal(t, map[string]ProjectEntry{"helm-a": planEntry(OutcomeNotPlanned)}, readSitesRecord(t, o).Projects)
	assert.Zero(t, jobsClient.createCount())
}

// lockWaitSite runs a plan against a Lock held elsewhere, with status
// deciding what the holder can be read as.
func lockWaitSite(t *testing.T, status func(context.Context, string) (*lock.LockStatus, error)) (github.ProjectResult, github.CheckRunOptions, prStatus) {
	t.Helper()
	o, jobsClient := sitesOrchestrator(t, &fakeLockManager{
		acquireForPlanFunc: func(context.Context, string, int, string, string) (bool, lock.Transition, error) {
			return false, lock.Transition{}, nil
		},
		getLockStatusFunc: status,
	})
	client := newSitesClient()
	target := testHelmfileTarget()

	result := o.executeOne(context.Background(), client, testRepo, testPR, 1, target)

	check := requireOneCreated(t, client, target)
	assert.Zero(t, jobsClient.createCount())
	return result, check, readSitesRecord(t, o)
}

// :175 — locked, and the holder could not be read.
func TestRefusalSite_LockWaitHolderUnknown(t *testing.T) {
	result, check, record := lockWaitSite(t, func(context.Context, string) (*lock.LockStatus, error) {
		return nil, errors.New("redis unavailable")
	})

	assert.False(t, result.Success)
	assert.Nil(t, result.BlockedBy, "no holder is invented")
	assert.Equal(t, "queued", check.Status)
	assert.Empty(t, check.Conclusion, "a Lock_Wait is waiting, not failed")
	assert.Equal(t, "locked by another pull request, re-plan once it's released", check.Title)
	assert.Equal(t, lockWaitTitle(0), check.Title)

	assert.Equal(t, map[string]ProjectEntry{"helm-a": planEntry(OutcomeNotPlanned)}, record.Projects)
}

// :175 again, for its other condition: the Lock reads as not held by
// anyone, so there is still no holder to name.
func TestRefusalSite_LockWaitHolderReleasedMeanwhile(t *testing.T) {
	_, check, record := lockWaitSite(t, func(context.Context, string) (*lock.LockStatus, error) {
		return &lock.LockStatus{Locked: false}, nil
	})

	assert.Equal(t, "queued", check.Status)
	assert.Equal(t, lockWaitTitle(0), check.Title)
	assert.Equal(t, map[string]ProjectEntry{"helm-a": planEntry(OutcomeNotPlanned)}, record.Projects)
}

// :177 — locked by a known pull request.
func TestRefusalSite_LockWaitByKnownPullRequest(t *testing.T) {
	result, check, record := lockWaitSite(t, func(context.Context, string) (*lock.LockStatus, error) {
		return &lock.LockStatus{Locked: true, PRNumber: 5, PullRequestURL: "https://github.com/owner/repo/pull/5"}, nil
	})

	assert.False(t, result.Success)
	require.NotNil(t, result.BlockedBy)
	assert.Equal(t, 5, result.BlockedBy.Number)
	assert.Equal(t, "queued", check.Status)
	assert.Empty(t, check.Conclusion)
	assert.Equal(t, "locked by PR #5, re-plan once it's released", check.Title)
	assert.Equal(t, lockWaitTitle(5), check.Title)

	entry := planEntry(OutcomeNotPlanned)
	entry.BlockedBy = 5
	assert.Equal(t, map[string]ProjectEntry{"helm-a": entry}, record.Projects)
}

// :175 and :177 are one kind: the check they create differs only where the
// blocking pull request is named — the Title, and the Summary that carries
// the comment's reason.
func TestRefusalSite_LockWaitSitesDifferOnlyInTheHolder(t *testing.T) {
	_, unknown, _ := lockWaitSite(t, func(context.Context, string) (*lock.LockStatus, error) {
		return nil, errors.New("redis unavailable")
	})
	_, known, _ := lockWaitSite(t, func(context.Context, string) (*lock.LockStatus, error) {
		return &lock.LockStatus{Locked: true, PRNumber: 5}, nil
	})

	assert.NotEqual(t, unknown.Title, known.Title)
	assert.Equal(t,
		strings.Replace(unknown.Title, "another pull request", "PR #5", 1), known.Title,
		"the Titles differ only in the blocking pull request")
	unknown.Title, known.Title = "", ""
	unknown.Summary, known.Summary = "", ""
	assert.Equal(t, unknown, known)
}

// :191–:221 — a Mutating_Operation with no usable plan behind it. Each
// condition is its own refusal; all of them are "other".
func TestRefusalSite_MutatingWithoutAUsablePlan(t *testing.T) {
	cases := []struct {
		name   string
		locks  *fakeLockManager
		reason string
	}{
		{
			name: "lock not held by this pull request",
			locks: &fakeLockManager{isLockedByPRFunc: func(context.Context, string, int) (bool, error) {
				return false, nil
			}},
			reason: "a new plan is required",
		},
		{
			name: "lock ownership unreadable",
			locks: &fakeLockManager{isLockedByPRFunc: func(context.Context, string, int) (bool, error) {
				return false, errors.New("redis unavailable")
			}},
			reason: "a new plan is required",
		},
		{
			name: "lock status unreadable",
			locks: &fakeLockManager{getLockStatusFunc: func(context.Context, string) (*lock.LockStatus, error) {
				return nil, errors.New("redis unavailable")
			}},
			reason: "reading lock",
		},
		{
			name: "stale plan",
			locks: &fakeLockManager{getLockStatusFunc: func(context.Context, string) (*lock.LockStatus, error) {
				return &lock.LockStatus{Locked: true, PRNumber: 42, State: lock.StatePlanStale}, nil
			}},
			reason: "no longer valid",
		},
		{
			name: "plan still running",
			locks: &fakeLockManager{getLockStatusFunc: func(context.Context, string) (*lock.LockStatus, error) {
				return &lock.LockStatus{Locked: true, PRNumber: 42, State: lock.StatePlanning}, nil
			}},
			reason: "no plan recorded",
		},
		{
			name: "no plan stored",
			locks: &fakeLockManager{getPlanFunc: func(context.Context, string, int) (lock.PlanRecord, error) {
				return lock.PlanRecord{}, lock.ErrNoPlan
			}},
			reason: "no plan recorded",
		},
		{
			name: "plan unreadable",
			locks: &fakeLockManager{getPlanFunc: func(context.Context, string, int) (lock.PlanRecord, error) {
				return lock.PlanRecord{}, errors.New("redis unavailable")
			}},
			reason: "retrieving plan",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o, jobsClient := sitesOrchestrator(t, tc.locks)
			before := seedSitesAwaitingApply(t, o)
			client := newSitesClient()
			target := testHelmfileTarget()
			target.Operation = "apply"

			result := o.executeOne(context.Background(), client, testRepo, testPR, 1, target)

			assert.False(t, result.Success)
			assert.Contains(t, result.Output, tc.reason)
			assertNoProjectCheck(t, client, target)
			assert.Equal(t, before, readSitesRecord(t, o), "a refused Mutating_Operation changes nothing")
			assert.Zero(t, jobsClient.createCount())
		})
	}
}

// failOperationRecordWrites fails only the write of an Operation Record, so
// the check run is created before it and the Pull_Request_Record can still
// be written after it — which a Redis failing everything would not allow.
type failOperationRecordWrites struct{}

func (failOperationRecordWrites) DialHook(next redis.DialHook) redis.DialHook { return next }

func (failOperationRecordWrites) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		args := cmd.Args()
		if strings.EqualFold(cmd.Name(), "set") && len(args) > 1 {
			if key, ok := args[1].(string); ok && strings.HasPrefix(key, operationKey("")) {
				err := errors.New("ERR injected: operation record write refused")
				cmd.SetErr(err)
				return err
			}
		}
		return next(ctx, cmd)
	}
}

func (failOperationRecordWrites) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}

// :278 — saving the Operation Record failed after the Project_Check was
// created: the check is completed as failed, and the plan left not
// planned.
func TestRefusalSite_OperationNotRecorded(t *testing.T) {
	jobsClient := &fakeJobCreator{t: t}
	o, redisClient := testOrchestrator(t, &fakeLockManager{}, jobsClient)
	redisClient.AddHook(failOperationRecordWrites{})
	client := newSitesClient()
	target := testHelmfileTarget()

	result := o.executeOne(context.Background(), client, testRepo, testPR, 1, target)

	assert.False(t, result.Success)
	assert.Contains(t, result.Output, "creating operation record")
	check := requireCompletedByUpdate(t, client, target)
	assert.Equal(t, "completed", check.Status)
	assert.Equal(t, "failure", check.Conclusion)
	assert.Equal(t, "operation could not be recorded", check.Title)
	assert.Equal(t, recordNotSavedTitle(), check.Title)

	assert.Equal(t, map[string]ProjectEntry{"helm-a": planEntry(OutcomeNotPlanned)}, readSitesRecord(t, o).Projects)
	assert.Zero(t, jobsClient.createCount())
}

// :313 — building the Runner Job failed. BuildJob rejects a tool version
// config validation would have refused at parse time; its own check is the
// one reachable failure of this step.
func TestRefusalSite_JobNotBuilt(t *testing.T) {
	o, jobsClient := sitesOrchestrator(t, &fakeLockManager{})
	client := newSitesClient()
	target := testHelmfileTarget()
	target.Project.ToolVersion = "not-a-version"

	result := o.executeOne(context.Background(), client, testRepo, testPR, 1, target)

	assert.False(t, result.Success)
	assert.Contains(t, result.Output, "building job")
	check := requireCompletedByUpdate(t, client, target)
	assert.Equal(t, "completed", check.Status)
	assert.Equal(t, "failure", check.Conclusion)
	assert.Equal(t, "Runner Job could not be built", check.Title)
	assert.Equal(t, jobNotBuiltTitle(), check.Title)

	assert.Equal(t, map[string]ProjectEntry{"helm-a": planEntry(OutcomeNotPlanned)}, readSitesRecord(t, o).Projects)
	assert.Zero(t, jobsClient.createCount())
}

// :327 — Kubernetes refused the Runner Job, for a plan.
func TestRefusalSite_JobNotCreated(t *testing.T) {
	o, jobsClient := sitesOrchestrator(t, &fakeLockManager{})
	jobsClient.createErr = errors.New("admission webhook denied the request")
	client := newSitesClient()
	target := testHelmfileTarget()

	result := o.executeOne(context.Background(), client, testRepo, testPR, 1, target)

	assert.False(t, result.Success)
	assert.Contains(t, result.Output, "creating job")
	check := requireCompletedByUpdate(t, client, target)
	assert.Equal(t, "completed", check.Status)
	assert.Equal(t, "failure", check.Conclusion)
	assert.Equal(t, "Runner Job could not be created", check.Title)
	assert.Equal(t, jobNotCreatedTitle(), check.Title)

	assert.Equal(t, map[string]ProjectEntry{"helm-a": planEntry(OutcomeNotPlanned)}, readSitesRecord(t, o).Projects)
}

// :327 for an apply: the check it created is completed the same way, and
// the record it never changed is left as it was.
func TestRefusalSite_ApplyJobNotCreated(t *testing.T) {
	o, jobsClient := sitesOrchestrator(t, &fakeLockManager{})
	jobsClient.createErr = errors.New("admission webhook denied the request")
	before := seedSitesAwaitingApply(t, o)
	client := newSitesClient()
	target := testHelmfileTarget()
	target.Operation = "apply"

	result := o.executeOne(context.Background(), client, testRepo, testPR, 1, target)

	assert.False(t, result.Success)
	check := requireCompletedByUpdate(t, client, target)
	assert.Equal(t, "completed", check.Status)
	assert.Equal(t, "failure", check.Conclusion)
	assert.Equal(t, jobNotCreatedTitle(), check.Title)
	assert.Equal(t, before, readSitesRecord(t, o), "a refused apply ran nothing, so the record must not change")
}

// cancelOnCreate creates the Job and then cancels the trigger's context
// without publishing a result, which is the one way to make the wait for
// the result fail without waiting out operationTTL.
type cancelOnCreate struct {
	fakeJobCreator
	cancel context.CancelFunc
}

func (c *cancelOnCreate) Create(_ context.Context, job *batchv1.Job) (*batchv1.Job, error) {
	c.mu.Lock()
	c.created++
	c.mu.Unlock()
	job.Name = "turnip-runner-canceled"
	c.cancel()
	return job, nil
}

// :337 — the Job exists, so HandleResult or the sweep owns the check and
// the record from here. The refusal must touch neither, or it could race
// them and overwrite the real outcome.
func TestRefusalSite_WaitingForResultFailed(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	jobsClient := &cancelOnCreate{cancel: cancel}
	o, _ := testOrchestrator(t, &fakeLockManager{}, jobsClient)
	client := newSitesClient()
	target := testHelmfileTarget()

	result := o.executeOne(ctx, client, testRepo, testPR, 1, target)

	assert.False(t, result.Success)
	assert.Contains(t, result.Output, "waiting for result")
	created, updated := client.projectChecks(target)
	require.Len(t, created, 1)
	assert.Equal(t, "in_progress", created[0].Status)
	assert.Empty(t, updated, "the dispatched Operation's check is not this refusal's to complete")
	assert.Empty(t, readSitesRecord(t, o).Projects, "nor is its Outcome this refusal's to record")
	assert.Equal(t, 1, jobsClient.createCount())
}

// No site today refuses after the check exists with anything but an
// Infrastructure_Error, so this calls refusalCheck directly: the invariant
// is Requirement 4.4's, not each site's, and a refusal added later that
// keeps the default kind — for an apply, which never gets a check created
// for it — must still complete the check rather than leave it in progress.
func TestRefusalCheck_CompletesAnExistingCheckWhateverTheKind(t *testing.T) {
	o, _ := sitesOrchestrator(t, &fakeLockManager{})
	target := testHelmfileTarget()
	target.Operation = "apply"

	cases := map[string]struct {
		r    refusal
		want string
	}{
		"other":         {refusal{reason: "something turnip checks later"}, notStartedTitle()},
		"lock wait":     {refusal{kind: refusalLockWait, reason: "locked", blockedBy: 5}, notStartedTitle()},
		"configuration": {refusal{kind: refusalConfiguration, reason: "not permitted", setting: overrideServiceAccount}, notPermittedTitle(overrideServiceAccount)},
		"titled":        {refusal{kind: refusalInfrastructure, reason: "boom", title: jobNotBuiltTitle()}, jobNotBuiltTitle()},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			client := newSitesClient()

			err := o.refusalCheck(context.Background(), client, testRepo, testPR, target, false, 555, tc.r)

			require.NoError(t, err)
			created, updated := client.projectChecks(target)
			assert.Empty(t, created, "a check that exists is completed, never created again")
			require.Len(t, updated, 1, "the existing check must not be left in progress")
			assert.Equal(t, "completed", updated[0].Status)
			assert.Equal(t, "failure", updated[0].Conclusion)
			assert.Equal(t, tc.want, updated[0].Title)
			assert.Equal(t, tc.r.reason, updated[0].Summary)
		})
	}
}

// A later plan replaces the queued check with a new run of its own rather
// than updating it: turnip keeps no record of the queued run
// (check-run-refusals Requirement 2).
func TestRefusalSite_LaterPlanReplacesTheQueuedCheck(t *testing.T) {
	held := true
	o, _ := sitesOrchestrator(t, &fakeLockManager{
		acquireForPlanFunc: func(context.Context, string, int, string, string) (bool, lock.Transition, error) {
			if held {
				return false, lock.Transition{}, nil
			}
			return true, lock.Transition{To: lock.StatePlanning}, nil
		},
		getLockStatusFunc: func(context.Context, string) (*lock.LockStatus, error) {
			return &lock.LockStatus{Locked: true, PRNumber: 5}, nil
		},
	})
	client := newSitesClient()
	target := testHelmfileTarget()

	o.executeOne(context.Background(), client, testRepo, testPR, 1, target)
	held = false // PR #5 released the Lock; someone re-plans
	result := o.executeOne(context.Background(), client, testRepo, testPR, 1, target)
	require.True(t, result.Success)

	created, updated := client.projectChecks(target)
	require.Len(t, created, 2, "the re-plan creates a check of its own")
	assert.Equal(t, "queued", created[0].Status)
	assert.Equal(t, "in_progress", created[1].Status)
	assert.Equal(t, runningTitle(nil), created[1].Title)
	assert.Empty(t, updated, "the queued run is never updated")
}

// countingCommentClient counts check runs, so a refusal of the whole
// trigger can be shown to create none.
type countingCommentClient struct {
	*fakeCommentEventClient

	mu      sync.Mutex
	created int
}

func (c *countingCommentClient) CreateCheckRun(ctx context.Context, owner, repo string, opts github.CheckRunOptions) (int64, error) {
	c.mu.Lock()
	c.created++
	c.mu.Unlock()
	return c.fakeCommentEventClient.CreateCheckRun(ctx, owner, repo, opts)
}

func (c *countingCommentClient) createdCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.created
}

// Refusals of the whole trigger return before any Target exists, so they
// create no check run — the one this slice adds for a Lock_Wait included.
// Check runs attach to a commit, and a closed pull request's head may
// already be in the base branch's history (check-run-refusals
// Requirement 6). The Lock is held elsewhere, so a Target that did get
// through would leave a queued check behind.
func TestWholeTriggerRefusals_CreateNoCheckRun(t *testing.T) {
	notCollaborator := false
	cases := map[string]*fakeCommentEventClient{
		"a closed pull request": {
			permission: "write",
			files:      map[string][]byte{"turnip.yaml": []byte(validTurnipYAML)},
			pr:         closedPR(),
		},
		"a commenter without access": {
			collaborator: &notCollaborator,
			files:        map[string][]byte{"turnip.yaml": []byte(validTurnipYAML)},
			pr:           openPR(),
		},
	}
	for name, fake := range cases {
		t.Run(name, func(t *testing.T) {
			o := testCommentOrchestrator(t, &fakeLockManager{
				acquireForPlanFunc: func(context.Context, string, int, string, string) (bool, lock.Transition, error) {
					return false, lock.Transition{}, nil
				},
			})
			client := &countingCommentClient{fakeCommentEventClient: fake}

			_ = callHandleIssueComment(o, client, commentEvent("/turnip diff helm-a", "alice"))
			// The trigger's work, if any got through, runs on a detached
			// goroutine; give it the time it would need.
			time.Sleep(200 * time.Millisecond)

			assert.Zero(t, client.createdCount())
		})
	}

	// The control: the same setup on an open pull request from a
	// collaborator does create the queued check, so the zero above is the
	// refusal's doing and not the fixture's.
	t.Run("control: an open pull request", func(t *testing.T) {
		o := testCommentOrchestrator(t, &fakeLockManager{
			acquireForPlanFunc: func(context.Context, string, int, string, string) (bool, lock.Transition, error) {
				return false, lock.Transition{}, nil
			},
		})
		client := &countingCommentClient{fakeCommentEventClient: &fakeCommentEventClient{
			permission: "write",
			files:      map[string][]byte{"turnip.yaml": []byte(validTurnipYAML)},
			pr:         openPR(),
		}}

		_ = callHandleIssueComment(o, client, commentEvent("/turnip diff helm-a", "alice"))

		assert.Eventually(t, func() bool { return client.createdCount() > 0 }, 2*time.Second, 10*time.Millisecond)
	})
}
