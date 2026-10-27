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

func TestRecordStore_CreateSetsTwentyFiveHourExpiry(t *testing.T) {
	s, client := newTestRecordStore(t)
	ctx := context.Background()
	require.NoError(t, s.Create(ctx, testRecord("op-1")))

	ttl, err := client.TTL(ctx, operationKey("op-1")).Result()
	require.NoError(t, err)
	assert.Greater(t, ttl, 24*time.Hour)
	assert.LessOrEqual(t, ttl, 25*time.Hour)
}

func TestRecordStore_TriggerFieldsRoundTrip(t *testing.T) {
	s, _ := newTestRecordStore(t)
	ctx := context.Background()
	rec := testRecord("op-1")
	rec.TriggerID = "trig-1"
	rec.CommandIndex = 2
	require.NoError(t, s.Create(ctx, rec))

	got, claimed, err := s.ClaimForResult(ctx, "op-1")
	require.NoError(t, err)
	require.True(t, claimed)
	assert.Equal(t, "trig-1", got.TriggerID)
	assert.Equal(t, 2, got.CommandIndex)
}

// TestRecordStore_ClaimModes_AgainstRecordStates covers every claim mode
// against every record state.
func TestRecordStore_ClaimModes_AgainstRecordStates(t *testing.T) {
	now := time.Now()
	states := []struct {
		name   string
		setup  func(rec *OperationRecord)
		start  bool
		gone   bool
		result bool
		timout bool
		stop   bool
		aband  bool
	}{
		{name: "not started, past start deadline", setup: func(r *OperationRecord) {
			r.StartDeadline = now.Add(-time.Minute).Unix()
		}, result: true, timout: true},
		{name: "not started, 25 hours old", setup: func(r *OperationRecord) {
			r.StartDeadline = now.Add(-25 * time.Hour).Unix()
			r.CreatedAt = now.Add(-25 * time.Hour)
		}, result: true, timout: true},
		{name: "started, 23 hours old", start: true, setup: func(r *OperationRecord) {
			r.CreatedAt = now.Add(-23 * time.Hour)
		}, result: true, stop: true},
		{name: "started, 24 hours old", start: true, setup: func(r *OperationRecord) {
			r.CreatedAt = now.Add(-24 * time.Hour)
		}, result: true, stop: true, aband: true},
		{name: "gone", gone: true},
	}
	modes := []struct {
		name  string
		claim func(s *recordStore, ctx context.Context) (bool, error)
		want  func(i int) bool
	}{
		{"result", func(s *recordStore, ctx context.Context) (bool, error) {
			_, c, err := s.ClaimForResult(ctx, "op-1")
			return c, err
		}, func(i int) bool { return states[i].result }},
		{"timeout", func(s *recordStore, ctx context.Context) (bool, error) {
			_, c, err := s.ClaimForTimeout(ctx, "op-1", now)
			return c, err
		}, func(i int) bool { return states[i].timout }},
		{"stopped", func(s *recordStore, ctx context.Context) (bool, error) {
			_, c, err := s.ClaimForStopped(ctx, "op-1")
			return c, err
		}, func(i int) bool { return states[i].stop }},
		{"abandon", func(s *recordStore, ctx context.Context) (bool, error) {
			_, c, err := s.ClaimForAbandon(ctx, "op-1", now)
			return c, err
		}, func(i int) bool { return states[i].aband }},
	}
	for i, st := range states {
		for _, m := range modes {
			t.Run(st.name+"/"+m.name, func(t *testing.T) {
				s, _ := newTestRecordStore(t)
				ctx := context.Background()
				if !st.gone {
					rec := testRecord("op-1")
					st.setup(rec)
					require.NoError(t, s.Create(ctx, rec))
					if st.start {
						_, _, err := s.MarkStarted(ctx, "op-1")
						require.NoError(t, err)
					}
				}
				claimed, err := m.claim(s, ctx)
				require.NoError(t, err)
				assert.Equal(t, m.want(i), claimed)
			})
		}
	}
}

