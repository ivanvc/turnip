package orchestrator

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivanvc/turnip/internal/config"
	"github.com/ivanvc/turnip/internal/github"
	"github.com/ivanvc/turnip/internal/lock"
	"github.com/ivanvc/turnip/internal/rpc"
)

func testResultOrchestrator(t *testing.T, locks *fakeLockManager) (*Orchestrator, *fakeExecuteClient) {
	t.Helper()
	client := newTestRedisClient(t)
	fakeClient := &fakeExecuteClient{}
	o := &Orchestrator{
		installationClient: func(id int64) github.GitHubClient { return fakeClient },
		locks:              locks,
		plugins:            testRegistry(),
		records:            newRecordStore(client),
		redis:              client,
	}
	return o, fakeClient
}

func createTestRecord(t *testing.T, o *Orchestrator, operationID, operation string) {
	t.Helper()
	require.NoError(t, o.records.Create(context.Background(), &OperationRecord{
		OperationID:   operationID,
		ProjectKey:    "owner/repo/helm-a",
		Project:       config.Project{Name: "helm-a", Tool: "helmfile"},
		Owner:         "owner",
		Repo:          "repo",
		PRNumber:      42,
		Operation:     operation,
		CheckRunID:    555,
		StartDeadline: time.Now().Add(5 * time.Minute).Unix(),
		CreatedAt:     time.Now(),
	}))
}

func TestHandleLog_MarksStarted(t *testing.T) {
	o, _ := testResultOrchestrator(t, &fakeLockManager{})
	createTestRecord(t, o, "op-1", "diff")

	before := scrapeMetric(t, "turnip_runner_job_start_latency_seconds", nil)

	require.NoError(t, o.HandleLog(context.Background(), "op-1", rpc.LogLine{Message: "hi"}))

	rec, err := o.records.get(context.Background(), "op-1")
	require.NoError(t, err)
	assert.True(t, rec.Started)
	assert.InDelta(t, before+1, scrapeMetric(t, "turnip_runner_job_start_latency_seconds", nil), 0.0001)

	// A second HandleLog for the same operation is a no-op transition
	// (Decision 2) — the histogram must not be observed again.
	require.NoError(t, o.HandleLog(context.Background(), "op-1", rpc.LogLine{Message: "again"}))
	assert.InDelta(t, before+1, scrapeMetric(t, "turnip_runner_job_start_latency_seconds", nil), 0.0001)
}

// publishedResult runs HandleResult and returns the ProjectResult it
// published, which is what reaches the pull request comment. The
// subscription is established before the handler runs because Pub/Sub has
// no replay.
func publishedResult(t *testing.T, o *Orchestrator, operationID string, result rpc.OperationResult) github.ProjectResult {
	t.Helper()
	ctx := context.Background()

	resultCh := make(chan github.ProjectResult, 1)
	go func() {
		r, _ := waitForDone(ctx, o.redis, operationID)
		resultCh <- r
	}()

	require.Eventually(t, func() bool {
		return o.redis.PubSubNumSub(ctx, doneChannel(operationID)).Val()[doneChannel(operationID)] > 0
	}, time.Second, time.Millisecond)

	require.NoError(t, o.HandleResult(ctx, operationID, result))

	select {
	case got := <-resultCh:
		return got
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the published result")
		return github.ProjectResult{}
	}
}

// The counts the Runner reported, and the Plugin's operation names, must
// reach the result — the renderer has no Plugin registry to resolve names
// itself, and re-parsing the output for counts would duplicate work the
// Runner already did.
func TestHandleResult_CarriesChangeCountsAndResolvedOperationNames(t *testing.T) {
	o, _ := testResultOrchestrator(t, &fakeLockManager{})
	createTestRecord(t, o, "op-counts", "diff")

	got := publishedResult(t, o, "op-counts", rpc.OperationResult{
		Success:  true,
		PlanData: []byte("plan-data"),
		Changes:  rpc.ChangeSummary{Add: 1, Change: 4, Destroy: 2},
	})

	assert.Equal(t, github.ChangeCounts{Add: 1, Change: 4, Destroy: 2}, got.Changes)
	assert.Equal(t, "diff", got.PlanOperation, "resolved from the Project's Plugin")
	assert.Equal(t, "apply", got.ApplyOperation)
}

// Locked reports the state after the result was handled, rather than being
// inferred from Success — which is what lets Slice 18 change whether a
// failed plan keeps its Lock without this code or the renderer changing.
func TestHandleResult_LockStateFollowsWhatActuallyHappened(t *testing.T) {
	t.Run("successful plan holds its lock", func(t *testing.T) {
		o, _ := testResultOrchestrator(t, &fakeLockManager{})
		createTestRecord(t, o, "op-plan", "diff")

		got := publishedResult(t, o, "op-plan", rpc.OperationResult{
			Success: true, PlanData: []byte("plan-data"),
		})

		assert.True(t, got.Locked)
	})

	t.Run("successful apply releases it", func(t *testing.T) {
		o, _ := testResultOrchestrator(t, &fakeLockManager{})
		createTestRecord(t, o, "op-apply", "apply")

		got := publishedResult(t, o, "op-apply", rpc.OperationResult{Success: true})

		assert.False(t, got.Locked, "a released lock must not be offered for unlocking")
	})

	t.Run("failed apply keeps it", func(t *testing.T) {
		o, _ := testResultOrchestrator(t, &fakeLockManager{})
		createTestRecord(t, o, "op-failed-apply", "apply")

		got := publishedResult(t, o, "op-failed-apply", rpc.OperationResult{Success: false})

		assert.True(t, got.Locked, "an apply that may have mutated infrastructure keeps its lock")
	})
}

