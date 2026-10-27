package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivanvc/turnip/internal/github"
	"github.com/ivanvc/turnip/internal/jobs"
	"github.com/ivanvc/turnip/internal/lock"
)

// errRedisDown is what redisFaults answers with in place of Redis.
var errRedisDown = errors.New("redis: connection refused")

// redisFaults is a go-redis hook that fails chosen commands before they
// reach Redis, and reports the ones that did. before sees a single command
// or a whole pipeline; a non-nil error is the reply and nothing is sent.
// after runs once a single command has returned.
type redisFaults struct {
	mu     sync.Mutex
	before func(cmds []redis.Cmder) error
	after  func(cmd redis.Cmder)
}

func (h *redisFaults) fail(cmds []redis.Cmder) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.before == nil {
		return nil
	}
	err := h.before(cmds)
	if err != nil {
		for _, cmd := range cmds {
			cmd.SetErr(err)
		}
	}
	return err
}

func (h *redisFaults) DialHook(next redis.DialHook) redis.DialHook { return next }

func (h *redisFaults) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		if err := h.fail([]redis.Cmder{cmd}); err != nil {
			return err
		}
		err := next(ctx, cmd)
		if h.after != nil {
			h.after(cmd)
		}
		return err
	}
}

func (h *redisFaults) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		if err := h.fail(cmds); err != nil {
			return err
		}
		return next(ctx, cmds)
	}
}

// cmdsMention reports whether a command named name among cmds has an
// argument containing text.
func cmdsMention(cmds []redis.Cmder, name, text string) bool {
	for _, cmd := range cmds {
		if cmd.Name() != name {
			continue
		}
		for _, arg := range cmd.Args() {
			if strings.Contains(fmt.Sprint(arg), text) {
				return true
			}
		}
	}
	return false
}

// failFirst fails the first n calls for which match holds, and counts
// every such call in *seen.
func failFirst(n int, seen *int, match func(cmds []redis.Cmder) bool) func([]redis.Cmder) error {
	return func(cmds []redis.Cmder) error {
		if !match(cmds) {
			return nil
		}
		*seen++
		if *seen <= n {
			return errRedisDown
		}
		return nil
	}
}

func isComplete(cmds []redis.Cmder) bool { return cmdsMention(cmds, "eval", "HINCRBY") }

// A stop that lands in the middle of a sweep tick does not skip the check
// and the Lock of an Operation the tick already claimed: nobody else will
// finalize it, and its trigger is carried forward either way.
func TestSweepOnce_StopAfterATimeoutClaimStillFinalizesTheOperation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// The first call a claimed timeout makes is the Job's status, so the
	// stop arrives right after the claim.
	jobsClient := &fakeJobCreator{t: t, statusFn: func(ctx context.Context, _ string) (*jobs.JobStatus, error) {
		cancel()
		return &jobs.JobStatus{JobFound: true, PodPhase: "Pending"}, ctx.Err()
	}}
	o, client := testSweepOrchestrator(t, jobsClient)
	var lockCtxErr error
	lockApplied := false
	o.locks = &fakeLockManager{applyFunc: func(ctx context.Context, _ string, _ int, _ lock.Event, _ *lock.PlanRecord) (lock.Transition, error) {
		lockApplied, lockCtxErr = true, ctx.Err()
		return lock.Transition{From: lock.StatePlanning, To: lock.StatePlanStale}, nil
	}}
	require.NoError(t, o.records.Create(context.Background(), sweepTestRecord("op-1", time.Now().Add(-time.Minute), false)))

	r := finalizedResult(t, o, "op-1", func() { o.sweepOnce(ctx) })

	require.Error(t, ctx.Err(), "the stop landed inside the tick")
	assert.Contains(t, r.Output, "Pod still Pending", "the Job's status is read after the stop")
	require.True(t, lockApplied)
	require.NoError(t, lockCtxErr, "the Lock event is applied on a context the stop does not cancel")
	assert.Equal(t, 1, client.updateCheckRunCalled)
	rec, err := o.records.get(context.Background(), "op-1")
	require.NoError(t, err)
	assert.Nil(t, rec, "the record is deleted, not left finalized until it expires")
}

func TestSweepOnce_StopAfterAnAbandonClaimStillDeletesTheRecord(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	o, _ := testSweepOrchestrator(t, runningJob(t))
	o.redis.AddHook(&redisFaults{after: func(cmd redis.Cmder) {
		// The claim's mode is an argument of its own; the script's text
		// names every mode.
		for _, arg := range cmd.Args() {
			if arg == "abandon" {
				cancel()
			}
		}
	}})
	require.NoError(t, o.records.Create(context.Background(), abandonableRecord("op-1", abandonAfter+time.Minute)))

	r := finalizedResult(t, o, "op-1", func() { o.sweepOnce(ctx) })

	require.Error(t, ctx.Err(), "the stop landed inside the tick")
	assert.Equal(t, abandonedOutput, r.Output)
	rec, err := o.records.get(context.Background(), "op-1")
	require.NoError(t, err)
	assert.Nil(t, rec, "the record is deleted, not left finalized until it expires")
}

