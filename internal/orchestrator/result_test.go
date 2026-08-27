package orchestrator

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivanvc/turnip/internal/config"
	"github.com/ivanvc/turnip/internal/github"
	"github.com/ivanvc/turnip/internal/plugin"
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

func createTestRecord(t *testing.T, o *Orchestrator, operationID, operation string, isApply bool) {
	t.Helper()
	require.NoError(t, o.records.Create(context.Background(), &OperationRecord{
		OperationID:   operationID,
		ProjectKey:    "owner/repo/helm-a",
		Project:       config.Project{Name: "helm-a", Tool: "helmfile"},
		Owner:         "owner",
		Repo:          "repo",
		PRNumber:      42,
		Operation:     operation,
		IsApply:       isApply,
		CheckRunID:    555,
		StartDeadline: time.Now().Add(5 * time.Minute).Unix(),
		CreatedAt:     time.Now(),
	}))
}

func TestHandleLog_MarksStarted(t *testing.T) {
	o, _ := testResultOrchestrator(t, &fakeLockManager{})
	createTestRecord(t, o, "op-1", "diff", false)

	require.NoError(t, o.HandleLog(context.Background(), "op-1", rpc.LogLine{Message: "hi"}))

	rec, err := o.records.get(context.Background(), "op-1")
	require.NoError(t, err)
	assert.True(t, rec.Started)
}

func TestHandleResult_UnclaimableIsNoop(t *testing.T) {
	o, _ := testResultOrchestrator(t, &fakeLockManager{})
	// No record created at all.
	require.NoError(t, o.HandleResult(context.Background(), "does-not-exist", rpc.OperationResult{Success: true}))
}

func TestHandleResult_SuccessfulPlan_StoresPlanDataNotReleasesLock(t *testing.T) {
	var stored bool
	var released bool
	locks := &fakeLockManager{
		storePlanDataFunc: func(ctx context.Context, projectKey string, prNumber int, planData []byte, summary plugin.ChangeSummary) error {
			stored = true
			assert.Equal(t, []byte("plan-data"), planData)
			return nil
		},
		releaseLockFunc: func(ctx context.Context, projectKey string, prNumber int) error {
			released = true
			return nil
		},
	}
	o, client := testResultOrchestrator(t, locks)
	createTestRecord(t, o, "op-1", "diff", false)

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
	createTestRecord(t, o, "op-1", "apply", true)

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
		assert.Contains(t, r.Output, "Lock released")
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
	createTestRecord(t, o, "op-1", "apply", true)

	require.NoError(t, o.HandleResult(context.Background(), "op-1", rpc.OperationResult{Success: false, Output: "failed"}))
	assert.False(t, released)
}

func TestHandleResult_DeletesRecordAfterFinalizing(t *testing.T) {
	o, _ := testResultOrchestrator(t, &fakeLockManager{})
	createTestRecord(t, o, "op-1", "diff", false)

	require.NoError(t, o.HandleResult(context.Background(), "op-1", rpc.OperationResult{Success: false}))

	rec, err := o.records.get(context.Background(), "op-1")
	require.NoError(t, err)
	assert.Nil(t, rec)
}
