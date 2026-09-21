package lock

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

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
	t.Cleanup(func() { _ = client.Close() })
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

		okA, err := acquireLock(ctx, m, projectKey, prA, "https://example.com/pr/a", "alice")
		require.NoError(t, err)
		require.True(t, okA)

		okB, err := acquireLock(ctx, m, projectKey, prB, "https://example.com/pr/b", "bob")
		require.NoError(t, err)
		require.False(t, okB, "PR A already holds the lock")
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

		ok, err := acquireLock(ctx, m, projectKey, pr, "https://example.com/pr", "alice")
		require.NoError(t, err)
		require.True(t, ok)
		require.NoError(t, storePlan(ctx, m, projectKey, pr, PlanRecord{Data: planData, Summary: plugin.ChangeSummary{Add: 1}}))
		require.NoError(t, releaseLock(ctx, m, projectKey, pr))

		status, err := m.GetLockStatus(ctx, projectKey)
		require.NoError(t, err)
		require.False(t, status.Locked)
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

		ok, err := acquireLock(ctx, m, projectKey, pr, "https://example.com/pr", "alice")
		require.NoError(t, err)
		require.True(t, ok)
		require.NoError(t, storePlan(ctx, m, projectKey, pr, PlanRecord{Data: planData, Summary: summary}))

		plan, err := m.GetPlan(ctx, projectKey, pr)
		require.NoError(t, err)
		require.Equal(t, planData, plan.Data)
		require.Equal(t, summary, plan.Summary)
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
				ok, err := acquireLock(context.Background(), m, projectKey, pr, "https://example.com/pr", "someone")
				if err == nil && ok {
					successes.Add(1)
				}
			}(pr)
		}
		wg.Wait()

		require.EqualValues(t, 1, successes.Load())
	})
}