func TestSweepOnce_StopAfterAStoppedClaimStillFinalizes(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	o, client := testSweepOrchestrator(t, jobWithStatus(t, &jobs.JobStatus{JobFound: false}))
	var lockCtxErr error
	o.locks = &fakeLockManager{applyFunc: func(ctx context.Context, _ string, _ int, _ lock.Event, _ *lock.PlanRecord) (lock.Transition, error) {
		lockCtxErr = ctx.Err()
		return lock.Transition{From: lock.StatePlanning, To: lock.StatePlanStale}, nil
	}}
	o.redis.AddHook(&redisFaults{after: func(cmd redis.Cmder) {
		for _, arg := range cmd.Args() {
			if arg == "stopped" {
				cancel()
			}
		}
	}})
	require.NoError(t, o.records.Create(context.Background(), startedSweepRecord("op-1")))

	r := finalizedResult(t, o, "op-1", func() { o.sweepOnce(ctx) })

	require.Error(t, ctx.Err(), "the stop landed inside the tick")
	assert.Equal(t, noJobDiagnostic, r.Output)
	require.NoError(t, lockCtxErr, "the Lock event is applied on a context the stop does not cancel")
	assert.Equal(t, 1, client.updateCheckRunCalled)
	rec, err := o.records.get(context.Background(), "op-1")
	require.NoError(t, err)
	assert.Nil(t, rec, "the record is deleted, not left finalized until it expires")
}

// A Redis error while a Project is completed in its trigger is tried
// again: the Operation is already claimed and its record deleted, so
// nothing else would ever complete the Project.
func TestCompleteProject_TriesAgainWhenRedisFailsTheCompletion(t *testing.T) {
	d := &dispatchRecorder{}
	o, client := carryOrchestrator(t, d)
	ctx := context.Background()
	meta := carryMeta([]Target{carryTarget("web", "diff")})
	require.NoError(t, o.triggers.Create(ctx, "t1", meta, nil))
	require.NoError(t, o.beginCommand(ctx, "t1", meta, 0))

	completions := 0
	o.redis.AddHook(&redisFaults{before: failFirst(2, &completions, isComplete)})

	require.NoError(t, o.completeProject(ctx, "t1", 0, "web", github.ProjectResult{ProjectName: "web", Tool: "helmfile", Operation: "diff", Success: true, Output: "planned-out"}))

	assert.Equal(t, 3, completions, "two failures, then the one that went through")
	require.Len(t, client.posted, 1, "the comment is posted once Redis answers")
	assert.Contains(t, client.posted[0], "planned-out")
	n, err := o.redis.Exists(ctx, triggerKey("t1")).Result()
	require.NoError(t, err)
	assert.Zero(t, n)
}

// The same for the step after a command's last completion: a Redis error
// beginning the next command, or reading the trigger back, does not strand
// the commands still to run.
func TestCompleteProject_TriesAgainWhenRedisFailsCarryingForward(t *testing.T) {
	for name, match := range map[string]func([]redis.Cmder) bool{
		"reading the trigger back":   func(cmds []redis.Cmder) bool { return cmdsMention(cmds, "hgetall", "trigger:") },
		"beginning the next command": func(cmds []redis.Cmder) bool { return cmdsMention(cmds, "hset", "pending:1:web") },
	} {
		t.Run(name, func(t *testing.T) {
			d := &dispatchRecorder{}
			o, client := carryOrchestrator(t, d)
			ctx := context.Background()
			meta := carryMeta([]Target{carryTarget("web", "diff")}, []Target{carryTarget("web", "apply")})
			require.NoError(t, o.triggers.Create(ctx, "t1", meta, nil))
			require.NoError(t, o.beginCommand(ctx, "t1", meta, 0))

			seen := 0
			o.redis.AddHook(&redisFaults{before: failFirst(1, &seen, match)})

			require.NoError(t, o.completeProject(ctx, "t1", 0, "web", testProjectResult("web")))

			assert.Equal(t, 2, seen)
			assert.Equal(t, []string{"diff:web", "apply:web"}, d.calls, "the next command is dispatched, once")
			assert.Empty(t, client.posted)
			st, err := o.triggers.Get(ctx, "t1")
			require.NoError(t, err)
			require.NotNil(t, st)
			assert.Equal(t, 1, st.Command)
		})
	}
}

// loseAnswer is a go-redis hook that lets the first command match accepts
// reach Redis and run, then drops its answer: the caller sees an error, as
// when a connection breaks after Redis has done the work.
type loseAnswer struct {
	mu    sync.Mutex
	match func(cmds []redis.Cmder) bool
	lost  bool
}

func (h *loseAnswer) DialHook(next redis.DialHook) redis.DialHook { return next }

