package plugin

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withStopGrace shortens the time a stopped tool has before it is killed,
// for one test.
func withStopGrace(t *testing.T, grace time.Duration) {
	t.Helper()
	old := stopGrace
	stopGrace = grace
	t.Cleanup(func() { stopGrace = old })
}

// runStoppable runs a shell script through execCommand with an idle
// timeout and returns how it ended and how long it took.
func runStoppable(t *testing.T, ctx context.Context, script string, idle time.Duration, rec *Record) (exitCode int, stopped StopReason, elapsed time.Duration) {
	t.Helper()
	start := time.Now()
	exitCode, stopped, err := execCommand(ctx, "", "sh", []string{"-c", script}, "", idle, rec)
	require.NoError(t, err)
	return exitCode, stopped, time.Since(start)
}

func lastLine(rec *Record) Line {
	lines := rec.snapshot()
	return lines[len(lines)-1]
}

func TestExecCommand_SilenceStopsTheTool(t *testing.T) {
	withStopGrace(t, 5*time.Second)
	rec := NewRecord()

	exitCode, stopped, elapsed := runStoppable(t, context.Background(), "echo one; sleep 30", 300*time.Millisecond, rec)

	assert.Equal(t, StopIdle, stopped)
	assert.Equal(t, -1, exitCode, "sh dies of the interrupt")
	assert.Less(t, elapsed, 5*time.Second, "stopped by the interrupt, not the kill")
	assert.Equal(t, []Line{stdoutLine("one")}, toolLines(rec))
	last := lastLine(rec)
	assert.Equal(t, Turnip, last.Stream)
	assert.True(t, strings.HasPrefix(last.Text, transcriptPrefix+"stopped: no output for 300ms, after "), last.Text)
}

func TestExecCommand_SteadyOutputIsNeverStopped(t *testing.T) {
	rec := NewRecord()

	// One second of output, a line every 50ms, against an idle timeout of
	// 300ms.
	exitCode, stopped, _ := runStoppable(t, context.Background(),
		"i=0; while [ $i -lt 20 ]; do echo $i; sleep 0.05; i=$((i+1)); done",
		300*time.Millisecond, rec)

	assert.Equal(t, StopNone, stopped)
	assert.Equal(t, 0, exitCode)
	assert.Len(t, toolLines(rec), 20)
	assert.True(t, strings.HasPrefix(lastLine(rec).Text, transcriptPrefix+"exit 0 in "))
}

func TestExecCommand_StandardErrorAlsoKeepsTheToolAlive(t *testing.T) {
	rec := NewRecord()

	exitCode, stopped, _ := runStoppable(t, context.Background(),
		"i=0; while [ $i -lt 20 ]; do echo $i >&2; sleep 0.05; i=$((i+1)); done",
		300*time.Millisecond, rec)

	assert.Equal(t, StopNone, stopped)
	assert.Equal(t, 0, exitCode)
}

func TestExecCommand_AnnotationLinesDoNotResetTheIdleTimer(t *testing.T) {
	withStopGrace(t, 5*time.Second)
	rec := NewRecord()

	// turnip's own lines keep arriving while the tool is silent.
	stopAnnotating := make(chan struct{})
	annotated := make(chan struct{})
	go func() {
		defer close(annotated)
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stopAnnotating:
				return
			case <-ticker.C:
				rec.Append(Turnip, annotation("still here"))
			}
		}
	}()

	_, stopped, elapsed := runStoppable(t, context.Background(), "sleep 30", 300*time.Millisecond, rec)
	close(stopAnnotating)
	<-annotated

	assert.Equal(t, StopIdle, stopped)
	assert.Less(t, elapsed, 5*time.Second)
}

func TestExecCommand_AToolExitingOnTheInterruptKeepsItsExitCode(t *testing.T) {
	withStopGrace(t, 10*time.Second)
	rec := NewRecord()

	exitCode, stopped, elapsed := runStoppable(t, context.Background(),
		`trap 'echo cleaning up; exit 130' INT; echo ready; while :; do sleep 0.05; done`,
		300*time.Millisecond, rec)

	assert.Equal(t, StopIdle, stopped)
	assert.Equal(t, 130, exitCode, "not killed: the tool's own exit")
	assert.Less(t, elapsed, 5*time.Second, "well within the grace")
	assert.Contains(t, rec.StreamText(Stdout), "cleaning up")
}

