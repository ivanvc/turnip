package lock

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivanvc/turnip/internal/plugin"
)

func newTestManager(t *testing.T) (*RedisLockManager, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr(), MaxRetries: -1})
	t.Cleanup(func() { _ = client.Close() })
	return NewRedisLockManager(client), mr
}

func TestFullLifecycleSequence(t *testing.T) {
	ctx := context.Background()
	m, _ := newTestManager(t)
	const projectKey = "owner/repo/project"

	ok, err := m.AcquireLock(ctx, projectKey, 1, "https://example.com/pr/1", "alice")
	require.NoError(t, err)
	require.True(t, ok)

	summary := plugin.ChangeSummary{Add: 1, Change: 2, Destroy: 3}
	require.NoError(t, m.StorePlanData(ctx, projectKey, 1, []byte("plan-bytes"), summary))

	data, gotSummary, err := m.GetPlanData(ctx, projectKey, 1)
	require.NoError(t, err)
	assert.Equal(t, "plan-bytes", string(data))
	assert.Equal(t, summary, gotSummary)

	status, err := m.GetLockStatus(ctx, projectKey)
	require.NoError(t, err)
	assert.True(t, status.Locked)
	assert.Equal(t, 1, status.PRNumber)
	assert.True(t, status.HasPlan)
	assert.Equal(t, summary, status.PlanSummary)

	require.NoError(t, m.ReleaseLock(ctx, projectKey, 1))

	status, err = m.GetLockStatus(ctx, projectKey)
	require.NoError(t, err)
	assert.False(t, status.Locked)
}

func TestAcquireLock_IdempotentSamePR(t *testing.T) {
	ctx := context.Background()
	m, _ := newTestManager(t)
	const projectKey = "owner/repo/project"

	ok, err := m.AcquireLock(ctx, projectKey, 1, "https://example.com/pr/1", "alice")
	require.NoError(t, err)
	require.True(t, ok)
	require.NoError(t, m.StorePlanData(ctx, projectKey, 1, []byte("plan-bytes"), plugin.ChangeSummary{Add: 1}))

	// Re-acquire (e.g. a retried webhook) must not disturb the stored plan.
	ok, err = m.AcquireLock(ctx, projectKey, 1, "https://example.com/pr/1", "alice")
	require.NoError(t, err)
	require.True(t, ok)

	data, _, err := m.GetPlanData(ctx, projectKey, 1)
	require.NoError(t, err)
	assert.Equal(t, "plan-bytes", string(data), "plan data must survive idempotent re-acquire")
}

func TestAcquireLock_DifferentPRFails(t *testing.T) {
	ctx := context.Background()
	m, _ := newTestManager(t)
	const projectKey = "owner/repo/project"

	ok, err := m.AcquireLock(ctx, projectKey, 1, "https://example.com/pr/1", "alice")
	require.NoError(t, err)
	require.True(t, ok)
	require.NoError(t, m.StorePlanData(ctx, projectKey, 1, []byte("plan-bytes"), plugin.ChangeSummary{Add: 1}))

	ok, err = m.AcquireLock(ctx, projectKey, 2, "https://example.com/pr/2", "bob")
	require.NoError(t, err)
	assert.False(t, ok, "PR 1 already holds the lock")

	// PR 1's lock and plan data must be untouched.
	data, _, err := m.GetPlanData(ctx, projectKey, 1)
	require.NoError(t, err)
	assert.Equal(t, "plan-bytes", string(data), "unaffected by failed PR 2 acquire")
}

func TestStorePlanData_WrongPRFails(t *testing.T) {
	ctx := context.Background()
	m, _ := newTestManager(t)
	const projectKey = "owner/repo/project"

	_, err := m.AcquireLock(ctx, projectKey, 1, "https://example.com/pr/1", "alice")
	require.NoError(t, err)

	err = m.StorePlanData(ctx, projectKey, 2, []byte("plan-bytes"), plugin.ChangeSummary{})
	require.ErrorIs(t, err, ErrLockedByOtherPR)

	_, _, err = m.GetPlanData(ctx, projectKey, 1)
	assert.ErrorIs(t, err, ErrNoPlanData, "lock must be untouched")
}

func TestStorePlanData_NoLockFails(t *testing.T) {
	ctx := context.Background()
	m, _ := newTestManager(t)

	err := m.StorePlanData(ctx, "owner/repo/project", 1, []byte("plan-bytes"), plugin.ChangeSummary{})
	assert.ErrorIs(t, err, ErrNoLock)
}

func TestStorePlanData_AfterReleaseFails(t *testing.T) {
	ctx := context.Background()
	m, _ := newTestManager(t)
	const projectKey = "owner/repo/project"

	_, err := m.AcquireLock(ctx, projectKey, 1, "https://example.com/pr/1", "alice")
	require.NoError(t, err)
	require.NoError(t, m.ReleaseLock(ctx, projectKey, 1))

	err = m.StorePlanData(ctx, projectKey, 1, []byte("plan-bytes"), plugin.ChangeSummary{})
	assert.ErrorIs(t, err, ErrNoLock)
}