// A Helmfile plan produces no artifact, so while the storage condition
// tested len(result.PlanData) > 0 nothing was ever recorded for it — and
// every apply that followed was refused as though no plan had run. The
// fact of a successful plan must be recorded whatever the tool returned.
func TestHandleResult_HelmfilePlanWithNoArtifactStillRecordsAPlan(t *testing.T) {
	var stored *lock.PlanRecord
	locks := &fakeLockManager{
		storePlanFunc: func(ctx context.Context, projectKey string, prNumber int, plan lock.PlanRecord) error {
			stored = &plan
			return nil
		},
	}
	o, _ := testResultOrchestrator(t, locks)
	createTestRecord(t, o, "op-helmfile-plan", "diff")

	require.NoError(t, o.HandleResult(context.Background(), "op-helmfile-plan", rpc.OperationResult{
		Success:  true,
		PlanData: nil,
		Changes:  rpc.ChangeSummary{Change: 4},
	}))

	require.NotNil(t, stored, "a successful plan must be recorded even with no artifact")
	assert.Empty(t, stored.Data, "Helmfile produces none, and that is not the same as no plan")
	assert.Equal(t, 4, stored.Summary.Change)
}

// Releasing is not apply-specific: a successful sync mutates real
// infrastructure and must give the Lock back too. Narrowing this to the
// apply alone is the regression worth guarding — it reads like the rule,
// and it strands a Project with no route back but a manual unlock.
func TestHandleResult_SuccessfulSyncReleasesTheLock(t *testing.T) {
	var released bool
	locks := &fakeLockManager{
		releaseLockFunc: func(ctx context.Context, projectKey string, prNumber int) error {
			released = true
			return nil
		},
	}
	o, _ := testResultOrchestrator(t, locks)
	createTestRecord(t, o, "op-sync", "sync")

	got := publishedResult(t, o, "op-sync", rpc.OperationResult{Success: true})

	assert.True(t, released, "a successful sync must release its Lock")
	assert.False(t, got.Locked, "a released lock must not be offered for unlocking")
}

// Requirement 1.2: the arguments recorded are the ones the Operation ran
// with, read from the record the Job was built from rather than
// re-derived from the trigger line — so whatever turnip normalized is
// what a later mutating Operation replays.
func TestHandleResult_RecordsTheOperationsOwnArguments(t *testing.T) {
	var stored *lock.PlanRecord
	locks := &fakeLockManager{
		storePlanFunc: func(ctx context.Context, projectKey string, prNumber int, plan lock.PlanRecord) error {
			stored = &plan
			return nil
		},
	}
	o, _ := testResultOrchestrator(t, locks)
	require.NoError(t, o.records.Create(context.Background(), &OperationRecord{
		OperationID:   "op-args",
		ProjectKey:    "owner/repo/helm-a",
		Project:       config.Project{Name: "helm-a", Tool: "helmfile"},
		Owner:         "owner",
		Repo:          "repo",
		PRNumber:      42,
		Operation:     "diff",
		ExtraArgs:     []string{"-l", "name=web"},
		CheckRunID:    555,
		StartDeadline: time.Now().Add(5 * time.Minute).Unix(),
		CreatedAt:     time.Now(),
	}))

	require.NoError(t, o.HandleResult(context.Background(), "op-args", rpc.OperationResult{Success: true}))

	require.NotNil(t, stored)
	assert.Equal(t, []string{"-l", "name=web"}, stored.Args)
}

func TestHandleResult_UnclaimableIsNoop(t *testing.T) {
	o, _ := testResultOrchestrator(t, &fakeLockManager{})
	// No record created at all.
	require.NoError(t, o.HandleResult(context.Background(), "does-not-exist", rpc.OperationResult{Success: true}))
}

func TestHandleResult_SuccessfulPlan_StoresPlanNotReleasesLock(t *testing.T) {
	var stored bool
	var released bool
	locks := &fakeLockManager{
		storePlanFunc: func(ctx context.Context, projectKey string, prNumber int, plan lock.PlanRecord) error {
			stored = true
			assert.Equal(t, []byte("plan-data"), plan.Data)
			return nil
		},
		releaseLockFunc: func(ctx context.Context, projectKey string, prNumber int) error {
			released = true
			return nil
		},
	}
	o, client := testResultOrchestrator(t, locks)
	createTestRecord(t, o, "op-1", "diff")

	err := o.HandleResult(context.Background(), "op-1", rpc.OperationResult{
		Success:  true,
		PlanData: []byte("plan-data"),
	})
	require.NoError(t, err)
	assert.True(t, stored)
	assert.False(t, released)
	assert.Equal(t, 1, client.updateCheckRunCalled)
}