func (h *loseAnswer) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		err := next(ctx, cmd)
		h.mu.Lock()
		defer h.mu.Unlock()
		if !h.lost && h.match([]redis.Cmder{cmd}) {
			h.lost = true
			cmd.SetErr(errRedisDown)
			return errRedisDown
		}
		return err
	}
}

func (h *loseAnswer) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}

// A completion Redis ran whose answer never arrived: the try after it
// carries the same token and is told it finished the command, so the
// trigger still moves on (the next command, or the comment), once.
func TestCompleteProject_ALostAnswerStillCarriesTheTriggerForward(t *testing.T) {
	t.Run("the next command", func(t *testing.T) {
		d := &dispatchRecorder{}
		o, client := carryOrchestrator(t, d)
		ctx := context.Background()
		meta := carryMeta([]Target{carryTarget("web", "diff")}, []Target{carryTarget("web", "apply")})
		require.NoError(t, o.triggers.Create(ctx, "t1", meta, nil))
		require.NoError(t, o.beginCommand(ctx, "t1", meta, 0))

		h := &loseAnswer{match: isComplete}
		o.redis.AddHook(h)

		require.NoError(t, o.completeProject(ctx, "t1", 0, "web", testProjectResult("web")))

		assert.True(t, h.lost, "the first completion's answer was dropped")
		assert.Equal(t, []string{"diff:web", "apply:web"}, d.calls, "the next command is dispatched, once")
		assert.Empty(t, client.posted)
	})

	t.Run("the comment", func(t *testing.T) {
		d := &dispatchRecorder{}
		o, client := carryOrchestrator(t, d)
		ctx := context.Background()
		meta := carryMeta([]Target{carryTarget("web", "diff")})
		require.NoError(t, o.triggers.Create(ctx, "t1", meta, nil))
		require.NoError(t, o.beginCommand(ctx, "t1", meta, 0))

		h := &loseAnswer{match: isComplete}
		o.redis.AddHook(h)

		require.NoError(t, o.completeProject(ctx, "t1", 0, "web", github.ProjectResult{ProjectName: "web", Tool: "helmfile", Operation: "diff", Success: true, Output: "planned-out"}))

		assert.True(t, h.lost)
		require.Len(t, client.posted, 1, "the comment is posted, once")
		assert.Contains(t, client.posted[0], "planned-out")
		assert.Equal(t, 1, strings.Count(client.posted[0], "planned-out"), "carrying the one result")
		n, err := o.redis.Exists(ctx, triggerKey("t1")).Result()
		require.NoError(t, err)
		assert.Zero(t, n)
	})
}

// Trying again is bounded: a Redis that stays down is reported to the
// caller after triggerAttempts tries.
func TestCompleteProject_GivesUpAfterTheLastAttempt(t *testing.T) {
	d := &dispatchRecorder{}
	o, client := carryOrchestrator(t, d)
	ctx := context.Background()
	meta := carryMeta([]Target{carryTarget("web", "diff")})
	require.NoError(t, o.triggers.Create(ctx, "t1", meta, nil))
	require.NoError(t, o.beginCommand(ctx, "t1", meta, 0))

	completions := 0
	o.redis.AddHook(&redisFaults{before: failFirst(1000, &completions, isComplete)})

	err := o.completeProject(ctx, "t1", 0, "web", testProjectResult("web"))

	require.ErrorIs(t, err, errRedisDown)
	assert.Equal(t, triggerAttempts, completions)
	assert.Empty(t, client.posted)
}

// A trigger that cannot be recorded dispatches nothing, and the pull
// request is told: the Projects refused during selection keep their
// reasons, every other Project says why it did not run, and the notices
// are not dropped.
func TestStartTrigger_RecordNotWrittenStillReportsEveryProject(t *testing.T) {
	d := &dispatchRecorder{}
	o, client := carryOrchestrator(t, d)
	meta := carryMeta([]Target{carryTarget("api", "diff")})
	meta.Notices = []string{"a notice"}
	refused := []github.ProjectResult{{ProjectName: "web", Tool: "helmfile", Operation: "apply", Output: "write permission is required"}}

	creations := 0
	o.redis.AddHook(&redisFaults{before: failFirst(1000, &creations, func(cmds []redis.Cmder) bool {
		return cmdsMention(cmds, "hset", "meta")
	})})

	err := o.startTrigger(context.Background(), client, meta, refused)

	require.ErrorIs(t, err, errRedisDown, "the webhook still learns the trigger was not recorded")
	assert.Equal(t, triggerAttempts, creations)
	assert.Empty(t, d.calls, "nothing is dispatched for a trigger with no record")
	require.Len(t, client.posted, 2, "one plan comment and one apply comment")
	assert.Contains(t, client.posted[0], "api")
	assert.Contains(t, client.posted[0], "recording the trigger")
	assert.Contains(t, client.posted[0], "comment again to retry")
	assert.Contains(t, client.posted[0], "a notice")
	assert.Contains(t, client.posted[1], "write permission is required")
}
