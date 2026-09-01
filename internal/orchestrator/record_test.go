package orchestrator

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivanvc/turnip/internal/config"
)

func newTestRecordStore(t *testing.T) (*recordStore, *redis.Client) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr(), MaxRetries: -1})
	t.Cleanup(func() { _ = client.Close() })
	return newRecordStore(client), client
}

func testRecord(operationID string) *OperationRecord {
	return &OperationRecord{
		OperationID:   operationID,
		ProjectKey:    "owner/repo/project",
		Project:       config.Project{Name: "project", Tool: "helmfile"},
		Owner:         "owner",
		Repo:          "repo",
		PRNumber:      42,
		Operation:     "diff",
		StartDeadline: time.Now().Add(5 * time.Minute).Unix(),
		CreatedAt:     time.Now(),
	}
}

func TestRecordStore_CreateAndDelete(t *testing.T) {
	s, _ := newTestRecordStore(t)
	ctx := context.Background()

	require.NoError(t, s.Create(ctx, testRecord("op-1")))
	rec, err := s.get(ctx, "op-1")
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Equal(t, "owner/repo/project", rec.ProjectKey)

	require.NoError(t, s.Delete(ctx, "op-1"))
	rec, err = s.get(ctx, "op-1")
	require.NoError(t, err)
	assert.Nil(t, rec)
}

func TestRecordStore_SetJobNameAndCheckRunID(t *testing.T) {
	s, _ := newTestRecordStore(t)
	ctx := context.Background()
	require.NoError(t, s.Create(ctx, testRecord("op-1")))

	require.NoError(t, s.SetJobName(ctx, "op-1", "turnip-runner-abc"))
	require.NoError(t, s.SetCheckRunID(ctx, "op-1", 999))

	rec, err := s.get(ctx, "op-1")
	require.NoError(t, err)
	assert.Equal(t, "turnip-runner-abc", rec.JobName)
	assert.EqualValues(t, 999, rec.CheckRunID)
}

func TestRecordStore_SetJobName_SurvivesTTL(t *testing.T) {
	s, client := newTestRecordStore(t)
	ctx := context.Background()
	require.NoError(t, s.Create(ctx, testRecord("op-1")))

	ttlBefore, err := client.TTL(ctx, operationKey("op-1")).Result()
	require.NoError(t, err)
	require.NoError(t, s.SetJobName(ctx, "op-1", "turnip-runner-abc"))
	ttlAfter, err := client.TTL(ctx, operationKey("op-1")).Result()
	require.NoError(t, err)

	assert.Positive(t, ttlAfter)
	assert.InDelta(t, ttlBefore.Seconds(), ttlAfter.Seconds(), 2)
}

func TestRecordStore_MarkStarted_Idempotent(t *testing.T) {
	s, _ := newTestRecordStore(t)
	ctx := context.Background()
	rec := testRecord("op-1")
	require.NoError(t, s.Create(ctx, rec))

	justStarted, createdAt, err := s.MarkStarted(ctx, "op-1")
	require.NoError(t, err)
	assert.True(t, justStarted)
	assert.WithinDuration(t, rec.CreatedAt, createdAt, time.Second)

	justStarted, _, err = s.MarkStarted(ctx, "op-1") // second call: harmless no-op
	require.NoError(t, err)
	assert.False(t, justStarted)

	got, err := s.get(ctx, "op-1")
	require.NoError(t, err)
	assert.True(t, got.Started)
}

func TestRecordStore_MarkStarted_MissingRecordIsNotAnError(t *testing.T) {
	s, _ := newTestRecordStore(t)
	justStarted, _, err := s.MarkStarted(context.Background(), "does-not-exist")
	require.NoError(t, err)
	assert.False(t, justStarted)
}

