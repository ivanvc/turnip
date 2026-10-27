package runner

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivanvc/turnip/internal/plugin"
	"github.com/ivanvc/turnip/internal/rpc"
)

// stoppablePlugin stands in for a tool the Runner stops: its Execute
// blocks until the tool's context ends, notes why, and returns what
// execCommand returns for a stopped tool.
type stoppablePlugin struct {
	fakePlugin
	exitCode int
	// started is closed once Execute is running.
	started chan struct{}

	mu        sync.Mutex
	cause     error
	opts      plugin.ExecuteOptions
	cancelled bool
}

func newStoppablePlugin(exitCode int) *stoppablePlugin {
	return &stoppablePlugin{
		fakePlugin: fakePlugin{operations: []string{"diff"}},
		exitCode:   exitCode,
		started:    make(chan struct{}),
	}
}

func (p *stoppablePlugin) Execute(ctx context.Context, _ string, opts plugin.ExecuteOptions) (*plugin.ExecuteResult, error) {
	opts.Record.Append(plugin.Stdout, "working")
	close(p.started)
	<-ctx.Done()

	p.mu.Lock()
	p.cause = context.Cause(ctx)
	p.opts = opts
	p.cancelled = true
	p.mu.Unlock()

	opts.Record.Close()
	return &plugin.ExecuteResult{Output: "working\n", ExitCode: p.exitCode, Stopped: plugin.StopRequested}, nil
}

func (p *stoppablePlugin) toolCancelled() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.cancelled
}

// ctxReporter is a fakeReporter whose Report runs a function of the test's,
// which sees the context the result is reported on.
type ctxReporter struct {
	fakeReporter
	report func(ctx context.Context, result OperationResult) error
}

func (r *ctxReporter) Report(ctx context.Context, result OperationResult) error {
	if r.followed != nil {
		<-r.followed
	}
	r.mu.Lock()
	r.lastResult = result
	r.mu.Unlock()
	return r.report(ctx, result)
}

func (r *ctxReporter) result() OperationResult {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastResult
}

func shortenReportBound(t *testing.T, d time.Duration) {
	t.Helper()
	old := reportBoundAfterStop
	reportBoundAfterStop = d
	t.Cleanup(func() { reportBoundAfterStop = old })
}

// The stop is injected: no signal reaches the test process.
func TestRunWithStop_StopInterruptsTheToolAndReportsStopped(t *testing.T) {
	p := newStoppablePlugin(130)

	var toolCancelledAtReport bool
	var reportCtxErr error
	rep := &ctxReporter{report: func(ctx context.Context, _ OperationResult) error {
		toolCancelledAtReport = p.toolCancelled()
		reportCtxErr = ctx.Err()
		return nil
	}}

	stop := make(chan struct{})
	var stdout, stderr safeBuffer
	done := make(chan int, 1)
	go func() {
		done <- runWithStop(context.Background(), testRunConfig(), rep, fakeSelector(p), stop, &stdout, &stderr)
	}()

	<-p.started
	close(stop)

	select {
	case code := <-done:
		assert.Equal(t, 0, code, "the stopped result was delivered")
	case <-time.After(5 * time.Second):
		require.FailNow(t, "the Runner did not finish after being told to stop")
	}

	require.ErrorIs(t, p.cause, errStopRequested, "the tool's context was cancelled with the cause stop requested")
	assert.True(t, toolCancelledAtReport, "the result was reported after the tool's context was cancelled")
	require.NoError(t, reportCtxErr, "the report's context is not the tool's: the stop did not cancel it")

	got := rep.result()
	assert.False(t, got.Success)
	assert.Equal(t, 130, got.ExitCode, "the tool's own exit code is kept")
	assert.Equal(t, rpc.FailureStopped, got.FailureCategory)
	assert.Equal(t, "The Runner was told to stop (its deadline, an eviction or a deletion), so turnip stopped the tool.", got.ErrorMessage)
	assert.Contains(t, stderr.String(), "runner: told to stop; interrupting the tool")
	assert.Contains(t, stderr.String(), "category=stopped")
}

// A tool that exits 0 on its interrupt still did not finish its operation.
func TestRunWithStop_ToolExitingZeroOnStopStillFails(t *testing.T) {
	p := newStoppablePlugin(0)
	rep := &ctxReporter{report: func(context.Context, OperationResult) error { return nil }}

	stop := make(chan struct{})
	var stdout, stderr safeBuffer
	done := make(chan int, 1)
	go func() {
		done <- runWithStop(context.Background(), testRunConfig(), rep, fakeSelector(p), stop, &stdout, &stderr)
	}()
	<-p.started
	close(stop)
	require.Equal(t, 0, <-done)

	got := rep.result()
	assert.False(t, got.Success)
	assert.Equal(t, 0, got.ExitCode)
	assert.Equal(t, rpc.FailureStopped, got.FailureCategory)
}

