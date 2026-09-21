package lock

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// DecodedState is the only place an unrecognised state is interpreted, so
// it is the only place the fail-safe direction can be got wrong.
func TestDecodedState_TolerantOfUnrecognisedValues(t *testing.T) {
	for _, tc := range []struct {
		name  string
		field LockState
		want  LockState
	}{
		{"planning round-trips", StatePlanning, StatePlanning},
		{"plan_ready round-trips", StatePlanReady, StatePlanReady},
		{"plan_stale round-trips", StatePlanStale, StatePlanStale},
		{"absent — written before states existed", "", StatePlanStale},
		{"a value this build does not know", LockState("applying"), StatePlanStale},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, LockData{State: tc.field}.DecodedState())
		})
	}
}

// The pre-upgrade Lock that matters is the one carrying a plan: reading
// has_plan to decide the state would promote it to PlanReady and apply a
// plan whose validity this build cannot vouch for.
func TestGetLockStatus_LegacyLockWithAPlanIsNotPlanReady(t *testing.T) {
	ctx := context.Background()
	m, mr := newTestManager(t)
	const projectKey = "owner/repo/project"

	legacy := `{"pr_number":1,"pull_request_url":"https://example.com/pr/1","locked_by":"alice","has_plan":true,"plan_data":"cGxhbg=="}`
	require.NoError(t, mr.Set(lockKey(projectKey), legacy))

	status, err := m.GetLockStatus(ctx, projectKey)
	require.NoError(t, err)

	assert.True(t, status.Locked, "a pre-upgrade lock is still a held lock")
	assert.NotEqual(t, StatePlanReady, status.State,
		"a pre-upgrade lock must never be promoted to appliable")
	assert.Equal(t, StatePlanStale, status.State,
		"and must not be Planning either, which would license releasing it on a failed plan")
}

// A Lock's state has to survive Redis, not merely exist in Go.
func TestLockState_RoundTripsThroughRedis(t *testing.T) {
	ctx := context.Background()
	m, _ := newTestManager(t)
	const projectKey = "owner/repo/project"

	ok, err := acquireLock(ctx, m, projectKey, 1, "https://example.com/pr/1", "alice")
	require.NoError(t, err)
	require.True(t, ok)

	status, err := m.GetLockStatus(ctx, projectKey)
	require.NoError(t, err)
	assert.Equal(t, StatePlanning, status.State, "a newly created Lock has established nothing")

	require.NoError(t, storePlan(ctx, m, projectKey, 1, PlanRecord{Args: []string{"-l", "name=web"}}))

	status, err = m.GetLockStatus(ctx, projectKey)
	require.NoError(t, err)
	assert.Equal(t, StatePlanReady, status.State, "a recorded plan makes the Lock appliable")
}

// Every row of the design's transition table, written out rather than
// derived, so that the test disagrees with the implementation when the
// implementation changes.
func TestApplyEvent_TheWholeTable(t *testing.T) {
	for _, tc := range []struct {
		from     LockState
		event    Event
		to       LockState
		released bool
	}{
		{StatePlanning, EventPlanDispatched, StatePlanning, false},
		{StatePlanning, EventPlanApplicable, StatePlanReady, false},
		{StatePlanning, EventPlanNothingToApply, "", true},
		{StatePlanning, EventPlanFailed, "", true},
		{StatePlanning, EventPlanTimedOut, StatePlanning, false},
		{StatePlanning, EventMutatingSucceeded, "", true},
		{StatePlanning, EventMutatingFailed, StatePlanStale, false},
		{StatePlanning, EventMutatingTimedOut, StatePlanStale, false},
		{StatePlanning, EventUnlocked, "", true},
		{StatePlanning, EventPullRequestClosed, "", true},

		{StatePlanReady, EventPlanDispatched, StatePlanStale, false},
		{StatePlanReady, EventMutatingSucceeded, "", true},
		{StatePlanReady, EventMutatingFailed, StatePlanStale, false},
		{StatePlanReady, EventMutatingTimedOut, StatePlanStale, false},
		{StatePlanReady, EventUnlocked, "", true},
		{StatePlanReady, EventPullRequestClosed, "", true},

		{StatePlanStale, EventPlanDispatched, StatePlanStale, false},
		{StatePlanStale, EventPlanApplicable, StatePlanReady, false},
		{StatePlanStale, EventPlanNothingToApply, "", true},
		{StatePlanStale, EventPlanFailed, StatePlanStale, false},
		{StatePlanStale, EventPlanTimedOut, StatePlanStale, false},
		{StatePlanStale, EventMutatingSucceeded, "", true},
		{StatePlanStale, EventMutatingFailed, StatePlanStale, false},
		{StatePlanStale, EventMutatingTimedOut, StatePlanStale, false},
		{StatePlanStale, EventUnlocked, "", true},
		{StatePlanStale, EventPullRequestClosed, "", true},
	} {
		t.Run(string(tc.from)+"/"+string(tc.event), func(t *testing.T) {
			got, ok := ApplyEvent(tc.from, tc.event)
			require.True(t, ok, "this combination must be defined")
			assert.Equal(t, tc.from, got.From)
			assert.Equal(t, tc.released, got.Released, "whether the Lock survives")
			assert.Equal(t, tc.to, got.To, "the state arrived at")
		})
	}
}

// The four absent cells. A plan result cannot reach StatePlanReady in an
// ordinary sequence, because dispatching that plan moved the Lock to
// StatePlanStale first.
//
// This is the test that catches the dispatch edge regressing from
// dispatch-time to result-time: make that change and these combinations
// become reachable, but nothing else in the suite notices — the window it
// silently reopens is the one where an apply runs against an unreviewed
// HEAD.
func TestApplyEvent_PlanResultsCannotReachPlanReady(t *testing.T) {
	for _, ev := range []Event{
		EventPlanApplicable, EventPlanNothingToApply, EventPlanFailed, EventPlanTimedOut,
	} {
		t.Run(string(ev), func(t *testing.T) {
			got, ok := ApplyEvent(StatePlanReady, ev)

			assert.False(t, ok, "a plan result in PlanReady is not an expected combination")
			assert.False(t, got.Released, "an unexpected combination must never release a Lock")
			assert.Equal(t, StatePlanReady, got.To, "nor move it")
		})
	}
}

// Every combination is either defined or one of the four known-absent
// ones. Adding a state or an event then forces a decision here rather than
// silently landing in the "not expected" branch, where it would quietly
// change nothing at runtime.
func TestApplyEvent_NoCombinationIsUndecided(t *testing.T) {
	absent := map[edge]bool{
		{StatePlanReady, EventPlanApplicable}:     true,
		{StatePlanReady, EventPlanNothingToApply}: true,
		{StatePlanReady, EventPlanFailed}:         true,
		{StatePlanReady, EventPlanTimedOut}:       true,
	}

	for _, from := range AllStates() {
		for _, ev := range AllEvents() {
			_, ok := ApplyEvent(from, ev)
			if absent[edge{from, ev}] {
				assert.False(t, ok, "%s/%s is deliberately absent", from, ev)
				continue
			}
			assert.True(t, ok, "%s/%s has no entry — decide it rather than leaving it undefined", from, ev)
		}
	}
}

// Releasing and moving are mutually exclusive: a released Lock has no
// state to be in, because it has no key.
func TestApplyEvent_AReleasedLockHasNoState(t *testing.T) {
	for _, from := range AllStates() {
		for _, ev := range AllEvents() {
			got, ok := ApplyEvent(from, ev)
			if ok && got.Released {
				assert.Empty(t, got.To, "%s/%s released, so To must be empty", from, ev)
			}
		}
	}
}
