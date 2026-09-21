package orchestrator

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivanvc/turnip/internal/config"
	"github.com/ivanvc/turnip/internal/github"
	"github.com/ivanvc/turnip/internal/lock"
	"github.com/ivanvc/turnip/internal/plugin"
	"github.com/ivanvc/turnip/internal/rpc"
)

// Classification is where the orchestrator's half of the state machine
// lives: everything after it is the table, which internal/lock tests
// exhaustively.
func TestLockEventFor(t *testing.T) {
	changes := plugin.ChangeSummary{Change: 1}
	none := plugin.ChangeSummary{}

	for _, tc := range []struct {
		name      string
		tool      string
		operation string
		success   bool
		changes   plugin.ChangeSummary
		want      lock.Event
		wantPlan  bool
	}{
		{"plan with changes", "helmfile", "diff", true, changes, lock.EventPlanApplicable, true},
		{"plan with none, tool acts anyway", "helmfile", "diff", true, none, lock.EventPlanApplicable, true},
		{"plan with none, tool is inert", "pulumi", "preview", true, none, lock.EventPlanNothingToApply, false},
		{"plan with changes, inert tool", "pulumi", "preview", true, changes, lock.EventPlanApplicable, true},
		{"plan failed", "helmfile", "diff", false, none, lock.EventPlanFailed, false},
		{"apply succeeded", "helmfile", "apply", true, none, lock.EventMutatingSucceeded, false},
		{"apply failed", "helmfile", "apply", false, none, lock.EventMutatingFailed, false},
		{"sync succeeded", "helmfile", "sync", true, none, lock.EventMutatingSucceeded, false},
		// An unregistered tool is treated as mutating: a plan classification
		// could release a Lock for a Plugin turnip cannot identify.
		{"unregistered tool, success", "terraform", "plan", true, none, lock.EventMutatingSucceeded, false},
		{"unregistered tool, failure", "terraform", "plan", false, none, lock.EventMutatingFailed, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := &Orchestrator{plugins: testRegistry()}
			rec := &OperationRecord{
				Project:   config.Project{Name: "p", Tool: tc.tool},
				Operation: tc.operation,
				ExtraArgs: []string{"-l", "name=web"},
			}

			ev, planRec := o.lockEventFor(rec, tc.success, tc.changes, []byte("bytes"))

			assert.Equal(t, tc.want, ev)
			if !tc.wantPlan {
				assert.Nil(t, planRec, "only an applicable plan is recorded")
				return
			}
			require.NotNil(t, planRec)
			assert.Equal(t, []string{"-l", "name=web"}, planRec.Args,
				"the scope stored is the Operation's own, not the trigger line re-parsed")
		})
	}
}

// "pulumi" in testRegistry answers false, which is the only way to reach
// the release edge — Helmfile answers true because of sync.
func TestHandleResult_NoChangePlanReleasesOnlyForAnInertTool(t *testing.T) {
	for _, tc := range []struct {
		name        string
		tool        string
		operation   string
		wantEvent   lock.Event
		wantLocked  bool
		wantNoteHas string
	}{
		{"inert tool releases", "pulumi", "preview", lock.EventPlanNothingToApply, false, "nothing to apply"},
		{"helmfile keeps it, because sync acts anyway", "helmfile", "diff", lock.EventPlanApplicable, true, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			locks := &fakeLockManager{}
			o, _ := testResultOrchestrator(t, locks)
			rec := &OperationRecord{
				OperationID: "op-1",
				ProjectKey:  "owner/repo/p",
				Project:     config.Project{Name: "p", Tool: tc.tool},
				Owner:       "owner", Repo: "repo", PRNumber: 42,
				Operation:     tc.operation,
				StartDeadline: time.Now().Add(time.Hour).Unix(),
				CreatedAt:     time.Now(),
			}
			require.NoError(t, o.records.Create(context.Background(), rec))

			require.NoError(t, o.HandleResult(context.Background(), "op-1", rpc.OperationResult{Success: true}))

			assert.Equal(t, []lock.Event{tc.wantEvent}, locks.appliedEvents())
		})
	}
}

// The live defect this slice fixes: a timed-out Operation holds its Lock,
// and reportTimeout never said so — Locked defaulted to false, so the
// Project vanished from the footer and was never offered unlock.
func TestReportTimeout_ReportsTheLockAsHeld(t *testing.T) {
	for _, tc := range []struct {
		name      string
		operation string
		wantNote  bool
	}{
		{"a timed-out plan changes nothing but stays held", "diff", false},
		{"a timed-out apply also invalidates the plan", "apply", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			jobsClient := &fakeJobCreator{t: t}
			o, _ := testSweepOrchestrator(t, jobsClient)

			rec := sweepTestRecord("op-1", time.Now().Add(-time.Minute), false)
			rec.Operation = tc.operation
			require.NoError(t, o.records.Create(context.Background(), rec))

			resultCh := make(chan github.ProjectResult, 1)
			go func() {
				r, err := waitForDone(context.Background(), o.redis, "op-1")
				assert.NoError(t, err)
				resultCh <- r
			}()
			require.Eventually(t, func() bool {
				return o.redis.PubSubNumSub(context.Background(), doneChannel("op-1")).Val()[doneChannel("op-1")] > 0
			}, time.Second, time.Millisecond)

			o.sweepOnce(context.Background())

			select {
			case r := <-resultCh:
				assert.True(t, r.Locked,
					"a timed-out Operation leaves its Lock held, and the comment must say so")
				if tc.wantNote {
					assert.Contains(t, r.LockNote, "Re-plan",
						"a timed-out apply may still be running, so the plan is no longer usable")
				}
			case <-time.After(2 * time.Second):
				t.Fatal("timed out waiting for published timeout result")
			}
		})
	}
}