func TestRunWith_SilentToolReportsTimedOut(t *testing.T) {
	cfg := testRunConfig()
	cfg.IdleTimeout = 30 * time.Minute

	var passed time.Duration
	p := &optsPlugin{fakePlugin: fakePlugin{operations: []string{"diff"}}, execute: func(opts plugin.ExecuteOptions) *plugin.ExecuteResult {
		passed = opts.IdleTimeout
		return &plugin.ExecuteResult{Output: "started\n", ExitCode: 130, Stopped: plugin.StopIdle}
	}}
	rep := &fakeReporter{}
	var stdout, stderr safeBuffer

	require.Equal(t, 0, runWith(context.Background(), cfg, rep, fakeSelector(p), &stdout, &stderr))

	assert.Equal(t, 30*time.Minute, passed, "the idle timeout reaches the Plugin")
	assert.False(t, rep.lastResult.Success)
	assert.Equal(t, 130, rep.lastResult.ExitCode)
	assert.Equal(t, rpc.FailureTimedOut, rep.lastResult.FailureCategory)
	assert.Equal(t, "The tool wrote nothing for 30m0s, so turnip stopped it.", rep.lastResult.ErrorMessage)
	assert.Contains(t, stderr.String(), "category=timed_out")
}

// optsPlugin returns what its function makes of the options it was given.
type optsPlugin struct {
	fakePlugin
	execute func(plugin.ExecuteOptions) *plugin.ExecuteResult
}

func (p *optsPlugin) Execute(_ context.Context, _ string, opts plugin.ExecuteOptions) (*plugin.ExecuteResult, error) {
	return p.execute(opts), nil
}

// Told to stop before the tool could start: the Plugin fails to start it on
// a cancelled context, and the result says the Runner was stopped rather
// than that the tool could not be run.
func TestRunWithStop_StopBeforeTheToolStartsReportsStopped(t *testing.T) {
	stop := make(chan struct{})
	close(stop)

	p := &ctxErrPlugin{fakePlugin: fakePlugin{operations: []string{"diff"}}}
	rep := &fakeReporter{}
	var stdout, stderr safeBuffer

	require.Equal(t, 0, runWithStop(context.Background(), testRunConfig(), rep, fakeSelector(p), stop, &stdout, &stderr))

	assert.Equal(t, -1, rep.lastResult.ExitCode)
	assert.Equal(t, rpc.FailureStopped, rep.lastResult.FailureCategory)
	assert.Equal(t, requestedStopMessage, rep.lastResult.ErrorMessage)
}

// ctxErrPlugin waits for its context to end and fails as exec does when
// asked to start a command on a cancelled context.
type ctxErrPlugin struct{ fakePlugin }

func (p *ctxErrPlugin) Execute(ctx context.Context, _ string, _ plugin.ExecuteOptions) (*plugin.ExecuteResult, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

// A Server that never acknowledges cannot hold a stopped Runner past the
// bound: the Pod is about to be killed, and the sweep finalizes the
// Operation instead.
func TestRunWithStop_ReportIsBoundedOnceStopped(t *testing.T) {
	shortenReportBound(t, 50*time.Millisecond)

	p := newStoppablePlugin(130)
	rep := &ctxReporter{report: func(context.Context, OperationResult) error {
		select {} // never acknowledged, and deaf to its context
	}}

	stop := make(chan struct{})
	var stdout, stderr safeBuffer
	done := make(chan int, 1)
	go func() {
		done <- runWithStop(context.Background(), testRunConfig(), rep, fakeSelector(p), stop, &stdout, &stderr)
	}()
	<-p.started
	close(stop)

	select {
	case code := <-done:
		assert.Equal(t, 1, code)
	case <-time.After(5 * time.Second):
		require.FailNow(t, "the report was not bounded after the stop")
	}
	assert.Contains(t, stderr.String(), "runner: report result: no acknowledgment within 50ms of being told to stop")
}

// A stop that arrives while the result is being reported bounds the report
// from then on, and does not touch the result already being sent.
func TestReportBounded_StopDuringReportBoundsIt(t *testing.T) {
	shortenReportBound(t, 50*time.Millisecond)

	entered := make(chan struct{})
	rep := &ctxReporter{report: func(ctx context.Context, _ OperationResult) error {
		close(entered)
		<-ctx.Done()
		return context.Cause(ctx)
	}}

	stop := make(chan struct{})
	done := make(chan error, 1)
	go func() { done <- reportBounded(context.Background(), rep, OperationResult{Success: true}, stop) }()

	<-entered
	close(stop)

	select {
	case err := <-done:
		require.Error(t, err)
		assert.Contains(t, err.Error(), "of being told to stop")
	case <-time.After(5 * time.Second):
		require.FailNow(t, "the report was not bounded after the stop")
	}
	assert.True(t, rep.result().Success)
}

// Without a stop, reporting keeps its own retry budget: no bound applies.
func TestReportBounded_NoStopLeavesTheReportUnbounded(t *testing.T) {
	shortenReportBound(t, time.Millisecond)

	rep := &ctxReporter{report: func(ctx context.Context, _ OperationResult) error {
		time.Sleep(20 * time.Millisecond)
		return ctx.Err()
	}}

	require.NoError(t, reportBounded(context.Background(), rep, OperationResult{}, nil))
}

func TestReportBounded_ReturnsTheReportersError(t *testing.T) {
	want := errors.New("refused")
	rep := &ctxReporter{report: func(context.Context, OperationResult) error { return want }}

	require.ErrorIs(t, reportBounded(context.Background(), rep, OperationResult{}, make(chan struct{})), want)
}
