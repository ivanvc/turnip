package lock

import (
	"context"
	"errors"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/ivanvc/turnip/internal/plugin"
)

func newTestManager(t *testing.T) (*RedisLockManager, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr(), MaxRetries: -1})
	t.Cleanup(func() { client.Close() })
	return NewRedisLockManager(client), mr
}

func TestFullLifecycleSequence(t *testing.T) {
	ctx := context.Background()
	m, _ := newTestManager(t)
	const projectKey = "owner/repo/project"

	ok, err := m.AcquireLock(ctx, projectKey, 1, "https://example.com/pr/1", "alice")
	if err != nil || !ok {
		t.Fatalf("AcquireLock() = (%v, %v), want (true, nil)", ok, err)
	}

	summary := plugin.ChangeSummary{Add: 1, Change: 2, Destroy: 3}
	if err := m.StorePlanData(ctx, projectKey, 1, []byte("plan-bytes"), summary); err != nil {
		t.Fatalf("StorePlanData() error = %v", err)
	}

	data, gotSummary, err := m.GetPlanData(ctx, projectKey, 1)
	if err != nil {
		t.Fatalf("GetPlanData() error = %v", err)
	}
	if string(data) != "plan-bytes" || gotSummary != summary {
		t.Fatalf("GetPlanData() = (%q, %+v), want (%q, %+v)", data, gotSummary, "plan-bytes", summary)
	}

	status, err := m.GetLockStatus(ctx, projectKey)
	if err != nil {
		t.Fatalf("GetLockStatus() error = %v", err)
	}
	if !status.Locked || status.PRNumber != 1 || !status.HasPlan || status.PlanSummary != summary {
		t.Fatalf("GetLockStatus() = %+v, want locked by PR 1 with plan", status)
	}

	if err := m.ReleaseLock(ctx, projectKey, 1); err != nil {
		t.Fatalf("ReleaseLock() error = %v", err)
	}

	status, err = m.GetLockStatus(ctx, projectKey)
	if err != nil {
		t.Fatalf("GetLockStatus() after release error = %v", err)
	}
	if status.Locked {
		t.Fatalf("GetLockStatus() after release = %+v, want Locked: false", status)
	}
}

func TestAcquireLock_IdempotentSamePR(t *testing.T) {
	ctx := context.Background()
	m, _ := newTestManager(t)
	const projectKey = "owner/repo/project"

	if ok, err := m.AcquireLock(ctx, projectKey, 1, "https://example.com/pr/1", "alice"); err != nil || !ok {
		t.Fatalf("first AcquireLock() = (%v, %v), want (true, nil)", ok, err)
	}
	if err := m.StorePlanData(ctx, projectKey, 1, []byte("plan-bytes"), plugin.ChangeSummary{Add: 1}); err != nil {
		t.Fatalf("StorePlanData() error = %v", err)
	}

	// Re-acquire (e.g. a retried webhook) must not disturb the stored plan.
	if ok, err := m.AcquireLock(ctx, projectKey, 1, "https://example.com/pr/1", "alice"); err != nil || !ok {
		t.Fatalf("second AcquireLock() = (%v, %v), want (true, nil)", ok, err)
	}

	data, _, err := m.GetPlanData(ctx, projectKey, 1)
	if err != nil {
		t.Fatalf("GetPlanData() error = %v", err)
	}
	if string(data) != "plan-bytes" {
		t.Fatalf("GetPlanData() = %q, want %q (plan data must survive idempotent re-acquire)", data, "plan-bytes")
	}
}

func TestAcquireLock_DifferentPRFails(t *testing.T) {
	ctx := context.Background()
	m, _ := newTestManager(t)
	const projectKey = "owner/repo/project"

	if ok, err := m.AcquireLock(ctx, projectKey, 1, "https://example.com/pr/1", "alice"); err != nil || !ok {
		t.Fatalf("AcquireLock(PR 1) = (%v, %v), want (true, nil)", ok, err)
	}
	if err := m.StorePlanData(ctx, projectKey, 1, []byte("plan-bytes"), plugin.ChangeSummary{Add: 1}); err != nil {
		t.Fatalf("StorePlanData() error = %v", err)
	}

	ok, err := m.AcquireLock(ctx, projectKey, 2, "https://example.com/pr/2", "bob")
	if err != nil {
		t.Fatalf("AcquireLock(PR 2) error = %v", err)
	}
	if ok {
		t.Fatalf("AcquireLock(PR 2) = true, want false: PR 1 already holds the lock")
	}

	// PR 1's lock and plan data must be untouched.
	data, _, err := m.GetPlanData(ctx, projectKey, 1)
	if err != nil {
		t.Fatalf("GetPlanData(PR 1) error = %v", err)
	}
	if string(data) != "plan-bytes" {
		t.Fatalf("GetPlanData(PR 1) = %q, want %q (unaffected by failed PR 2 acquire)", data, "plan-bytes")
	}
}