// The three sequences this slice was written for, end to end through
// executeOne's admission rather than through the table directly.
func TestAdmission_AStalePlanIsRefused(t *testing.T) {
	for _, tc := range []struct {
		name        string
		state       lock.LockState
		wantRefusal string
	}{
		{
			name:        "after a push superseded the plan",
			state:       lock.StatePlanStale,
			wantRefusal: "no longer valid",
		},
		{
			name:        "after a failed apply left infrastructure part-changed",
			state:       lock.StatePlanStale,
			wantRefusal: "Re-plan before retrying",
		},
		{
			name:        "when nothing was ever planned",
			state:       lock.StatePlanning,
			wantRefusal: "no plan recorded",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			locks := &fakeLockManager{
				getLockStatusFunc: func(ctx context.Context, projectKey string) (*lock.LockStatus, error) {
					return &lock.LockStatus{Locked: true, PRNumber: 42, State: tc.state}, nil
				},
			}
			o, _ := testResultOrchestrator(t, locks)
			o.plugins = testRegistry()

			res := o.executeOne(context.Background(), &fakeExecuteClient{},
				github.Repository{Owner: "owner", Name: "repo"},
				github.PullRequest{Number: 42, HeadSHA: "abc"},
				1,
				Target{Project: config.Project{Name: "helm-a", Directory: "a", Tool: "helmfile"}, Operation: "apply"},
			)

			assert.False(t, res.Success)
			assert.Contains(t, res.Output, tc.wantRefusal)
			assert.Empty(t, locks.appliedEvents(),
				"a refused Operation must not move the Lock it was refused by")
		})
	}
}

// A Lock written before states existed must ask for a re-plan rather than
// be promoted to appliable — the single edge where a wrong default applies
// something nobody reviewed.
func TestAdmission_APreUpgradeLockIsRefused(t *testing.T) {
	locks := &fakeLockManager{
		getLockStatusFunc: func(ctx context.Context, projectKey string) (*lock.LockStatus, error) {
			// What GetLockStatus reports for a stored value with no state
			// field: DecodedState maps it to PlanStale.
			return &lock.LockStatus{Locked: true, PRNumber: 42, State: lock.LockData{}.DecodedState()}, nil
		},
	}
	o, _ := testResultOrchestrator(t, locks)
	o.plugins = testRegistry()

	res := o.executeOne(context.Background(), &fakeExecuteClient{},
		github.Repository{Owner: "owner", Name: "repo"},
		github.PullRequest{Number: 42, HeadSHA: "abc"},
		1,
		Target{Project: config.Project{Name: "helm-a", Directory: "a", Tool: "helmfile"}, Operation: "apply"},
	)

	assert.False(t, res.Success)
	assert.Contains(t, res.Output, "no longer valid")
}

// Requirement 7.4: a transition that failed left the Lock where it was, so
// the comment must report it as held and say nothing. No happy-path row
// reaches this, and the tempting implementation — compose the message
// beside the decision, then perform it — is wrong in the one direction
// that matters.
func TestHandleResult_AFailedTransitionAnnouncesNothing(t *testing.T) {
	locks := &fakeLockManager{
		applyFunc: func(ctx context.Context, projectKey string, prNumber int, ev lock.Event, plan *lock.PlanRecord) (lock.Transition, error) {
			return lock.Transition{}, assert.AnError
		},
	}
	o, _ := testResultOrchestrator(t, locks)
	rec := &OperationRecord{
		OperationID: "op-1",
		ProjectKey:  "owner/repo/p",
		Project:     config.Project{Name: "p", Tool: "helmfile"},
		Owner:       "owner", Repo: "repo", PRNumber: 42,
		Operation:     "apply",
		StartDeadline: time.Now().Add(time.Hour).Unix(),
		CreatedAt:     time.Now(),
	}
	require.NoError(t, o.records.Create(context.Background(), rec))

	resultCh := make(chan github.ProjectResult, 1)
	go func() {
		r, err := waitForDone(context.Background(), o.redis, "op-1")
		assert.NoError(t, err)
		resultCh <- r
	}()
	require.Eventually(t, func() bool {
		return o.redis.PubSubNumSub(context.Background(), doneChannel("op-1")).Val()[doneChannel("op-1")] > 0
	}, time.Second, time.Millisecond)

	require.NoError(t, o.HandleResult(context.Background(), "op-1", rpc.OperationResult{Success: true}))

	select {
	case r := <-resultCh:
		assert.True(t, r.Locked, "a release that errored left the Lock held; say so")
		assert.Empty(t, r.LockNote, "and announce nothing, because nothing happened")
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for published result")
	}
}
