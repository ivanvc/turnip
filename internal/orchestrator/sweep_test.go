package orchestrator

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"

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
	o, client := testSweepOrchestrator(t, runningJob(t))
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
	assert.Equal(t, "failure", client.updatedCheckRun.Conclusion)
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
	o, client := testSweepOrchestrator(t, runningJob(t))
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
	o, client := testSweepOrchestrator(t, runningJob(t))
	require.NoError(t, o.records.Create(context.Background(), abandonableRecord("op-1", 23*time.Hour)))

	o.sweepOnce(context.Background())

	assert.Zero(t, client.updateCheckRunCalled)
	rec, err := o.records.get(context.Background(), "op-1")
	require.NoError(t, err)
	assert.NotNil(t, rec)
}

func TestSweepOnce_ResultAfterAbandonmentIsIgnored(t *testing.T) {
	o, client := testSweepOrchestrator(t, runningJob(t))
	require.NoError(t, o.records.Create(context.Background(), abandonableRecord("op-1", abandonAfter+time.Minute)))
	r := publishedSweep(t, o, "op-1")
	require.Equal(t, abandonedOutput, r.Output)

	require.NoError(t, o.HandleResult(context.Background(), "op-1", rpc.OperationResult{Success: true}))

	assert.Zero(t, client.updateCheckRunCalled, "a late result leaves the check untouched")
	assert.Len(t, heldResults(t, o, "held-op-1"), 1, "and adds nothing to the trigger")
}

func TestSweepOnce_ConcurrentSweepsAbandonOnce(t *testing.T) {
	o, _ := testSweepOrchestrator(t, runningJob(t))
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

// runningJob is a Job client whose every Job is still running its Pod.
func runningJob(t *testing.T) *fakeJobCreator {
	return &fakeJobCreator{t: t, statusFn: func(ctx context.Context, jobName string) (*jobs.JobStatus, error) {
		return &jobs.JobStatus{JobFound: true, Active: 1, PodPhase: corev1.PodRunning}, nil
	}}
}

// jobWithStatus is a Job client reporting status for every Job.
func jobWithStatus(t *testing.T, status *jobs.JobStatus) *fakeJobCreator {
	return &fakeJobCreator{t: t, statusFn: func(ctx context.Context, jobName string) (*jobs.JobStatus, error) {
		return status, nil
	}}
}

// startedSweepRecord is a started record dispatched an hour ago: past its
// start deadline, far from abandonment.
func startedSweepRecord(operationID string) *OperationRecord {
	rec := sweepTestRecord(operationID, time.Now().Add(-55*time.Minute), true)
	rec.CreatedAt = time.Now().Add(-time.Hour)
	return rec
}

// Each row of the stopped diagnostics, as the sweep finalizes a started
// Operation whose Job ended without a result.
func TestSweepOnce_FinalizesStartedOperationWhoseJobEnded(t *testing.T) {
	failed := func(reason string) jobs.JobStatus {
		return jobs.JobStatus{JobFound: true, Finished: true, FailedReason: reason, Failed: 1}
	}
	tests := []struct {
		name   string
		status func() *jobs.JobStatus
		want   string
	}{
		{"the Job is gone", func() *jobs.JobStatus {
			return &jobs.JobStatus{JobFound: false}
		}, "No Job/Pod found for this operation; it may have been deleted or never successfully scheduled"},
		{"the Pod is gone", func() *jobs.JobStatus {
			return &jobs.JobStatus{JobFound: true, Active: 1}
		}, "No Job/Pod found for this operation; it may have been deleted or never successfully scheduled"},
		{"the Job's deadline, its Pod already deleted", func() *jobs.JobStatus {
			s := failed("DeadlineExceeded")
			s.Deadline = 4 * time.Hour
			return &s
		}, "Job turnip-runner-op-1: stopped at its deadline of 4h0m0s"},
		{"the Job's deadline, its Pod gone before the Failed condition", func() *jobs.JobStatus {
			return &jobs.JobStatus{JobFound: true, FailedReason: "DeadlineExceeded", Deadline: 4 * time.Hour}
		}, "Job turnip-runner-op-1: stopped at its deadline of 4h0m0s"},
		{"the Job's deadline, read from the setting", func() *jobs.JobStatus {
			s := failed("DeadlineExceeded")
			s.PodPhase = corev1.PodFailed
			s.TerminatedContainer, s.TerminatedReason = "runner", "Error"
			return &s
		}, "Job turnip-runner-op-1: stopped at its deadline of 2h0m0s"},
		{"an out-of-memory Runner", func() *jobs.JobStatus {
			s := failed("BackoffLimitExceeded")
			s.PodPhase = corev1.PodFailed
			s.TerminatedContainer, s.TerminatedReason = "runner", "OOMKilled"
			return &s
		}, "Job turnip-runner-op-1: container runner terminated (OOMKilled)"},
		{"an evicted Pod", func() *jobs.JobStatus {
			s := failed("BackoffLimitExceeded")
			s.PodPhase = corev1.PodFailed
			s.Evicted = true
			return &s
		}, "Job turnip-runner-op-1: Pod evicted"},
		{"a Job that completed", func() *jobs.JobStatus {
			return &jobs.JobStatus{JobFound: true, Finished: true, Succeeded: 1, PodPhase: corev1.PodSucceeded}
		}, "Job turnip-runner-op-1: finished without reporting a result"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o, client := testSweepOrchestrator(t, jobWithStatus(t, tt.status()))
			o.runnerDeadline = 2 * time.Hour
			locks := &fakeLockManager{}
			o.locks = locks
			require.NoError(t, o.records.Create(context.Background(), startedSweepRecord("op-1")))

			r := publishedSweep(t, o, "op-1")

			assert.Equal(t, tt.want, r.Output, "the diagnostic is the Project's output")
			assert.False(t, r.Success)
			assert.Equal(t, 1, client.updateCheckRunCalled)
			assert.Equal(t, "failure", client.updatedCheckRun.Conclusion)
			assert.Equal(t, tt.want, client.updatedCheckRun.Title)
			assert.Equal(t, "The Runner stopped before reporting a result.", client.updatedCheckRun.Summary)
			assert.Equal(t, []lock.Event{lock.EventPlanTimedOut}, locks.appliedEvents(), "a plan takes the plan timeout event")
			rec, err := o.records.get(context.Background(), "op-1")
			require.NoError(t, err)
			assert.Nil(t, rec, "the record is deleted")
		})
	}
}

