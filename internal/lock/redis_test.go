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

// acquireLock, storePlan and releaseLock are test-side spellings of the
// two entry points, so these tests read as the sequences they describe
// rather than as event names. They go through the real doors.
func acquireLock(ctx context.Context, m *RedisLockManager, key string, pr int, url, by string) (bool, error) {
	ok, _, err := m.AcquireForPlan(ctx, key, pr, url, by)
	return ok, err
}

func storePlan(ctx context.Context, m *RedisLockManager, key string, pr int, rec PlanRecord) error {
	_, err := m.Apply(ctx, key, pr, EventPlanApplicable, &rec)
	return err
}

func releaseLock(ctx context.Context, m *RedisLockManager, key string, pr int) error {
	_, err := m.Apply(ctx, key, pr, EventUnlocked, nil)
	return err
}

func TestFullLifecycleSequence(t *testing.T) {
	ctx := context.Background()
	m, _ := newTestManager(t)
	const projectKey = "owner/repo/project"

	ok, err := acquireLock(ctx, m, projectKey, 1, "https://example.com/pr/1", "alice")
	require.NoError(t, err)
	require.True(t, ok)

	summary := plugin.ChangeSummary{Add: 1, Change: 2, Destroy: 3}
	require.NoError(t, storePlan(ctx, m, projectKey, 1, PlanRecord{Data: []byte("plan-bytes"), Summary: summary}))

	plan, err := m.GetPlan(ctx, projectKey, 1)
	require.NoError(t, err)
	assert.Equal(t, "plan-bytes", string(plan.Data))
	assert.Equal(t, summary, plan.Summary)

	status, err := m.GetLockStatus(ctx, projectKey)
	require.NoError(t, err)
	assert.True(t, status.Locked)
	assert.Equal(t, 1, status.PRNumber)
	assert.Equal(t, StatePlanReady, status.State)
	assert.Equal(t, summary, status.PlanSummary)

	require.NoError(t, releaseLock(ctx, m, projectKey, 1))

	status, err = m.GetLockStatus(ctx, projectKey)
	require.NoError(t, err)
	assert.False(t, status.Locked)
}
func TestAcquireLock_DifferentPRFails(t *testing.T) {
	ctx := context.Background()
	m, _ := newTestManager(t)
	const projectKey = "owner/repo/project"

	ok, err := acquireLock(ctx, m, projectKey, 1, "https://example.com/pr/1", "alice")
	require.NoError(t, err)
	require.True(t, ok)
	require.NoError(t, storePlan(ctx, m, projectKey, 1, PlanRecord{Data: []byte("plan-bytes"), Summary: plugin.ChangeSummary{Add: 1}}))

	ok, err = acquireLock(ctx, m, projectKey, 2, "https://example.com/pr/2", "bob")
	require.NoError(t, err)
	assert.False(t, ok, "PR 1 already holds the lock")

	// PR 1's lock and plan must be untouched.
	plan, err := m.GetPlan(ctx, projectKey, 1)
	require.NoError(t, err)
	assert.Equal(t, "plan-bytes", string(plan.Data), "unaffected by failed PR 2 acquire")
}

func TestStorePlan_WrongPRFails(t *testing.T) {
	ctx := context.Background()
	m, _ := newTestManager(t)
	const projectKey = "owner/repo/project"

	_, err := acquireLock(ctx, m, projectKey, 1, "https://example.com/pr/1", "alice")
	require.NoError(t, err)

	err = storePlan(ctx, m, projectKey, 2, PlanRecord{Data: []byte("plan-bytes")})
	require.ErrorIs(t, err, ErrLockedByOtherPR)

	_, err = m.GetPlan(ctx, projectKey, 1)
	assert.ErrorIs(t, err, ErrNoPlan, "lock must be untouched")
}

func TestGetPlan_WrongPRFails(t *testing.T) {
	ctx := context.Background()
	m, _ := newTestManager(t)
	const projectKey = "owner/repo/project"

	_, err := acquireLock(ctx, m, projectKey, 1, "https://example.com/pr/1", "alice")
	require.NoError(t, err)
	require.NoError(t, storePlan(ctx, m, projectKey, 1, PlanRecord{Data: []byte("plan-bytes")}))

	_, err = m.GetPlan(ctx, projectKey, 2)
	assert.ErrorIs(t, err, ErrLockedByOtherPR)
}

func TestGetPlan_NoPlanYetFails(t *testing.T) {
	ctx := context.Background()
	m, _ := newTestManager(t)
	const projectKey = "owner/repo/project"

	_, err := acquireLock(ctx, m, projectKey, 1, "https://example.com/pr/1", "alice")
	require.NoError(t, err)

	_, err = m.GetPlan(ctx, projectKey, 1)
	assert.ErrorIs(t, err, ErrNoPlan, "distinguishable from wrong-PR")
}

