package orchestrator

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivanvc/turnip/internal/github"
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
		ProjectEntry{Outcome: OutcomeAwaitingApply, Operation: "diff", Tool: "helmfile"}))
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

// unbuildableTarget pins a tool version jobs.BuildJob refuses. Config
// validation rejects it at parse time in production, but BuildJob checks
// again, and that check is the one reachable failure of the build step.
func unbuildableTarget(operation string) Target {
	t := testHelmfileTarget()
	t.Operation = operation
	t.Project.ToolVersion = "not-a-version"
	return t
}

// Requirement 4.4: a plan whose Runner Job cannot be built completes the
// Project_Check it already created.
func TestExecuteOne_PlanJobNotBuiltCompletesTheCheck(t *testing.T) {
	o, _ := stuckCheckOrchestrator(t, &fakeLockManager{})
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