func TestSweepOnce_StoppedMutatingOperationTakesTheMutatingEvent(t *testing.T) {
	o, _ := testSweepOrchestrator(t, jobWithStatus(t, &jobs.JobStatus{JobFound: true, Finished: true, FailedReason: "DeadlineExceeded", Deadline: time.Hour}))
	locks := &fakeLockManager{}
	o.locks = locks
	rec := startedSweepRecord("op-1")
	rec.Operation = "apply"
	require.NoError(t, o.records.Create(context.Background(), rec))

	r := publishedSweep(t, o, "op-1")

	assert.Equal(t, "Job turnip-runner-op-1: stopped at its deadline of 1h0m0s", r.Output)
	assert.Equal(t, []lock.Event{lock.EventMutatingTimedOut}, locks.appliedEvents())
}

// A started Operation whose Job still runs is the Runner's to finish:
// whether its Pod is running, or its Job has failed (or has only a
// FailureTarget) while its Pod is given its grace period to report.
func TestSweepOnce_LeavesStartedOperationWhoseJobRuns(t *testing.T) {
	tests := []struct {
		name   string
		status *jobs.JobStatus
		err    error
	}{
		{"running", &jobs.JobStatus{JobFound: true, Active: 1, PodPhase: corev1.PodRunning}, nil},
		{"its Pod stopping, not yet failed", &jobs.JobStatus{JobFound: true, Active: 1, PodPhase: corev1.PodRunning, TerminatedContainer: "runner", TerminatedReason: "Error"}, nil},
		{"its deadline reached, its Pod still terminating", &jobs.JobStatus{JobFound: true, FailedReason: "DeadlineExceeded", PodPhase: corev1.PodRunning}, nil},
		{"failed, its Pod still running", &jobs.JobStatus{JobFound: true, Finished: true, Failed: 1, FailedReason: "DeadlineExceeded", PodPhase: corev1.PodRunning}, nil},
		{"failed, its Pod still pending", &jobs.JobStatus{JobFound: true, Finished: true, Failed: 1, FailedReason: "DeadlineExceeded", PodPhase: corev1.PodPending}, nil},
		{"its status unreadable", nil, errors.New("apiserver unavailable")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			read := 0
			jobsClient := &fakeJobCreator{t: t, statusFn: func(ctx context.Context, jobName string) (*jobs.JobStatus, error) {
				read++
				assert.Equal(t, "turnip-runner-op-1", jobName)
				return tt.status, tt.err
			}}
			o, client := testSweepOrchestrator(t, jobsClient)
			o.locks = &untouchedLocks{t: t}
			require.NoError(t, o.records.Create(context.Background(), startedSweepRecord("op-1")))

			o.sweepOnce(context.Background())

			assert.Equal(t, 1, read, "the sweep reads the Job of a started Operation")
			assert.Zero(t, client.updateCheckRunCalled)
			rec, err := o.records.get(context.Background(), "op-1")
			require.NoError(t, err)
			require.NotNil(t, rec)
			assert.False(t, rec.Finalized)
		})
	}
}

