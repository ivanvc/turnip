package lock

import (
	"context"
	"os"
	"sync"
	"testing"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const pkey = "owner/repo/project"

func TestAcquireForPlan_CreatesInPlanning(t *testing.T) {
	ctx := context.Background()
	m, _ := newTestManager(t)

	ok, tr, err := m.AcquireForPlan(ctx, pkey, 1, "https://example.com/pr/1", "alice")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, StatePlanning, tr.To)
	assert.False(t, tr.Released)

	status, err := m.GetLockStatus(ctx, pkey)
	require.NoError(t, err)
	assert.Equal(t, StatePlanning, status.State)
}

// The edge the slice exists for: dispatching a plan supersedes the stored
// one at the push, not minutes later when the Runner reports.
func TestAcquireForPlan_SupersedesAStoredPlan(t *testing.T) {
	ctx := context.Background()
	m, _ := newTestManager(t)

	_, _, err := m.AcquireForPlan(ctx, pkey, 1, "url", "alice")
	require.NoError(t, err)
	_, err = m.Apply(ctx, pkey, 1, EventPlanApplicable, &PlanRecord{Args: []string{"-l", "name=web"}})
	require.NoError(t, err)

	status, err := m.GetLockStatus(ctx, pkey)
	require.NoError(t, err)
	require.Equal(t, StatePlanReady, status.State)

	ok, tr, err := m.AcquireForPlan(ctx, pkey, 1, "url", "alice")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, StatePlanReady, tr.From)
	assert.Equal(t, StatePlanStale, tr.To, "a new plan invalidates the stored one")

	status, err = m.GetLockStatus(ctx, pkey)
	require.NoError(t, err)
	assert.Equal(t, StatePlanStale, status.State)
	_, err = m.GetPlan(ctx, pkey, 1)
	assert.ErrorIs(t, err, ErrNoPlan, "the recorded plan is no longer retrievable for an apply")
}

func TestAcquireForPlan_DifferentPRChangesNothing(t *testing.T) {
	ctx := context.Background()
	m, _ := newTestManager(t)

	_, _, err := m.AcquireForPlan(ctx, pkey, 1, "url", "alice")
	require.NoError(t, err)
	_, err = m.Apply(ctx, pkey, 1, EventPlanApplicable, &PlanRecord{})
	require.NoError(t, err)

	ok, _, err := m.AcquireForPlan(ctx, pkey, 2, "url", "bob")
	require.NoError(t, err)
	assert.False(t, ok)

	status, err := m.GetLockStatus(ctx, pkey)
	require.NoError(t, err)
	assert.Equal(t, 1, status.PRNumber)
	assert.Equal(t, StatePlanReady, status.State, "a refused acquire must not move the holder's state")
}

// A Lock written before states existed decodes as stale; dispatching a
// plan is where it gains an explicit state rather than decoding forever.
func TestAcquireForPlan_UpgradesALegacyLock(t *testing.T) {
	ctx := context.Background()
	m, mr := newTestManager(t)

	legacy := `{"pr_number":1,"pull_request_url":"u","locked_by":"alice","has_plan":true}`
	require.NoError(t, mr.Set(lockKey(pkey), legacy))

	ok, tr, err := m.AcquireForPlan(ctx, pkey, 1, "u", "alice")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, StatePlanStale, tr.From)
	assert.Equal(t, StatePlanStale, tr.To)

	raw, err := mr.Get(lockKey(pkey))
	require.NoError(t, err)
	assert.Contains(t, raw, `"state":"plan_stale"`, "the state is now written explicitly")
}

func TestApply_RecordsThePlanAndMovesToPlanReady(t *testing.T) {
	ctx := context.Background()
	m, _ := newTestManager(t)
	_, _, err := m.AcquireForPlan(ctx, pkey, 1, "url", "alice")
	require.NoError(t, err)

	tr, err := m.Apply(ctx, pkey, 1, EventPlanApplicable, &PlanRecord{Args: []string{"-l", "name=web"}})
	require.NoError(t, err)
	assert.Equal(t, StatePlanReady, tr.To)

	plan, err := m.GetPlan(ctx, pkey, 1)
	require.NoError(t, err)
	assert.Equal(t, []string{"-l", "name=web"}, plan.Args)
}

