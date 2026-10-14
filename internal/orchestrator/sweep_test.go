package orchestrator

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivanvc/turnip/internal/config"
	"github.com/ivanvc/turnip/internal/github"
	"github.com/ivanvc/turnip/internal/jobs"
	"github.com/ivanvc/turnip/internal/lock"
	"github.com/ivanvc/turnip/internal/rpc"
)

func testSweepOrchestrator(t *testing.T, jobsClient jobCreator) (*Orchestrator, *fakeExecuteClient) {
	t.Helper()
	client := newTestRedisClient(t)
	fakeClient := &fakeExecuteClient{}
	o := &Orchestrator{
		installationClient: func(id int64) github.GitHubClient { return fakeClient },
		jobs:               jobsClient,
		plugins:            testRegistry(),
		catalog:            testCatalog(),
		records:            newRecordStore(client),
		triggers:           newTriggerStore(client),
		redis:              client,
		// The sweep now applies a transition, so it has a Lock dependency
		// it did not have when a timeout mutated nothing.
		locks: &fakeLockManager{},
	}
	return o, fakeClient
}

func sweepTestRecord(operationID string, deadline time.Time, started bool) *OperationRecord {
	return &OperationRecord{
		OperationID:   operationID,
		ProjectKey:    "owner/repo/helm-a",
		Project:       config.Project{Name: "helm-a", Tool: "helmfile"},
		Owner:         "owner",
		Repo:          "repo",
		PRNumber:      42,
		Operation:     "diff",
		JobName:       "turnip-runner-" + operationID,
		CheckRunID:    555,
		StartDeadline: deadline.Unix(),
		Started:       started,
		CreatedAt:     time.Now(),
	}
}

func TestSweepOnce_ClaimsExpiredUnstartedRecord(t *testing.T) {
	statusCalled := false
	jobsClient := &fakeJobCreator{t: t, statusFn: func(ctx context.Context, jobName string) (*jobs.JobStatus, error) {
		statusCalled = true
		return &jobs.JobStatus{JobFound: true, PodPhase: "Pending"}, nil
	}}
	o, client := testSweepOrchestrator(t, jobsClient)
	require.NoError(t, o.records.Create(context.Background(), sweepTestRecord("op-1", time.Now().Add(-time.Minute), false)))

	r := finalizedResult(t, o, "op-1", func() {
		o.sweepOnce(context.Background())
	})

	assert.True(t, statusCalled)
	assert.Equal(t, 1, client.updateCheckRunCalled)
	assert.False(t, r.Success)
	assert.Contains(t, r.Output, "Pending")

	rec, err := o.records.get(context.Background(), "op-1")
	require.NoError(t, err)
	assert.Nil(t, rec)
}

func TestSweepOnce_LeavesNotYetExpiredRecordUntouched(t *testing.T) {
	o, client := testSweepOrchestrator(t, &fakeJobCreator{t: t})
	require.NoError(t, o.records.Create(context.Background(), sweepTestRecord("op-1", time.Now().Add(5*time.Minute), false)))

	o.sweepOnce(context.Background())

	assert.Equal(t, 0, client.updateCheckRunCalled)
	rec, err := o.records.get(context.Background(), "op-1")
	require.NoError(t, err)
	assert.NotNil(t, rec, "an unexpired record must not be claimed")
}

func TestSweepOnce_LeavesStartedRecordUntouched(t *testing.T) {
	o, client := testSweepOrchestrator(t, &fakeJobCreator{t: t})
	require.NoError(t, o.records.Create(context.Background(), sweepTestRecord("op-1", time.Now().Add(-time.Minute), true)))

	o.sweepOnce(context.Background())

	assert.Equal(t, 0, client.updateCheckRunCalled)
	rec, err := o.records.get(context.Background(), "op-1")
	require.NoError(t, err)
	assert.NotNil(t, rec, "a started record must not be claimed as a timeout")
}

// neverReleaseLocks is a fakeLockManager that fails the test if
// ReleaseLock is ever called — sweepOnce must never release a Lock
// (Requirement 6.8/8.5).
type neverReleaseLocks struct {
	fakeLockManager
	t *testing.T
}

func (n *neverReleaseLocks) ReleaseLock(ctx context.Context, projectKey string, prNumber int) error {
	n.t.Fatal("sweepOnce must never call ReleaseLock")
	return nil
}

// TestSweepOnce_NeverReleasesLock asserts the one behavioral guarantee
// worth a runtime test: sweepOnce must never call ReleaseLock
// (Requirement 6.8/8.5). Job deletion needs no equivalent test — the
// jobCreator interface sweep.go depends on has no Delete method at all,
// so "sweepOnce never deletes the Job" is already guaranteed at compile
// time, not just at runtime.
func TestSweepOnce_NeverReleasesLock(t *testing.T) {
	o, _ := testSweepOrchestrator(t, &fakeJobCreator{t: t})
	o.locks = &neverReleaseLocks{t: t}
	require.NoError(t, o.records.Create(context.Background(), sweepTestRecord("op-1", time.Now().Add(-time.Minute), false)))

	o.sweepOnce(context.Background())
}

func TestRun_SweepsPeriodicallyAndExitsOnCancel(t *testing.T) {
	o, _ := testSweepOrchestrator(t, &fakeJobCreator{t: t})
	o.sweepInterval = 5 * time.Millisecond
	require.NoError(t, o.records.Create(context.Background(), sweepTestRecord("op-1", time.Now().Add(-time.Minute), false)))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- o.Run(ctx) }()

	require.Eventually(t, func() bool {
		rec, err := o.records.get(context.Background(), "op-1")
		return err == nil && rec == nil
	}, time.Second, 5*time.Millisecond, "Run should have swept the expired record away")

	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("Run did not exit after context cancellation")
	}
}

