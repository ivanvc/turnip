package lock

import (
	"context"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"pgregory.net/rapid"

	"github.com/ivanvc/turnip/internal/plugin"
)

// identifierPattern approximates a Go-style identifier: a letter followed
// by up to 15 letters or digits.
const identifierPattern = "[a-zA-Z][a-zA-Z0-9]{0,15}"

func newPropertyManager(t *rapid.T) *RedisLockManager {
	return NewRedisLockManager(newPropertyClient(t))
}

func newPropertyClient(t *rapid.T) *redis.Client {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { client.Close() })
	return client
}

func genChangeSummary(t *rapid.T) plugin.ChangeSummary {
	return plugin.ChangeSummary{
		Add:     rapid.IntRange(0, 100).Draw(t, "add"),
		Change:  rapid.IntRange(0, 100).Draw(t, "change"),
		Destroy: rapid.IntRange(0, 100).Draw(t, "destroy"),
	}
}

// Feature: multi-iac-automation-platform, Property 9: Lock Acquisition Prevents Concurrent Operations
func TestProperty_LockAcquisitionPreventsConcurrentOperations(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		projectKey := rapid.StringMatching(identifierPattern).Draw(t, "projectKey")
		prA := rapid.IntRange(1, 100000).Draw(t, "prA")
		prB := rapid.IntRange(100001, 200000).Draw(t, "prB")

		ctx := context.Background()
		m := newPropertyManager(t)

		okA, err := m.AcquireLock(ctx, projectKey, prA, "https://example.com/pr/a", "alice")
		if err != nil || !okA {
			t.Fatalf("AcquireLock(PR A) = (%v, %v), want (true, nil)", okA, err)
		}

		okB, err := m.AcquireLock(ctx, projectKey, prB, "https://example.com/pr/b", "bob")
		if err != nil {
			t.Fatalf("AcquireLock(PR B) error = %v", err)
		}
		if okB {
			t.Fatalf("AcquireLock(PR B) = true, want false: PR A already holds the lock")
		}
	})
}

// Feature: multi-iac-automation-platform, Property 10: Lock Release After Operation Completion
func TestProperty_LockReleaseAfterOperationCompletion(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		projectKey := rapid.StringMatching(identifierPattern).Draw(t, "projectKey")
		pr := rapid.IntRange(1, 100000).Draw(t, "pr")
		planData := rapid.SliceOf(rapid.Byte()).Draw(t, "planData")

		ctx := context.Background()
		m := newPropertyManager(t)

		if ok, err := m.AcquireLock(ctx, projectKey, pr, "https://example.com/pr", "alice"); err != nil || !ok {
			t.Fatalf("AcquireLock() = (%v, %v), want (true, nil)", ok, err)
		}
		if err := m.StorePlanData(ctx, projectKey, pr, planData, plugin.ChangeSummary{Add: 1}); err != nil {
			t.Fatalf("StorePlanData() error = %v", err)
		}
		if err := m.ReleaseLock(ctx, projectKey, pr); err != nil {
			t.Fatalf("ReleaseLock() error = %v", err)
		}

		status, err := m.GetLockStatus(ctx, projectKey)
		if err != nil {
			t.Fatalf("GetLockStatus() error = %v", err)
		}
		if status.Locked {
			t.Fatalf("GetLockStatus() after release = %+v, want Locked: false", status)
		}
	})
}

// Feature: multi-iac-automation-platform, Property 11: Plan-Apply Lock Consistency
func TestProperty_PlanApplyLockConsistency(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		projectKey := rapid.StringMatching(identifierPattern).Draw(t, "projectKey")
		pr := rapid.IntRange(1, 100000).Draw(t, "pr")
		planData := rapid.SliceOfN(rapid.Byte(), 16, 16).Draw(t, "planData")
		summary := genChangeSummary(t)

		ctx := context.Background()
		m := newPropertyManager(t)

		if ok, err := m.AcquireLock(ctx, projectKey, pr, "https://example.com/pr", "alice"); err != nil || !ok {
			t.Fatalf("AcquireLock() = (%v, %v), want (true, nil)", ok, err)
		}
		if err := m.StorePlanData(ctx, projectKey, pr, planData, summary); err != nil {
			t.Fatalf("StorePlanData() error = %v", err)
		}

		gotData, gotSummary, err := m.GetPlanData(ctx, projectKey, pr)
		if err != nil {
			t.Fatalf("GetPlanData() error = %v", err)
		}
		if !reflect.DeepEqual(planData, gotData) {
			t.Fatalf("GetPlanData() data = %v, want %v", gotData, planData)
		}
		if gotSummary != summary {
			t.Fatalf("GetPlanData() summary = %+v, want %+v", gotSummary, summary)
		}
	})
}

// Feature: multi-iac-automation-platform, Property 36: Lock Acquisition Across Instances
func TestProperty_LockAcquisitionAcrossInstances(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		projectKey := rapid.StringMatching(identifierPattern).Draw(t, "projectKey")
		base := rapid.IntRange(1, 100000).Draw(t, "base")

		client := newPropertyClient(t)
		const numGoroutines = 10

		var successes atomic.Int64
		var wg sync.WaitGroup
		wg.Add(numGoroutines)
		for i := range numGoroutines {
			pr := base + i
			go func(pr int) {
				defer wg.Done()
				// Each goroutine uses its own LockManager value sharing
				// one *redis.Client, per design.md, so the race is
				// exercised at the Redis level, not masked by
				// in-process serialization on a shared LockManager.
				m := NewRedisLockManager(client)
				ok, err := m.AcquireLock(context.Background(), projectKey, pr, "https://example.com/pr", "someone")
				if err == nil && ok {
					successes.Add(1)
				}
			}(pr)
		}
		wg.Wait()

		if got := successes.Load(); got != 1 {
			t.Fatalf("successes = %d, want exactly 1", got)
		}
	})
}
