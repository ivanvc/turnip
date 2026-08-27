package orchestrator

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivanvc/turnip/internal/config"
	"github.com/ivanvc/turnip/internal/github"
	"github.com/ivanvc/turnip/internal/jobs"
)

func testSweepOrchestrator(t *testing.T, jobsClient jobCreator) (*Orchestrator, *fakeExecuteClient) {
	t.Helper()
	client := newTestRedisClient(t)
	fakeClient := &fakeExecuteClient{}
	o := &Orchestrator{
		installationClient: func(id int64) github.GitHubClient { return fakeClient },
		jobs:               jobsClient,
		plugins:            testRegistry(),
		records:            newRecordStore(client),
		redis:              client,
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

	resultCh := make(chan github.ProjectResult, 1)
	go func() {
		r, err := waitForDone(context.Background(), o.redis, "op-1")
		assert.NoError(t, err)
		resultCh <- r
	}()
	require.Eventually(t, func() bool {
		return o.redis.PubSubNumSub(context.Background(), doneChannel("op-1")).Val()[doneChannel("op-1")] > 0
	}, time.Second, time.Millisecond)

	o.sweepOnce(context.Background())

	assert.True(t, statusCalled)
	assert.Equal(t, 1, client.updateCheckRunCalled)

	select {
	case r := <-resultCh:
		assert.False(t, r.Success)
		assert.Contains(t, r.Output, "Pending")
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for published timeout result")
	}

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