// TestStorePlan_RecordsAPlanWithNoArtifact is the Helmfile shape: a plan
// that succeeds and produces no bytes. Before the Lock recorded a state this stored
// nothing retrievable, so every apply that followed was refused as though
// no plan had run.
func TestStorePlan_RecordsAPlanWithNoArtifact(t *testing.T) {
	ctx := context.Background()
	m, _ := newTestManager(t)
	const projectKey = "owner/repo/project"

	_, err := acquireLock(ctx, m, projectKey, 1, "https://example.com/pr/1", "alice")
	require.NoError(t, err)

	summary := plugin.ChangeSummary{Change: 4}
	require.NoError(t, storePlan(ctx, m, projectKey, 1, PlanRecord{Args: []string{"-l", "name=web"}, Summary: summary}))

	plan, err := m.GetPlan(ctx, projectKey, 1)
	require.NoError(t, err, "a plan with no artifact is still a plan")
	assert.Empty(t, plan.Data)
	assert.Equal(t, []string{"-l", "name=web"}, plan.Args)
	assert.Equal(t, summary, plan.Summary)

	status, err := m.GetLockStatus(ctx, projectKey)
	require.NoError(t, err)
	assert.Equal(t, StatePlanReady, status.State, "the comment must be able to offer an apply")
}

// TestStorePlan_RecordsAbsentArguments pins Requirement 1.3: a plan that
// ran with no arguments is distinguishable from no plan at all.
func TestStorePlan_RecordsAbsentArguments(t *testing.T) {
	ctx := context.Background()
	m, _ := newTestManager(t)
	const projectKey = "owner/repo/project"

	_, err := acquireLock(ctx, m, projectKey, 1, "https://example.com/pr/1", "alice")
	require.NoError(t, err)
	require.NoError(t, storePlan(ctx, m, projectKey, 1, PlanRecord{}))

	plan, err := m.GetPlan(ctx, projectKey, 1)
	require.NoError(t, err)
	assert.Empty(t, plan.Args)
}

// TestGetPlan_LegacyLockHasNoPlan covers the migration: a Lock written
// before has_plan existed decodes with the field false, so its pull
// request is asked to re-plan rather than replaying an unrecorded scope.
func TestGetPlan_LegacyLockHasNoPlan(t *testing.T) {
	ctx := context.Background()
	m, mr := newTestManager(t)
	const projectKey = "owner/repo/project"

	legacy := `{"pr_number":1,"pull_request_url":"https://example.com/pr/1","locked_by":"alice","plan_data":"cGxhbg=="}`
	require.NoError(t, mr.Set(lockKey(projectKey), legacy))

	_, err := m.GetPlan(ctx, projectKey, 1)
	assert.ErrorIs(t, err, ErrNoPlan, "a pre-upgrade lock must ask for a re-plan")
}

func TestReleaseLock_NoLockIsNoop(t *testing.T) {
	ctx := context.Background()
	m, _ := newTestManager(t)

	assert.NoError(t, releaseLock(ctx, m, "owner/repo/project", 1))
}

func TestReleaseLock_WrongPRFails(t *testing.T) {
	ctx := context.Background()
	m, _ := newTestManager(t)
	const projectKey = "owner/repo/project"

	_, err := acquireLock(ctx, m, projectKey, 1, "https://example.com/pr/1", "alice")
	require.NoError(t, err)

	err = releaseLock(ctx, m, projectKey, 2)
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

	_, err := acquireLock(ctx, m, projectKey, 1, "https://example.com/pr/1", "alice")
	require.NoError(t, err)
	summary := plugin.ChangeSummary{Add: 4, Change: 5, Destroy: 6}
	require.NoError(t, storePlan(ctx, m, projectKey, 1, PlanRecord{Data: []byte("plan-bytes"), Summary: summary}))

	status, err := m.GetLockStatus(ctx, projectKey)
	require.NoError(t, err)
	assert.True(t, status.Locked)
	assert.Equal(t, 1, status.PRNumber)
	assert.Equal(t, "https://example.com/pr/1", status.PullRequestURL)
	assert.Equal(t, "alice", status.LockedBy)
	assert.Equal(t, StatePlanReady, status.State)
	assert.Equal(t, summary, status.PlanSummary)
}

func TestIsLockedByPR(t *testing.T) {
	ctx := context.Background()
	m, _ := newTestManager(t)
	const projectKey = "owner/repo/project"

	locked, err := m.IsLockedByPR(ctx, projectKey, 1)
	require.NoError(t, err)
	assert.False(t, locked)

	_, err = acquireLock(ctx, m, projectKey, 1, "https://example.com/pr/1", "alice")
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

	_, err := acquireLock(ctx, m, projectKey, 1, "https://example.com/pr/1", "alice")
	require.NoError(t, err)
	require.NoError(t, storePlan(ctx, m, projectKey, 1, PlanRecord{Data: []byte("plan-bytes")}))

	mr.Close()

	_, err = acquireLock(ctx, m, projectKey, 2, "https://example.com/pr/2", "bob")
	require.Error(t, err)

	err = storePlan(ctx, m, projectKey, 1, PlanRecord{Data: []byte("more-bytes")})
	require.Error(t, err)

	_, err = m.GetPlan(ctx, projectKey, 1)
	require.Error(t, err)

	err = releaseLock(ctx, m, projectKey, 1)
	require.Error(t, err)

	_, err = m.GetLockStatus(ctx, projectKey)
	require.Error(t, err)

	_, err = m.IsLockedByPR(ctx, projectKey, 1)
	require.Error(t, err)
}
