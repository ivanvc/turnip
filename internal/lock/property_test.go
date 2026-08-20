package lock

import (
	"context"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"

	"github.com/ivanvc/turnip/internal/plugin"
)

func testParameters() *gopter.TestParameters {
	params := gopter.DefaultTestParameters()
	params.MinSuccessfulTests = 100
	return params
}

func newPropertyClient(t interface {
	Fatalf(string, ...any)
	Cleanup(func())
	Logf(string, ...any)
}) *redis.Client {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { client.Close() })
	return client
}

var changeSummaryGen = gen.Struct(reflect.TypeOf(plugin.ChangeSummary{}), map[string]gopter.Gen{
	"Add":     gen.IntRange(0, 100),
	"Change":  gen.IntRange(0, 100),
	"Destroy": gen.IntRange(0, 100),
})

// Feature: multi-iac-automation-platform, Property 9: Lock Acquisition Prevents Concurrent Operations
func TestProperty_LockAcquisitionPreventsConcurrentOperations(t *testing.T) {
	properties := gopter.NewProperties(testParameters())

	properties.Property("PR B cannot acquire a lock PR A already holds", prop.ForAll(
		func(projectKey string, prA, prB int) bool {
			ctx := context.Background()
			m := NewRedisLockManager(newPropertyClient(t))

			okA, err := m.AcquireLock(ctx, projectKey, prA, "https://example.com/pr/a", "alice")
			if err != nil || !okA {
				return false
			}

			okB, err := m.AcquireLock(ctx, projectKey, prB, "https://example.com/pr/b", "bob")
			if err != nil {
				return false
			}

			return !okB
		},
		gen.Identifier(),
		gen.IntRange(1, 100000),
		gen.IntRange(100001, 200000),
	))

	properties.TestingRun(t)
}

// Feature: multi-iac-automation-platform, Property 10: Lock Release After Operation Completion
func TestProperty_LockReleaseAfterOperationCompletion(t *testing.T) {
	properties := gopter.NewProperties(testParameters())

	properties.Property("releasing a lock after storing a plan (modeling a successful apply) leaves it unlocked", prop.ForAll(
		func(projectKey string, pr int, planData []byte) bool {
			ctx := context.Background()
			m := NewRedisLockManager(newPropertyClient(t))

			if ok, err := m.AcquireLock(ctx, projectKey, pr, "https://example.com/pr", "alice"); err != nil || !ok {
				return false
			}
			if err := m.StorePlanData(ctx, projectKey, pr, planData, plugin.ChangeSummary{Add: 1}); err != nil {
				return false
			}
			if err := m.ReleaseLock(ctx, projectKey, pr); err != nil {
				return false
			}

			status, err := m.GetLockStatus(ctx, projectKey)
			if err != nil {
				return false
			}

			return !status.Locked
		},
		gen.Identifier(),
		gen.IntRange(1, 100000),
		gen.SliceOf(gen.UInt8()).Map(func(bs []uint8) []byte {
			out := make([]byte, len(bs))
			for i, b := range bs {
				out[i] = byte(b)
			}
			return out
		}),
	))

	properties.TestingRun(t)
}

// Feature: multi-iac-automation-platform, Property 11: Plan-Apply Lock Consistency
func TestProperty_PlanApplyLockConsistency(t *testing.T) {
	properties := gopter.NewProperties(testParameters())

	properties.Property("GetPlanData returns exactly what StorePlanData stored", prop.ForAll(
		func(projectKey string, pr int, planData []byte, summary plugin.ChangeSummary) bool {
			ctx := context.Background()
			m := NewRedisLockManager(newPropertyClient(t))

			if ok, err := m.AcquireLock(ctx, projectKey, pr, "https://example.com/pr", "alice"); err != nil || !ok {
				return false
			}
			if err := m.StorePlanData(ctx, projectKey, pr, planData, summary); err != nil {
				return false
			}

			gotData, gotSummary, err := m.GetPlanData(ctx, projectKey, pr)
			if err != nil {
				return false
			}

			// A zero-length stored planData round-trips as ErrNoPlanData
			// (Requirement 2.5), which is covered by the unit tests; the
			// property here only holds for non-empty plan bytes.
			if len(planData) == 0 {
				return true
			}

			return reflect.DeepEqual(planData, gotData) && gotSummary == summary
		},
		gen.Identifier(),
		gen.IntRange(1, 100000),
		gen.SliceOfN(16, gen.UInt8()).Map(func(bs []uint8) []byte {
			out := make([]byte, len(bs))
			for i, b := range bs {
				out[i] = byte(b)
			}
			return out
		}),
		changeSummaryGen,
	))

	properties.TestingRun(t)
}

// Feature: multi-iac-automation-platform, Property 36: Lock Acquisition Across Instances
func TestProperty_LockAcquisitionAcrossInstances(t *testing.T) {
	properties := gopter.NewProperties(testParameters())

	properties.Property("exactly one of N concurrent AcquireLock calls for the same key succeeds", prop.ForAll(
		func(projectKey string, base int) bool {
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

			return successes.Load() == 1
		},
		gen.Identifier(),
		gen.IntRange(1, 100000),
	))

	properties.TestingRun(t)
}