// A started record with no Job name has no Job to read; it is left to
// abandonment rather than taken for a Runner that ended.
func TestSweepOnce_LeavesStartedOperationWithoutJobName(t *testing.T) {
	jobsClient := &fakeJobCreator{t: t, statusFn: func(ctx context.Context, jobName string) (*jobs.JobStatus, error) {
		t.Error("no Job name, nothing to read")
		return nil, nil
	}}
	o, client := testSweepOrchestrator(t, jobsClient)
	rec := startedSweepRecord("op-1")
	rec.JobName = ""
	require.NoError(t, o.records.Create(context.Background(), rec))

	o.sweepOnce(context.Background())

	assert.Zero(t, client.updateCheckRunCalled)
	got, err := o.records.get(context.Background(), "op-1")
	require.NoError(t, err)
	assert.NotNil(t, got)
}

// A Runner that reported on its way out finalized the Operation as the
// sweep read its ended Job: the sweep's claim loses, and nothing is
// finalized twice.
func TestSweepOnce_StoppedClaimLosesToResult(t *testing.T) {
	var o *Orchestrator
	jobsClient := &fakeJobCreator{t: t, statusFn: func(ctx context.Context, jobName string) (*jobs.JobStatus, error) {
		_, claimed, err := o.records.ClaimForResult(ctx, "op-1")
		require.NoError(t, err)
		assert.True(t, claimed, "the result claims first")
		return &jobs.JobStatus{JobFound: true, Finished: true, FailedReason: "DeadlineExceeded"}, nil
	}}
	o, client := testSweepOrchestrator(t, jobsClient)
	o.locks = &untouchedLocks{t: t}
	require.NoError(t, o.records.Create(context.Background(), startedSweepRecord("op-1")))

	o.sweepOnce(context.Background())

	assert.Zero(t, client.updateCheckRunCalled, "the sweep leaves the check to the result")
	rec, err := o.records.get(context.Background(), "op-1")
	require.NoError(t, err)
	require.NotNil(t, rec, "the result's finalization deletes it, not the sweep")
	assert.True(t, rec.Finalized)
}

// Two replicas finding the same ended Job: exactly one finalizes it.
func TestSweepOnce_ConcurrentSweepsFinalizeStoppedOnce(t *testing.T) {
	o, client := testSweepOrchestrator(t, jobWithStatus(t, &jobs.JobStatus{JobFound: true, Finished: true, FailedReason: "DeadlineExceeded", Deadline: time.Hour}))
	require.NoError(t, o.records.Create(context.Background(), startedSweepRecord("op-1")))

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

	assert.Equal(t, "Job turnip-runner-op-1: stopped at its deadline of 1h0m0s", r.Output, "finalizedResult requires exactly one result")
	assert.Equal(t, 1, client.updateCheckRunCalled)
}