func TestRecordStore_Claim_FinalizedRefusedByEveryMode(t *testing.T) {
	now := time.Now()
	s, _ := newTestRecordStore(t)
	ctx := context.Background()
	rec := testRecord("op-1")
	rec.CreatedAt = now.Add(-30 * time.Hour)
	rec.StartDeadline = now.Add(-time.Hour).Unix()
	require.NoError(t, s.Create(ctx, rec))
	_, _, err := s.MarkStarted(ctx, "op-1")
	require.NoError(t, err)
	_, claimed, err := s.ClaimForAbandon(ctx, "op-1", now)
	require.NoError(t, err)
	require.True(t, claimed)

	_, claimed, err = s.ClaimForResult(ctx, "op-1")
	require.NoError(t, err)
	assert.False(t, claimed)
	_, claimed, err = s.ClaimForTimeout(ctx, "op-1", now)
	require.NoError(t, err)
	assert.False(t, claimed)
	_, claimed, err = s.ClaimForStopped(ctx, "op-1")
	require.NoError(t, err)
	assert.False(t, claimed)
	_, claimed, err = s.ClaimForAbandon(ctx, "op-1", now)
	require.NoError(t, err)
	assert.False(t, claimed)
}

// startedRecord creates a started record op-1 in s.
func startedRecord(t *testing.T, s *recordStore) {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, s.Create(ctx, testRecord("op-1")))
	_, _, err := s.MarkStarted(ctx, "op-1")
	require.NoError(t, err)
}

// A Runner told to stop may report just as the sweep finds its Job
// ended: whichever claims first finalizes, and the other is refused.
func TestRecordStore_ClaimForStopped_LosesToResult(t *testing.T) {
	s, _ := newTestRecordStore(t)
	ctx := context.Background()
	startedRecord(t, s)

	_, claimed, err := s.ClaimForResult(ctx, "op-1")
	require.NoError(t, err)
	require.True(t, claimed)

	rec, claimed, err := s.ClaimForStopped(ctx, "op-1")
	require.NoError(t, err)
	assert.False(t, claimed)
	assert.Nil(t, rec)
}

func TestRecordStore_ClaimForStopped_ResultLosesAfterIt(t *testing.T) {
	s, _ := newTestRecordStore(t)
	ctx := context.Background()
	startedRecord(t, s)

	rec, claimed, err := s.ClaimForStopped(ctx, "op-1")
	require.NoError(t, err)
	require.True(t, claimed)
	assert.Equal(t, "op-1", rec.OperationID)

	_, claimed, err = s.ClaimForResult(ctx, "op-1")
	require.NoError(t, err)
	assert.False(t, claimed)
}

// Two replicas, and a result, racing for the same started record: exactly
// one of them claims it.
func TestRecordStore_ClaimForStopped_ExactlyOneWinsTheRace(t *testing.T) {
	for range 20 {
		s, _ := newTestRecordStore(t)
		ctx := context.Background()
		startedRecord(t, s)

		claims := []func() (bool, error){
			func() (bool, error) { _, c, err := s.ClaimForStopped(ctx, "op-1"); return c, err },
			func() (bool, error) { _, c, err := s.ClaimForStopped(ctx, "op-1"); return c, err },
			func() (bool, error) { _, c, err := s.ClaimForResult(ctx, "op-1"); return c, err },
		}
		var (
			wg   sync.WaitGroup
			mu   sync.Mutex
			wins int
		)
		for _, claim := range claims {
			wg.Add(1)
			go func() {
				defer wg.Done()
				c, err := claim()
				assert.NoError(t, err)
				if c {
					mu.Lock()
					wins++
					mu.Unlock()
				}
			}()
		}
		wg.Wait()
		require.Equal(t, 1, wins)
	}
}