func TestGetPlanData_WrongPRFails(t *testing.T) {
	ctx := context.Background()
	m, _ := newTestManager(t)
	const projectKey = "owner/repo/project"

	_, err := m.AcquireLock(ctx, projectKey, 1, "https://example.com/pr/1", "alice")
	require.NoError(t, err)
	require.NoError(t, m.StorePlanData(ctx, projectKey, 1, []byte("plan-bytes"), plugin.ChangeSummary{}))

	_, _, err = m.GetPlanData(ctx, projectKey, 2)
	assert.ErrorIs(t, err, ErrLockedByOtherPR)
}

func TestGetPlanData_NoPlanYetFails(t *testing.T) {
	ctx := context.Background()
	m, _ := newTestManager(t)
	const projectKey = "owner/repo/project"

	_, err := m.AcquireLock(ctx, projectKey, 1, "https://example.com/pr/1", "alice")
	require.NoError(t, err)

	_, _, err = m.GetPlanData(ctx, projectKey, 1)
	assert.ErrorIs(t, err, ErrNoPlanData, "distinguishable from wrong-PR")
}

func TestReleaseLock_NoLockIsNoop(t *testing.T) {
	ctx := context.Background()
	m, _ := newTestManager(t)

	assert.NoError(t, m.ReleaseLock(ctx, "owner/repo/project", 1))
}

func TestReleaseLock_WrongPRFails(t *testing.T) {
	ctx := context.Background()
	m, _ := newTestManager(t)
	const projectKey = "owner/repo/project"

	_, err := m.AcquireLock(ctx, projectKey, 1, "https://example.com/pr/1", "alice")
	require.NoError(t, err)

	err = m.ReleaseLock(ctx, projectKey, 2)
	require.ErrorIs(t, err, ErrLockedByOtherPR)

	locked, err := m.IsLockedByPR(ctx, projectKey, 1)
	require.NoError(t, err)
	assert.True(t, locked, "failed release by PR 2 must not release the lock")
}

func TestGetLockStatus_Unlocked(t *testing.T) {
	ctx := context.Background()
	m, _ := newTestManager(t)

	status, err := m.GetLockStatus(ctx, "owner/repo/project")
	require.NoError(t, err)
	assert.False(t, status.Locked)
}

func TestGetLockStatus_LockedWithPlan(t *testing.T) {
	ctx := context.Background()
	m, _ := newTestManager(t)
	const projectKey = "owner/repo/project"

	_, err := m.AcquireLock(ctx, projectKey, 1, "https://example.com/pr/1", "alice")
	require.NoError(t, err)
	summary := plugin.ChangeSummary{Add: 4, Change: 5, Destroy: 6}
	require.NoError(t, m.StorePlanData(ctx, projectKey, 1, []byte("plan-bytes"), summary))

	status, err := m.GetLockStatus(ctx, projectKey)
	require.NoError(t, err)
	assert.True(t, status.Locked)
	assert.Equal(t, 1, status.PRNumber)
	assert.Equal(t, "https://example.com/pr/1", status.PullRequestURL)
	assert.Equal(t, "alice", status.LockedBy)
	assert.True(t, status.HasPlan)
	assert.Equal(t, summary, status.PlanSummary)
}

func TestIsLockedByPR(t *testing.T) {
	ctx := context.Background()
	m, _ := newTestManager(t)
	const projectKey = "owner/repo/project"

	locked, err := m.IsLockedByPR(ctx, projectKey, 1)
	require.NoError(t, err)
	assert.False(t, locked)

	_, err = m.AcquireLock(ctx, projectKey, 1, "https://example.com/pr/1", "alice")
	require.NoError(t, err)

	locked, err = m.IsLockedByPR(ctx, projectKey, 1)
	require.NoError(t, err)
	assert.True(t, locked)

	locked, err = m.IsLockedByPR(ctx, projectKey, 2)
	require.NoError(t, err)
	assert.False(t, locked)
}

func TestGetLockData_DecodeErrorPropagates(t *testing.T) {
	ctx := context.Background()
	m, mr := newTestManager(t)
	const projectKey = "owner/repo/project"

	require.NoError(t, mr.Set(lockKey(projectKey), "not-json"))

	_, err := m.GetLockStatus(ctx, projectKey)
	assert.Error(t, err, "want a decode error for a corrupted lock value")
}

func TestConnectionFailurePropagates(t *testing.T) {
	ctx := context.Background()
	m, mr := newTestManager(t)
	const projectKey = "owner/repo/project"

	_, err := m.AcquireLock(ctx, projectKey, 1, "https://example.com/pr/1", "alice")
	require.NoError(t, err)
	require.NoError(t, m.StorePlanData(ctx, projectKey, 1, []byte("plan-bytes"), plugin.ChangeSummary{}))

	mr.Close()

	_, err = m.AcquireLock(ctx, projectKey, 2, "https://example.com/pr/2", "bob")
	require.Error(t, err)

	err = m.StorePlanData(ctx, projectKey, 1, []byte("more-bytes"), plugin.ChangeSummary{})
	require.Error(t, err)

	_, _, err = m.GetPlanData(ctx, projectKey, 1)
	require.Error(t, err)

	err = m.ReleaseLock(ctx, projectKey, 1)
	require.Error(t, err)

	_, err = m.GetLockStatus(ctx, projectKey)
	require.Error(t, err)

	_, err = m.IsLockedByPR(ctx, projectKey, 1)
	require.Error(t, err)
}
