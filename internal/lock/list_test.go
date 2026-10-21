package lock

import (
	"context"
	"testing"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListLocks(t *testing.T) {
	ctx := context.Background()
	m, mr := newTestManager(t)

	for _, k := range []string{"owner/api/web", "owner/api/gcp/web", "owner/api-v2/web", "owner/other/web"} {
		ok, err := acquireLock(ctx, m, k, 7, "https://example.test/pull/7", "alice")
		require.NoError(t, err)
		require.True(t, ok)
	}
	require.NoError(t, storePlan(ctx, m, "owner/api/web", 7, PlanRecord{}))

	got, err := m.ListLocks(ctx, "owner/api/")
	require.NoError(t, err)
	byKey := map[string]ListedLock{}
	for _, l := range got {
		byKey[l.ProjectKey] = l
	}
	require.Len(t, byKey, 2)
	assert.Equal(t, StatePlanReady, byKey["owner/api/web"].Status.State)
	assert.Equal(t, StatePlanning, byKey["owner/api/gcp/web"].Status.State)
	assert.True(t, byKey["owner/api/web"].Status.Locked)
	assert.Equal(t, 7, byKey["owner/api/web"].Status.PRNumber)
	assert.Equal(t, "alice", byKey["owner/api/web"].Status.LockedBy)

	// Glob characters in the prefix match literally.
	got, err = m.ListLocks(ctx, "owner/*/")
	require.NoError(t, err)
	assert.Empty(t, got)

	// A value that cannot be decoded fails the whole call.
	require.NoError(t, mr.Set("lock:owner/api/bad", "not json"))
	_, err = m.ListLocks(ctx, "owner/api/")
	require.Error(t, err)
}

func TestListLocks_SkipsKeyGoneAfterScan(t *testing.T) {
	ctx := context.Background()
	m, mr := newTestManager(t)
	for _, k := range []string{"owner/api/a", "owner/api/b"} {
		_, err := acquireLock(ctx, m, k, 1, "u", "bob")
		require.NoError(t, err)
	}

	// Delete a key after SCAN has returned it, before MGET reads it.
	first := true
	m.client.AddHook(deleteAfterScanHook{onMGet: func() {
		if first {
			first = false
			mr.Del("lock:owner/api/b")
		}
	}})

	got, err := m.ListLocks(ctx, "owner/api/")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "owner/api/a", got[0].ProjectKey)
}

func TestListLocks_ReadFailure(t *testing.T) {
	m, mr := newTestManager(t)
	_, err := acquireLock(context.Background(), m, "owner/api/a", 1, "u", "bob")
	require.NoError(t, err)
	mr.Close()
	_, err = m.ListLocks(context.Background(), "owner/api/")
	require.Error(t, err)
}

// deleteAfterScanHook runs onMGet just before an MGET goes out.
type deleteAfterScanHook struct{ onMGet func() }

func (h deleteAfterScanHook) DialHook(next redis.DialHook) redis.DialHook { return next }
func (h deleteAfterScanHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		if cmd.Name() == "mget" {
			h.onMGet()
		}
		return next(ctx, cmd)
	}
}
func (h deleteAfterScanHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}