func TestHandleResult_SuccessfulApply_ReleasesLockAndAppendsNote(t *testing.T) {
	var released bool
	locks := &fakeLockManager{
		releaseLockFunc: func(ctx context.Context, projectKey string, prNumber int) error {
			released = true
			return nil
		},
	}
	o, _ := testResultOrchestrator(t, locks)
	createTestRecord(t, o, "op-1", "apply")

	resultCh := make(chan github.ProjectResult, 1)
	go func() {
		r, err := waitForDone(context.Background(), o.redis, "op-1")
		assert.NoError(t, err)
		resultCh <- r
	}()
	require.Eventually(t, func() bool {
		return o.redis.PubSubNumSub(context.Background(), doneChannel("op-1")).Val()[doneChannel("op-1")] > 0
	}, time.Second, time.Millisecond)

	require.NoError(t, o.HandleResult(context.Background(), "op-1", rpc.OperationResult{Success: true, Output: "applied"}))
	assert.True(t, released)

	select {
	case r := <-resultCh:
		// The note moved off Output, which renders inside the tool's own
		// code fence and is what gets split when output is long, onto a
		// field the renderer places in the trailer beside the next steps.
		assert.Contains(t, r.LockNote, "Lock released")
		assert.NotContains(t, r.Output, "Lock released", "turnip's voice does not belong inside the tool's output")
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for published result")
	}
}

func TestHandleResult_FailedApply_DoesNotReleaseLock(t *testing.T) {
	var released bool
	locks := &fakeLockManager{
		releaseLockFunc: func(ctx context.Context, projectKey string, prNumber int) error {
			released = true
			return nil
		},
	}
	o, _ := testResultOrchestrator(t, locks)
	createTestRecord(t, o, "op-1", "apply")

	require.NoError(t, o.HandleResult(context.Background(), "op-1", rpc.OperationResult{Success: false, Output: "failed"}))
	assert.False(t, released)
}

func TestHandleResult_DeletesRecordAfterFinalizing(t *testing.T) {
	o, _ := testResultOrchestrator(t, &fakeLockManager{})
	createTestRecord(t, o, "op-1", "diff")

	require.NoError(t, o.HandleResult(context.Background(), "op-1", rpc.OperationResult{Success: false}))

	rec, err := o.records.get(context.Background(), "op-1")
	require.NoError(t, err)
	assert.Nil(t, rec)
}

// The check run's name is its stable identity — required status checks
// and branch protection match on it — so an Operation's outcome is
// carried by the title and summary, which is what the checks tab renders
// beneath the name.
func TestCheckRunResultTitle(t *testing.T) {
	assert.Equal(t, "success", checkRunResultTitle(true))
	assert.Equal(t, "failure", checkRunResultTitle(false))
}

func TestCheckRunResultSummary(t *testing.T) {
	assert.Equal(t, "add: 1, change: 2, destroy: 0", checkRunResultSummary(true, "add: 1, change: 2, destroy: 0"))
	assert.Equal(t, "No changes.", checkRunResultSummary(true, ""))
	assert.Contains(t, checkRunResultSummary(false, "add: 0, change: 2, destroy: 0"), "failed")
	assert.Contains(t, checkRunResultSummary(false, "add: 0, change: 2, destroy: 0"), "change: 2")
	assert.Contains(t, checkRunResultSummary(false, ""), "failed")
	assert.NotEmpty(t, checkRunResultSummary(false, ""), "summary is required by GitHub whenever output is sent")
}

// The checks tab renders the title (and the start of the summary) under
// the check run's name, so that is where an Operation's outcome belongs.
// The name itself must not vary: required status checks match on it.
func TestHandleResult_SuccessCheckRunCarriesOutcomeInTitleAndSummary(t *testing.T) {
	o, client := testResultOrchestrator(t, &fakeLockManager{})
	createTestRecord(t, o, "op-1", "diff")

	require.NoError(t, o.HandleResult(context.Background(), "op-1", rpc.OperationResult{
		Success: true,
		Changes: rpc.ChangeSummary{Change: 2},
	}))

	assert.Equal(t, "success", client.updatedCheckRun.Title)
	assert.Contains(t, client.updatedCheckRun.Summary, "change: 2")
	assert.Contains(t, client.updatedCheckRun.Name, "diff", "the name stays the stable identity")
}

func TestHandleResult_FailureCheckRunTitleSaysFailure(t *testing.T) {
	o, client := testResultOrchestrator(t, &fakeLockManager{})
	createTestRecord(t, o, "op-2", "diff")

	require.NoError(t, o.HandleResult(context.Background(), "op-2", rpc.OperationResult{
		Success: false,
		Output:  "helmfile diff exited 1",
	}))

	assert.Equal(t, "failure", client.updatedCheckRun.Title)
	assert.Contains(t, client.updatedCheckRun.Summary, "failed")
	assert.Contains(t, client.updatedCheckRun.Text, "helmfile diff exited 1")
}