func TestStorePlanData_WrongPRFails(t *testing.T) {
	ctx := context.Background()
	m, _ := newTestManager(t)
	const projectKey = "owner/repo/project"

	if _, err := m.AcquireLock(ctx, projectKey, 1, "https://example.com/pr/1", "alice"); err != nil {
		t.Fatalf("AcquireLock() error = %v", err)
	}

	err := m.StorePlanData(ctx, projectKey, 2, []byte("plan-bytes"), plugin.ChangeSummary{})
	if !errors.Is(err, ErrLockedByOtherPR) {
		t.Fatalf("StorePlanData(wrong PR) error = %v, want wrapping ErrLockedByOtherPR", err)
	}

	if _, _, err := m.GetPlanData(ctx, projectKey, 1); !errors.Is(err, ErrNoPlanData) {
		t.Fatalf("GetPlanData(PR 1) error = %v, want ErrNoPlanData (lock must be untouched)", err)
	}
}

func TestStorePlanData_NoLockFails(t *testing.T) {
	ctx := context.Background()
	m, _ := newTestManager(t)

	err := m.StorePlanData(ctx, "owner/repo/project", 1, []byte("plan-bytes"), plugin.ChangeSummary{})
	if !errors.Is(err, ErrNoLock) {
		t.Fatalf("StorePlanData() error = %v, want wrapping ErrNoLock", err)
	}
}

func TestStorePlanData_AfterReleaseFails(t *testing.T) {
	ctx := context.Background()
	m, _ := newTestManager(t)
	const projectKey = "owner/repo/project"

	if _, err := m.AcquireLock(ctx, projectKey, 1, "https://example.com/pr/1", "alice"); err != nil {
		t.Fatalf("AcquireLock() error = %v", err)
	}
	if err := m.ReleaseLock(ctx, projectKey, 1); err != nil {
		t.Fatalf("ReleaseLock() error = %v", err)
	}

	err := m.StorePlanData(ctx, projectKey, 1, []byte("plan-bytes"), plugin.ChangeSummary{})
	if !errors.Is(err, ErrNoLock) {
		t.Fatalf("StorePlanData() after release error = %v, want wrapping ErrNoLock", err)
	}
}

func TestGetPlanData_WrongPRFails(t *testing.T) {
	ctx := context.Background()
	m, _ := newTestManager(t)
	const projectKey = "owner/repo/project"

	if _, err := m.AcquireLock(ctx, projectKey, 1, "https://example.com/pr/1", "alice"); err != nil {
		t.Fatalf("AcquireLock() error = %v", err)
	}
	if err := m.StorePlanData(ctx, projectKey, 1, []byte("plan-bytes"), plugin.ChangeSummary{}); err != nil {
		t.Fatalf("StorePlanData() error = %v", err)
	}

	if _, _, err := m.GetPlanData(ctx, projectKey, 2); !errors.Is(err, ErrLockedByOtherPR) {
		t.Fatalf("GetPlanData(wrong PR) error = %v, want wrapping ErrLockedByOtherPR", err)
	}
}

func TestGetPlanData_NoPlanYetFails(t *testing.T) {
	ctx := context.Background()
	m, _ := newTestManager(t)
	const projectKey = "owner/repo/project"

	if _, err := m.AcquireLock(ctx, projectKey, 1, "https://example.com/pr/1", "alice"); err != nil {
		t.Fatalf("AcquireLock() error = %v", err)
	}

	if _, _, err := m.GetPlanData(ctx, projectKey, 1); !errors.Is(err, ErrNoPlanData) {
		t.Fatalf("GetPlanData() error = %v, want wrapping ErrNoPlanData (distinguishable from wrong-PR)", err)
	}
}

func TestReleaseLock_NoLockIsNoop(t *testing.T) {
	ctx := context.Background()
	m, _ := newTestManager(t)

	if err := m.ReleaseLock(ctx, "owner/repo/project", 1); err != nil {
		t.Fatalf("ReleaseLock() on unlocked project error = %v, want nil (idempotent)", err)
	}
}

func TestReleaseLock_WrongPRFails(t *testing.T) {
	ctx := context.Background()
	m, _ := newTestManager(t)
	const projectKey = "owner/repo/project"

	if _, err := m.AcquireLock(ctx, projectKey, 1, "https://example.com/pr/1", "alice"); err != nil {
		t.Fatalf("AcquireLock() error = %v", err)
	}

	err := m.ReleaseLock(ctx, projectKey, 2)
	if !errors.Is(err, ErrLockedByOtherPR) {
		t.Fatalf("ReleaseLock(wrong PR) error = %v, want wrapping ErrLockedByOtherPR", err)
	}

	locked, err := m.IsLockedByPR(ctx, projectKey, 1)
	if err != nil {
		t.Fatalf("IsLockedByPR() error = %v", err)
	}
	if !locked {
		t.Fatalf("IsLockedByPR(PR 1) = false, want true: failed release by PR 2 must not release the lock")
	}
}