func TestRecordStore_ClaimForResult_NotClaimedWhenMissing(t *testing.T) {
	s, _ := newTestRecordStore(t)
	rec, claimed, err := s.ClaimForResult(context.Background(), "does-not-exist")
	require.NoError(t, err)
	assert.False(t, claimed)
	assert.Nil(t, rec)
}

func TestRecordStore_ClaimForResult_NotClaimedWhenAlreadyFinalized(t *testing.T) {
	s, _ := newTestRecordStore(t)
	ctx := context.Background()
	require.NoError(t, s.Create(ctx, testRecord("op-1")))

	_, claimed, err := s.ClaimForResult(ctx, "op-1")
	require.NoError(t, err)
	require.True(t, claimed)

	_, claimed, err = s.ClaimForResult(ctx, "op-1")
	require.NoError(t, err)
	assert.False(t, claimed)
}

func TestRecordStore_ClaimForTimeout_NotClaimedWhenStarted(t *testing.T) {
	s, _ := newTestRecordStore(t)
	ctx := context.Background()
	rec := testRecord("op-1")
	rec.StartDeadline = time.Now().Add(-time.Minute).Unix() // already past
	require.NoError(t, s.Create(ctx, rec))
	_, _, err := s.MarkStarted(ctx, "op-1")
	require.NoError(t, err)

	_, claimed, err := s.ClaimForTimeout(ctx, "op-1", time.Now())
	require.NoError(t, err)
	assert.False(t, claimed)
}

func TestRecordStore_ClaimForTimeout_NotClaimedBeforeDeadline(t *testing.T) {
	s, _ := newTestRecordStore(t)
	ctx := context.Background()
	require.NoError(t, s.Create(ctx, testRecord("op-1"))) // deadline 5m from now

	_, claimed, err := s.ClaimForTimeout(ctx, "op-1", time.Now())
	require.NoError(t, err)
	assert.False(t, claimed)
}

func TestRecordStore_ClaimForTimeout_ClaimedAfterDeadline(t *testing.T) {
	s, _ := newTestRecordStore(t)
	ctx := context.Background()
	rec := testRecord("op-1")
	rec.StartDeadline = time.Now().Add(-time.Minute).Unix()
	require.NoError(t, s.Create(ctx, rec))

	got, claimed, err := s.ClaimForTimeout(ctx, "op-1", time.Now())
	require.NoError(t, err)
	require.True(t, claimed)
	assert.Equal(t, "op-1", got.OperationID)
}

func TestRecordStore_ClaimForResultAndClaimForTimeout_ExactlyOneWins(t *testing.T) {
	s, _ := newTestRecordStore(t)
	ctx := context.Background()
	rec := testRecord("op-1")
	rec.StartDeadline = time.Now().Add(-time.Minute).Unix()
	require.NoError(t, s.Create(ctx, rec))

	var wg sync.WaitGroup
	var mu sync.Mutex
	claims := 0

	wg.Add(2)
	go func() {
		defer wg.Done()
		_, claimed, err := s.ClaimForResult(ctx, "op-1")
		assert.NoError(t, err)
		if claimed {
			mu.Lock()
			claims++
			mu.Unlock()
		}
	}()
	go func() {
		defer wg.Done()
		_, claimed, err := s.ClaimForTimeout(ctx, "op-1", time.Now())
		assert.NoError(t, err)
		if claimed {
			mu.Lock()
			claims++
			mu.Unlock()
		}
	}()
	wg.Wait()

	assert.Equal(t, 1, claims)
}

func TestRecordStore_ScanOperationKeys(t *testing.T) {
	s, _ := newTestRecordStore(t)
	ctx := context.Background()
	require.NoError(t, s.Create(ctx, testRecord("op-1")))
	require.NoError(t, s.Create(ctx, testRecord("op-2")))
	require.NoError(t, s.Create(ctx, testRecord("op-3")))

	keys, err := s.ScanOperationKeys(ctx)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"operation:op-1", "operation:op-2", "operation:op-3"}, keys)
}
