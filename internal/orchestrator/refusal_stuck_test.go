package orchestrator

import (
	"context"
	"errors"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivanvc/turnip/internal/config"
	"github.com/ivanvc/turnip/internal/github"
	"github.com/ivanvc/turnip/internal/jobs"
	"github.com/ivanvc/turnip/internal/lock"
)

// hookedCheckClient runs afterCreate once the Project_Check exists. That is
// the only moment a failure can be injected that proves the point: before
// it there is no check to leave stuck, and after the record is saved the
// step under test has already succeeded.
type hookedCheckClient struct {
	*fakeExecuteClient
	afterCreate func()
}

func (h *hookedCheckClient) CreateCheckRun(ctx context.Context, owner, repo string, opts github.CheckRunOptions) (int64, error) {
	id, err := h.fakeExecuteClient.CreateCheckRun(ctx, owner, repo, opts)
	if h.afterCreate != nil {
		h.afterCreate()
	}
	return id, err
}

// stuckCheckOrchestrator is testOrchestrator with the miniredis in hand, so
// a test can make Redis fail between the check being created and the
// Operation Record being saved, then recover it to read the record back.
func stuckCheckOrchestrator(t *testing.T, locks lock.LockManager) (*Orchestrator, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr(), MaxRetries: -1})
	t.Cleanup(func() { _ = client.Close() })
	o := &Orchestrator{
		locks:            locks,
		jobs:             &fakeJobCreator{t: t, redis: client},
		plugins:          testRegistry(),
		records:          newRecordStore(client),
		redis:            client,
		allowedOverrides: defaultAllowedOverrides(),
		startTimeout:     5 * time.Minute,
		sweepInterval:    30 * time.Second,
	}
	return o, mr
}

var stuckCheckRef = prRef{Owner: "owner", Repo: "repo", PRNumber: 42, HeadSHA: "abc123"}

// applyLocks has a recorded plan, so an apply gets as far as creating its
// Project_Check.
func applyLocks() *fakeLockManager {
	return &fakeLockManager{
		getPlanFunc: func(ctx context.Context, projectKey string, prNumber int) (lock.PlanRecord, error) {
			return lock.PlanRecord{Data: []byte("plan-data")}, nil
		},
	}
}

// seedAwaitingApply gives the apply cases a record worth protecting: the
// plan they replay said there was something to apply, and a refused apply
// ran nothing, so that must still be what the record says afterwards.
func seedAwaitingApply(t *testing.T, o *Orchestrator) prStatus {
	t.Helper()
	require.NoError(t, o.records.WriteOutcome(context.Background(), stuckCheckRef, "helm-a",
		ProjectEntry{Outcome: OutcomeAwaitingApply, Operation: "diff"}))
	before, err := o.records.ReadPRStatus(context.Background(), stuckCheckRef)
	require.NoError(t, err)
	return before
}

func assertCheckCompletedAsFailure(t *testing.T, client *fakeExecuteClient) {
	t.Helper()
	require.Equal(t, "in_progress", client.createdCheckRun.Status,
		"precondition: the Project_Check was created before the failure")
	assert.Equal(t, 1, client.updateCheckRunCalled,
		"the Project_Check must be completed, not left in progress")
	assert.Equal(t, "completed", client.updatedCheckRun.Status)
	assert.Equal(t, "failure", client.updatedCheckRun.Conclusion)
}

// Requirement 4.4: a plan whose Operation Record cannot be saved completes
// the Project_Check it already created.
func TestExecuteOne_PlanRecordNotSavedCompletesTheCheck(t *testing.T) {
	o, mr := stuckCheckOrchestrator(t, &fakeLockManager{})
	client := &fakeExecuteClient{}
	hooked := &hookedCheckClient{fakeExecuteClient: client, afterCreate: func() { mr.SetError("ERR injected: redis unavailable") }}

	result := o.executeOne(context.Background(), hooked, testRepo, testPR, 1, testHelmfileTarget())
	mr.SetError("")

	assert.False(t, result.Success)
	assert.Contains(t, result.Output, "creating operation record")
	assertCheckCompletedAsFailure(t, client)
}

// Requirement 4.4: the same for an apply, which creates its Project_Check
// just as a plan does, and whose refusal leaves the Pull_Request_Record as
// it was.
func TestExecuteOne_ApplyRecordNotSavedCompletesTheCheck(t *testing.T) {
	o, mr := stuckCheckOrchestrator(t, applyLocks())
	before := seedAwaitingApply(t, o)
	client := &fakeExecuteClient{}
	hooked := &hookedCheckClient{fakeExecuteClient: client, afterCreate: func() { mr.SetError("ERR injected: redis unavailable") }}

	target := testHelmfileTarget()
	target.Operation = "apply"
	result := o.executeOne(context.Background(), hooked, testRepo, testPR, 1, target)
	mr.SetError("")

	assert.False(t, result.Success)
	assert.Contains(t, result.Output, "creating operation record")
	assertCheckCompletedAsFailure(t, client)

	after, err := o.records.ReadPRStatus(context.Background(), stuckCheckRef)
	require.NoError(t, err)
	assert.Equal(t, before, after, "a refused apply ran nothing, so the record must not change")
}

// failBuildJob makes the build step fail. No input reaches a failure of
// jobs.BuildJob itself any more, so the step is replaced rather than fed
// something it refuses.
func failBuildJob(o *Orchestrator) {
	o.buildJob = func(config.Project, jobs.OperationParams) (*batchv1.Job, error) {
		return nil, errors.New("jobs: marshal tool config: unsupported value")
	}
}

func unbuildableTarget(operation string) Target {
	t := testHelmfileTarget()
	t.Operation = operation
	return t
}

// Requirement 4.4: a plan whose Runner Job cannot be built completes the
// Project_Check it already created.
func TestExecuteOne_PlanJobNotBuiltCompletesTheCheck(t *testing.T) {
	o, _ := stuckCheckOrchestrator(t, &fakeLockManager{})
	failBuildJob(o)
	client := &fakeExecuteClient{}

	result := o.executeOne(context.Background(), client, testRepo, testPR, 1, unbuildableTarget("diff"))

	assert.False(t, result.Success)
	assert.Contains(t, result.Output, "building job")
	assertCheckCompletedAsFailure(t, client)
}

// Requirement 4.4: the same for an apply, leaving the Pull_Request_Record
// as it was.
func TestExecuteOne_ApplyJobNotBuiltCompletesTheCheck(t *testing.T) {
	o, _ := stuckCheckOrchestrator(t, applyLocks())
	failBuildJob(o)
	before := seedAwaitingApply(t, o)
	client := &fakeExecuteClient{}

	result := o.executeOne(context.Background(), client, testRepo, testPR, 1, unbuildableTarget("apply"))

	assert.False(t, result.Success)
	assert.Contains(t, result.Output, "building job")
	assertCheckCompletedAsFailure(t, client)

	after, err := o.records.ReadPRStatus(context.Background(), stuckCheckRef)
	require.NoError(t, err)
	assert.Equal(t, before, after, "a refused apply ran nothing, so the record must not change")
}