func TestApply_ReleaseAndHoldDependOnWhatWasEstablished(t *testing.T) {
	ctx := context.Background()

	t.Run("a failed plan with nothing established releases", func(t *testing.T) {
		m, _ := newTestManager(t)
		_, _, err := m.AcquireForPlan(ctx, pkey, 1, "url", "alice")
		require.NoError(t, err)

		tr, err := m.Apply(ctx, pkey, 1, EventPlanFailed, nil)
		require.NoError(t, err)
		assert.True(t, tr.Released)

		status, err := m.GetLockStatus(ctx, pkey)
		require.NoError(t, err)
		assert.False(t, status.Locked)
	})

	t.Run("a failed plan after one succeeded keeps the Lock", func(t *testing.T) {
		m, _ := newTestManager(t)
		_, _, err := m.AcquireForPlan(ctx, pkey, 1, "url", "alice")
		require.NoError(t, err)
		_, err = m.Apply(ctx, pkey, 1, EventPlanApplicable, &PlanRecord{})
		require.NoError(t, err)
		_, _, err = m.AcquireForPlan(ctx, pkey, 1, "url", "alice") // the push
		require.NoError(t, err)

		tr, err := m.Apply(ctx, pkey, 1, EventPlanFailed, nil)
		require.NoError(t, err)
		assert.False(t, tr.Released, "a typo must not evict an author who had a working plan")
		assert.Equal(t, StatePlanStale, tr.To)

		status, err := m.GetLockStatus(ctx, pkey)
		require.NoError(t, err)
		assert.True(t, status.Locked)
	})
}

// Requirement 1.7: closing a pull request mid-apply deletes the Lock, and
// the Operation's result still has to reach the reader.
func TestApply_MissingLockIsANoOpNotAnError(t *testing.T) {
	ctx := context.Background()
	m, _ := newTestManager(t)

	tr, err := m.Apply(ctx, pkey, 1, EventMutatingSucceeded, nil)
	require.NoError(t, err, "a vanished Lock is an expected race, not a failure")
	assert.False(t, tr.Released)
	assert.Empty(t, tr.From)
}

func TestApply_OtherPRsLockIsAnError(t *testing.T) {
	ctx := context.Background()
	m, _ := newTestManager(t)
	_, _, err := m.AcquireForPlan(ctx, pkey, 1, "url", "alice")
	require.NoError(t, err)

	_, err = m.Apply(ctx, pkey, 2, EventMutatingSucceeded, nil)
	assert.ErrorIs(t, err, ErrLockedByOtherPR)
}

// realAddr skips rather than fails, so a plain `go test ./...` needs no
// infrastructure. miniredis serialises every command, so it cannot fail
// the way a real instance would — which is the whole point of this file's
// last two tests.
func realAddr(t *testing.T) string {
	t.Helper()
	addr := os.Getenv("TURNIP_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("TURNIP_TEST_REDIS_ADDR not set; skipping real-Redis atomicity test")
	}
	return addr
}

func realManager(t *testing.T, key string) *RedisLockManager {
	t.Helper()
	client := redis.NewClient(&redis.Options{Addr: realAddr(t)})
	t.Cleanup(func() {
		_ = client.Del(context.Background(), lockKey(key)).Err()
		_ = client.Close()
	})
	require.NoError(t, client.Del(context.Background(), lockKey(key)).Err())
	return NewRedisLockManager(client)
}

// Exactly one concurrent release wins. Everything else must see the Lock
// already gone and report no transition — never a second release.
func TestApply_ConcurrentReleaseHasOneWinner(t *testing.T) {
	ctx := context.Background()
	key := pkey + "/concurrent-release"
	m := realManager(t, key)

	_, _, err := m.AcquireForPlan(ctx, key, 1, "url", "alice")
	require.NoError(t, err)
	_, err = m.Apply(ctx, key, 1, EventPlanApplicable, &PlanRecord{})
	require.NoError(t, err)

	const n = 12
	var wg sync.WaitGroup
	var mu sync.Mutex
	released := 0
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tr, err := m.Apply(ctx, key, 1, EventMutatingSucceeded, nil)
			assert.NoError(t, err)
			mu.Lock()
			defer mu.Unlock()
			if tr.Released {
				released++
			}
		}()
	}
	wg.Wait()

	assert.Equal(t, 1, released, "exactly one caller may release a Lock")
}

// Concurrent dispatches against a PlanReady Lock: the state must end
// stale, and exactly one caller may observe the PlanReady-to-PlanStale
// edge. A second observer would mean two Operations both believed they
// were the one that superseded the stored plan.
func TestAcquireForPlan_ConcurrentDispatchSupersedesOnce(t *testing.T) {
	ctx := context.Background()
	key := pkey + "/concurrent-dispatch"
	m := realManager(t, key)

	_, _, err := m.AcquireForPlan(ctx, key, 1, "url", "alice")
	require.NoError(t, err)
	_, err = m.Apply(ctx, key, 1, EventPlanApplicable, &PlanRecord{})
	require.NoError(t, err)

	const n = 12
	var wg sync.WaitGroup
	var mu sync.Mutex
	superseded := 0
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, tr, err := m.AcquireForPlan(ctx, key, 1, "url", "alice")
			assert.NoError(t, err)
			assert.True(t, ok)
			mu.Lock()
			defer mu.Unlock()
			if tr.From == StatePlanReady && tr.To == StatePlanStale {
				superseded++
			}
		}()
	}
	wg.Wait()

	assert.Equal(t, 1, superseded, "only one dispatch may supersede the stored plan")

	status, err := m.GetLockStatus(ctx, key)
	require.NoError(t, err)
	assert.Equal(t, StatePlanStale, status.State)
}