func TestSweepOnce_TimeoutCheckRunTitleIsTheDiagnostic(t *testing.T) {
	jobsClient := &fakeJobCreator{t: t, statusFn: func(ctx context.Context, jobName string) (*jobs.JobStatus, error) {
		return &jobs.JobStatus{JobFound: true, PodPhase: "Pending"}, nil
	}}
	o, client := testSweepOrchestrator(t, jobsClient)
	require.NoError(t, o.records.Create(context.Background(), sweepTestRecord("op-1", time.Now().Add(-time.Minute), false)))

	o.sweepOnce(context.Background())

	assert.Contains(t, client.updatedCheckRun.Title, "Pod still Pending")
	assert.NotEmpty(t, client.updatedCheckRun.Summary, "GitHub rejects check-run output without a summary")
	assert.Equal(t, "The Runner never reported back within the start timeout.", client.updatedCheckRun.Summary)
}

func TestSweepOnce_TimeoutCompletionFailureIsNotedAsInProgress(t *testing.T) {
	jobsClient := &fakeJobCreator{t: t, statusFn: func(ctx context.Context, jobName string) (*jobs.JobStatus, error) {
		return &jobs.JobStatus{JobFound: true, PodPhase: "Pending"}, nil
	}}
	o, client := testSweepOrchestrator(t, jobsClient)
	client.updateCheckRunErr = errors.New("503")
	require.NoError(t, o.records.Create(context.Background(), sweepTestRecord("op-1", time.Now().Add(-time.Minute), false)))
	got := publishedSweep(t, o, "op-1")

	assert.Contains(t, got.Output, "Pending", "the diagnostic reaches the comment")
	assert.Contains(t, got.Output, "could not be completed (503)")
	assert.Contains(t, got.Output, "its check stays in progress")
}

// publishedSweep runs one sweep and returns the ProjectResult it added to
// the Operation's trigger.
func publishedSweep(t *testing.T, o *Orchestrator, operationID string) github.ProjectResult {
	t.Helper()
	return finalizedResult(t, o, operationID, func() {
		o.sweepOnce(context.Background())
	})
}

// abandonableRecord is a started record created age ago.
func abandonableRecord(operationID string, age time.Duration) *OperationRecord {
	rec := sweepTestRecord(operationID, time.Now().Add(-age), true)
	rec.CreatedAt = time.Now().Add(-age)
	return rec
}

// untouchedLocks fails the test if the sweep moves or releases a Lock.
type untouchedLocks struct {
	fakeLockManager
	t *testing.T
}

func (u *untouchedLocks) Apply(ctx context.Context, projectKey string, prNumber int, ev lock.Event, plan *lock.PlanRecord) (lock.Transition, error) {
	u.t.Error("abandoning an Operation must not move its Lock")
	return lock.Transition{}, nil
}

func (u *untouchedLocks) ReleaseLock(ctx context.Context, projectKey string, prNumber int) error {
	u.t.Error("abandoning an Operation must not release its Lock")
	return nil
}

func TestSweepOnce_AbandonsStartedRecordAfter24Hours(t *testing.T) {
	o, client := testSweepOrchestrator(t, &fakeJobCreator{t: t})
	o.locks = &untouchedLocks{t: t}
	require.NoError(t, o.records.Create(context.Background(), abandonableRecord("op-1", abandonAfter+time.Minute)))

	r := publishedSweep(t, o, "op-1")

	assert.Equal(t, "No result was received from the Runner within 24 hours.", r.Output)
	assert.False(t, r.Success)
	assert.True(t, r.Locked, "the Lock is left as it was")
	assert.Zero(t, client.updateCheckRunCalled, "the check stays in progress")
	rec, err := o.records.get(context.Background(), "op-1")
	require.NoError(t, err)
	assert.Nil(t, rec, "the record is deleted")
}

func TestSweepOnce_LeavesRecordStartedLessThan24HoursAgo(t *testing.T) {
	o, client := testSweepOrchestrator(t, &fakeJobCreator{t: t})
	require.NoError(t, o.records.Create(context.Background(), abandonableRecord("op-1", 23*time.Hour)))

	o.sweepOnce(context.Background())

	assert.Zero(t, client.updateCheckRunCalled)
	rec, err := o.records.get(context.Background(), "op-1")
	require.NoError(t, err)
	assert.NotNil(t, rec)
}

func TestSweepOnce_ResultAfterAbandonmentIsIgnored(t *testing.T) {
	o, client := testSweepOrchestrator(t, &fakeJobCreator{t: t})
	require.NoError(t, o.records.Create(context.Background(), abandonableRecord("op-1", abandonAfter+time.Minute)))
	r := publishedSweep(t, o, "op-1")
	require.Equal(t, abandonedOutput, r.Output)

	require.NoError(t, o.HandleResult(context.Background(), "op-1", rpc.OperationResult{Success: true}))

	assert.Zero(t, client.updateCheckRunCalled, "a late result leaves the check untouched")
	assert.Len(t, heldResults(t, o, "held-op-1"), 1, "and adds nothing to the trigger")
}

func TestSweepOnce_ConcurrentSweepsAbandonOnce(t *testing.T) {
	o, _ := testSweepOrchestrator(t, &fakeJobCreator{t: t})
	require.NoError(t, o.records.Create(context.Background(), abandonableRecord("op-1", abandonAfter+time.Minute)))

	r := finalizedResult(t, o, "op-1", func() {
		var wg sync.WaitGroup
		for range 2 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				o.sweepOnce(context.Background())
			}()
		}
		wg.Wait()
	})

	assert.Equal(t, abandonedOutput, r.Output, "finalizedResult requires exactly one result")
}