func TestGetLockStatus_Unlocked(t *testing.T) {
	ctx := context.Background()
	m, _ := newTestManager(t)

	status, err := m.GetLockStatus(ctx, "owner/repo/project")
	if err != nil {
		t.Fatalf("GetLockStatus() error = %v", err)
	}
	if status.Locked {
		t.Fatalf("GetLockStatus() = %+v, want Locked: false", status)
	}
}

func TestGetLockStatus_LockedWithPlan(t *testing.T) {
	ctx := context.Background()
	m, _ := newTestManager(t)
	const projectKey = "owner/repo/project"

	if _, err := m.AcquireLock(ctx, projectKey, 1, "https://example.com/pr/1", "alice"); err != nil {
		t.Fatalf("AcquireLock() error = %v", err)
	}
	summary := plugin.ChangeSummary{Add: 4, Change: 5, Destroy: 6}
	if err := m.StorePlanData(ctx, projectKey, 1, []byte("plan-bytes"), summary); err != nil {
		t.Fatalf("StorePlanData() error = %v", err)
	}

	status, err := m.GetLockStatus(ctx, projectKey)
	if err != nil {
		t.Fatalf("GetLockStatus() error = %v", err)
	}
	if !status.Locked || status.PRNumber != 1 || status.PullRequestURL != "https://example.com/pr/1" ||
		status.LockedBy != "alice" || !status.HasPlan || status.PlanSummary != summary {
		t.Fatalf("GetLockStatus() = %+v, unexpected", status)
	}
}

func TestIsLockedByPR(t *testing.T) {
	ctx := context.Background()
	m, _ := newTestManager(t)
	const projectKey = "owner/repo/project"

	if locked, err := m.IsLockedByPR(ctx, projectKey, 1); err != nil || locked {
		t.Fatalf("IsLockedByPR() on unlocked project = (%v, %v), want (false, nil)", locked, err)
	}

	if _, err := m.AcquireLock(ctx, projectKey, 1, "https://example.com/pr/1", "alice"); err != nil {
		t.Fatalf("AcquireLock() error = %v", err)
	}

	if locked, err := m.IsLockedByPR(ctx, projectKey, 1); err != nil || !locked {
		t.Fatalf("IsLockedByPR(holding PR) = (%v, %v), want (true, nil)", locked, err)
	}
	if locked, err := m.IsLockedByPR(ctx, projectKey, 2); err != nil || locked {
		t.Fatalf("IsLockedByPR(other PR) = (%v, %v), want (false, nil)", locked, err)
	}
}

func TestGetLockData_DecodeErrorPropagates(t *testing.T) {
	ctx := context.Background()
	m, mr := newTestManager(t)
	const projectKey = "owner/repo/project"

	if err := mr.Set(lockKey(projectKey), "not-json"); err != nil {
		t.Fatalf("miniredis Set() error = %v", err)
	}

	if _, err := m.GetLockStatus(ctx, projectKey); err == nil {
		t.Fatalf("GetLockStatus() error = nil, want a decode error for a corrupted lock value")
	}
}

func TestConnectionFailurePropagates(t *testing.T) {
	ctx := context.Background()
	m, mr := newTestManager(t)
	const projectKey = "owner/repo/project"

	if _, err := m.AcquireLock(ctx, projectKey, 1, "https://example.com/pr/1", "alice"); err != nil {
		t.Fatalf("AcquireLock() error = %v", err)
	}
	if err := m.StorePlanData(ctx, projectKey, 1, []byte("plan-bytes"), plugin.ChangeSummary{}); err != nil {
		t.Fatalf("StorePlanData() error = %v", err)
	}

	mr.Close()

	if _, err := m.AcquireLock(ctx, projectKey, 2, "https://example.com/pr/2", "bob"); err == nil {
		t.Fatalf("AcquireLock() error = nil, want a connection error once the server is closed")
	}
	if err := m.StorePlanData(ctx, projectKey, 1, []byte("more-bytes"), plugin.ChangeSummary{}); err == nil {
		t.Fatalf("StorePlanData() error = nil, want a connection error once the server is closed")
	}
	if _, _, err := m.GetPlanData(ctx, projectKey, 1); err == nil {
		t.Fatalf("GetPlanData() error = nil, want a connection error once the server is closed")
	}
	if err := m.ReleaseLock(ctx, projectKey, 1); err == nil {
		t.Fatalf("ReleaseLock() error = nil, want a connection error once the server is closed")
	}
	if _, err := m.GetLockStatus(ctx, projectKey); err == nil {
		t.Fatalf("GetLockStatus() error = nil, want a connection error once the server is closed")
	}
	if _, err := m.IsLockedByPR(ctx, projectKey, 1); err == nil {
		t.Fatalf("IsLockedByPR() error = nil, want a connection error once the server is closed")
	}
}