func TestExecCommand_AToolIgnoringTheInterruptIsKilledAfterTheGrace(t *testing.T) {
	const grace = 500 * time.Millisecond
	withStopGrace(t, grace)
	rec := NewRecord()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		// Once the tool has said it trapped the interrupt.
		for !strings.Contains(rec.StreamText(Stdout), "ready") {
			time.Sleep(10 * time.Millisecond)
		}
		cancel()
	}()

	exitCode, stopped, elapsed := runStoppable(t, ctx,
		`trap 'echo ignoring' INT; echo ready; while :; do sleep 0.05; done`,
		0, rec)

	assert.Equal(t, StopRequested, stopped)
	assert.Equal(t, -1, exitCode, "killed")
	assert.GreaterOrEqual(t, elapsed, grace, "only after the grace")
	assert.Contains(t, rec.StreamText(Stdout), "ignoring", "it was interrupted first")
	last := lastLine(rec)
	assert.True(t, strings.HasPrefix(last.Text, transcriptPrefix+"stopped: the Runner was told to stop, after "), last.Text)
}

// The interrupt goes to the tool's whole process group: a child process
// the tool started receives it too. The outer shell waits on the inner
// one, which only ends on its own interrupt, so a signal sent to the outer
// shell alone would leave the inner one running until the kill.
func TestExecCommand_AChildOfTheToolReceivesTheInterrupt(t *testing.T) {
	withStopGrace(t, 10*time.Second)
	rec := NewRecord()

	exitCode, stopped, elapsed := runStoppable(t, context.Background(),
		`trap 'echo outer interrupted' INT; `+
			`sh -c "trap 'echo child interrupted; exit 0' INT; echo ready; while :; do sleep 0.05; done"; `+
			`echo outer done`,
		300*time.Millisecond, rec)

	assert.Equal(t, StopIdle, stopped)
	assert.Less(t, elapsed, 5*time.Second, "not killed")
	assert.Equal(t, 0, exitCode)
	out := rec.StreamText(Stdout)
	assert.Contains(t, out, "child interrupted")
	assert.Contains(t, out, "outer done")
}

func TestExecCommand_ARequestedStopInterruptsTheTool(t *testing.T) {
	withStopGrace(t, 10*time.Second)
	rec := NewRecord()
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	go func() {
		for !strings.Contains(rec.StreamText(Stdout), "ready") {
			time.Sleep(10 * time.Millisecond)
		}
		cancel(assert.AnError)
	}()

	exitCode, stopped, elapsed := runStoppable(t, ctx,
		`trap 'exit 3' INT; echo ready; while :; do sleep 0.05; done`, time.Hour, rec)

	assert.Equal(t, StopRequested, stopped)
	assert.Equal(t, 3, exitCode)
	assert.Less(t, elapsed, 5*time.Second)
}

func TestExecCommand_AToolExitingZeroOnTheInterruptReportsZero(t *testing.T) {
	withStopGrace(t, 10*time.Second)
	rec := NewRecord()

	exitCode, stopped, _ := runStoppable(t, context.Background(),
		`trap 'exit 0' INT; echo ready; while :; do sleep 0.05; done`, 300*time.Millisecond, rec)

	assert.Equal(t, StopIdle, stopped)
	assert.Equal(t, 0, exitCode)
}

func TestTranscriptTrailer_WhenStopped(t *testing.T) {
	started := time.Now().Add(-41 * time.Minute)
	assert.Equal(t, "@@ turnip: stopped: no output for 30m0s, after 41m0s @@",
		transcriptTrailer(started, nil, StopIdle, 30*time.Minute))
	assert.Equal(t, "@@ turnip: stopped: the Runner was told to stop, after 41m0s @@",
		transcriptTrailer(started, nil, StopRequested, 30*time.Minute))
}

// os/exec can call Cancel after the tool has exited on its own and been
// reaped, when its context ends in the gap before Wait collects the
// cancellation. The interrupt then reaches no one, and the run must not
// be taken for a stopped one.
func TestInterruptGroup_AToolThatAlreadyExitedIsNotRecordedAsStopped(t *testing.T) {
	cmd := exec.Command("sh", "-c", "exit 0")
	startProcessGroup(cmd)
	require.NoError(t, cmd.Start())
	require.NoError(t, cmd.Wait())

	var interrupted atomic.Bool
	err := interruptGroup(cmd.Process, &interrupted)
	require.ErrorIs(t, err, os.ErrProcessDone)
	assert.False(t, interrupted.Load())
}

func TestInterruptGroup_ARunningToolIsRecordedAsStopped(t *testing.T) {
	cmd := exec.Command("sh", "-c", "sleep 5")
	startProcessGroup(cmd)
	require.NoError(t, cmd.Start())

	var interrupted atomic.Bool
	require.NoError(t, interruptGroup(cmd.Process, &interrupted))
	assert.True(t, interrupted.Load())
	_ = cmd.Wait()
}
