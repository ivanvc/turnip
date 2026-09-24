package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivanvc/turnip/internal/lock"
)

var testRef = prRef{Owner: "owner", Repo: "repo", PRNumber: 42, HeadSHA: "abc123"}

func TestOutcomeForEvent(t *testing.T) {
	cases := map[lock.Event]Outcome{
		lock.EventPlanApplicable:     OutcomeAwaitingApply,
		lock.EventPlanNothingToApply: OutcomeNothingToApply,
		lock.EventPlanFailed:         OutcomeNotPlanned,
		lock.EventPlanTimedOut:       OutcomeNotPlanned,
		lock.EventMutatingSucceeded:  OutcomeApplied,
		lock.EventMutatingFailed:     OutcomeApplyFailed,
		lock.EventMutatingTimedOut:   OutcomeApplyFailed,
	}
	for ev, want := range cases {
		got, ok := outcomeForEvent(ev)
		assert.True(t, ok, ev)
		assert.Equal(t, want, got, ev)
	}

	// Lock-only events are not Operation outcomes. An unlock in particular
	// must never reach the record (Requirement 3.5).
	for _, ev := range []lock.Event{lock.EventPlanDispatched, lock.EventUnlocked, lock.EventPullRequestClosed} {
		_, ok := outcomeForEvent(ev)
		assert.False(t, ok, ev)
	}
}

func TestPRStatus_ConcurrentWritesForDifferentProjectsAllSurvive(t *testing.T) {
	store := newRecordStore(newTestRedisClient(t))
	ctx := context.Background()

	var wg sync.WaitGroup
	for i := range 20 {
		wg.Go(func() {
			assert.NoError(t, store.WriteOutcome(ctx, testRef, fmt.Sprintf("p%d", i), ProjectEntry{Outcome: OutcomeApplied, Operation: "apply"}))
		})
	}
	wg.Wait()

	st, err := store.ReadPRStatus(ctx, testRef)
	require.NoError(t, err)
	assert.Len(t, st.Projects, 20)
	assert.Equal(t, int64(20), st.Version, "every write bumps the version exactly once")
	assert.True(t, st.Mutated)
}

func TestPRStatus_LatestOutcomeReplacesTheProjectsPrevious(t *testing.T) {
	store := newRecordStore(newTestRedisClient(t))
	ctx := context.Background()

	require.NoError(t, store.WriteOutcome(ctx, testRef, "web", ProjectEntry{Outcome: OutcomeAwaitingApply, Operation: "diff"}))
	require.NoError(t, store.WriteOutcome(ctx, testRef, "api", ProjectEntry{Outcome: OutcomeAwaitingApply, Operation: "diff"}))
	require.NoError(t, store.WriteOutcome(ctx, testRef, "web", ProjectEntry{Outcome: OutcomeApplied, Operation: "sync"}))

	st, err := store.ReadPRStatus(ctx, testRef)
	require.NoError(t, err)
	assert.Equal(t, ProjectEntry{Outcome: OutcomeApplied, Operation: "sync"}, st.Projects["web"])
	assert.Equal(t, OutcomeAwaitingApply, st.Projects["api"].Outcome, "writing one Project leaves the others alone")
}

// Keying by commit is what makes a push start a fresh record (Requirement
// 3.4) and keeps a late result for the old commit out of the new one.
func TestPRStatus_DifferentCommitsNeverMeet(t *testing.T) {
	store := newRecordStore(newTestRedisClient(t))
	ctx := context.Background()
	newHead := testRef
	newHead.HeadSHA = "def456"

	require.NoError(t, store.WriteOutcome(ctx, testRef, "web", ProjectEntry{Outcome: OutcomeApplied}))

	st, err := store.ReadPRStatus(ctx, newHead)
	require.NoError(t, err)
	assert.Empty(t, st.Projects)
	assert.Zero(t, st.Version)
}

func TestPRStatus_TTLIsSetAndRefreshed(t *testing.T) {
	client := newTestRedisClient(t)
	store := newRecordStore(client)
	ctx := context.Background()

	require.NoError(t, store.WriteOutcome(ctx, testRef, "web", ProjectEntry{Outcome: OutcomeAwaitingApply}))
	require.NoError(t, client.Expire(ctx, prStatusKey(testRef), time.Minute).Err())

	require.NoError(t, store.MarkEmpty(ctx, testRef))
	assert.Equal(t, prStatusTTL, client.TTL(ctx, prStatusKey(testRef)).Val())
}

// The publisher's bookkeeping must not bump the version, or every publish
// would look like a change and publish again.
func TestPRStatus_CheckRunBookkeepingDoesNotBumpTheVersion(t *testing.T) {
	store := newRecordStore(newTestRedisClient(t))
	ctx := context.Background()

	require.NoError(t, store.WriteOutcome(ctx, testRef, "web", ProjectEntry{Outcome: OutcomeApplied}))
	require.NoError(t, store.SetAggregateCheckRun(ctx, testRef, 7, true))
	require.NoError(t, store.SetAggregateCheckDone(ctx, testRef, false))

	st, err := store.ReadPRStatus(ctx, testRef)
	require.NoError(t, err)
	assert.Equal(t, int64(1), st.Version)
	assert.Equal(t, int64(7), st.CheckRunID)
	assert.False(t, st.CheckDone)
}

func TestPRStatus_MetadataMarks(t *testing.T) {
	store := newRecordStore(newTestRedisClient(t))
	ctx := context.Background()

	require.NoError(t, store.MarkConfigInvalid(ctx, testRef))
	require.NoError(t, store.MarkEmpty(ctx, testRef))

	st, err := store.ReadPRStatus(ctx, testRef)
	require.NoError(t, err)
	assert.True(t, st.ConfigInvalid)
	assert.True(t, st.Empty)
	assert.False(t, st.Mutated)
	assert.Equal(t, int64(2), st.Version)
}

// An entry written before BlockedBy and Setting existed decodes as it
// did, and one without them is written as it was: both are omitempty
// (check-run-refusals Requirements 1.4, 3.3).
func TestProjectEntry_WithoutTheNewFieldsDecodesUnchanged(t *testing.T) {
	old := `{"outcome":"not_planned","operation":"diff","tool":"helmfile"}`

	var entry ProjectEntry
	require.NoError(t, json.Unmarshal([]byte(old), &entry))
	assert.Equal(t, ProjectEntry{Outcome: OutcomeNotPlanned, Operation: "diff", Tool: "helmfile"}, entry)

	written, err := json.Marshal(entry)
	require.NoError(t, err)
	assert.JSONEq(t, old, string(written))
	assert.NotContains(t, string(written), "blocked_by")
	assert.NotContains(t, string(written), "setting")
}

func TestPRStatus_BlockedByAndSettingRoundTrip(t *testing.T) {
	store := newRecordStore(newTestRedisClient(t))
	ctx := context.Background()

	blocked := ProjectEntry{Outcome: OutcomeNotPlanned, Operation: "diff", BlockedBy: 5}
	refused := ProjectEntry{Outcome: OutcomeRefused, Operation: "diff", Setting: "runner.serviceAccount"}
	require.NoError(t, store.WriteOutcome(ctx, testRef, "web", blocked))
	require.NoError(t, store.WriteOutcome(ctx, testRef, "api", refused))

	st, err := store.ReadPRStatus(ctx, testRef)
	require.NoError(t, err)
	assert.Equal(t, blocked, st.Projects["web"])
	assert.Equal(t, refused, st.Projects["api"])
	assert.False(t, st.Mutated, "a refusal ran nothing")
}
