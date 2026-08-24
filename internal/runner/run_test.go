package runner

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivanvc/turnip/internal/plugin"
)

// safeBuffer is a mutex-guarded io.Writer/fmt.Stringer, since the test
// goroutine reads stdout/stderr concurrently with runWith's goroutine
// writing to them.
type safeBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *safeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *safeBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

type outputLine struct{ stream, line string }

// fakePlugin produces a scripted sequence of OnOutput calls before
// returning a fixed result.
type fakePlugin struct {
	operations []string
	script     []outputLine
	result     *plugin.ExecuteResult
	resultErr  error
}

func (p *fakePlugin) Name() string              { return "fake" }
func (p *fakePlugin) GetOperations() []string   { return p.operations }
func (p *fakePlugin) GetPlanOperation() string  { return p.operations[0] }
func (p *fakePlugin) GetApplyOperation() string { return p.operations[0] }
func (p *fakePlugin) Execute(_ context.Context, _ string, opts plugin.ExecuteOptions) (*plugin.ExecuteResult, error) {
	for _, l := range p.script {
		if opts.OnOutput != nil {
			opts.OnOutput(l.stream, l.line)
		}
	}
	return p.result, p.resultErr
}

func fakeSelector(p plugin.Plugin) pluginSelector {
	return func(tool string) (plugin.Plugin, error) { return p, nil }
}

func noopClone(context.Context, string, string, string, string) error { return nil }

// fakeReporter records every call run.go makes, and can be told to block
// LogLine for one specific line, signaling entry via entered so a test can
// synchronize deterministically without polling.
type fakeReporter struct {
	mu           sync.Mutex
	connectCalls int
	connectErr   error
	logLines     []outputLine
	reportErr    error
	lastResult   OperationResult

	blockOn string
	entered chan struct{}
	release chan struct{}
}

func (r *fakeReporter) Connect(context.Context) error {
	r.mu.Lock()
	r.connectCalls++
	r.mu.Unlock()
	return r.connectErr
}

func (r *fakeReporter) LogLine(stream, line string) {
	r.mu.Lock()
	r.logLines = append(r.logLines, outputLine{stream, line})
	r.mu.Unlock()

	if r.blockOn != "" && line == r.blockOn {
		r.entered <- struct{}{}
		<-r.release
	}
}

func (r *fakeReporter) Report(_ context.Context, result OperationResult) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lastResult = result
	return r.reportErr
}

func testRunConfig() Config {
	return Config{
		Tool:      "fake",
		Operation: "diff",
		RepoURL:   "https://github.com/acme/repo.git",
		CommitSHA: "abc123",
	}
}

func TestPathWithToolsDir_PrependsToolsDir(t *testing.T) {
	assert.Equal(t, "/tools:/usr/bin:/bin", pathWithToolsDir("/tools", "/usr/bin:/bin"))
}

func TestPathWithToolsDir_EmptyToolsDirLeavesPathUntouched(t *testing.T) {
	assert.Equal(t, "/usr/bin:/bin", pathWithToolsDir("", "/usr/bin:/bin"))
}

func TestRunWith_OnOutputWritesToMatchingLocalStreamInOrder(t *testing.T) {
	p := &fakePlugin{
		operations: []string{"diff"},
		script: []outputLine{
			{"stdout", "out1"},
			{"stderr", "err1"},
			{"stdout", "out2"},
		},
		result: &plugin.ExecuteResult{ExitCode: 0, Output: "ok"},
	}
	rep := &fakeReporter{}
	var stdout, stderr safeBuffer

	exitCode := runWith(context.Background(), testRunConfig(), rep, fakeSelector(p), noopClone, &stdout, &stderr)

	assert.Equal(t, 0, exitCode)
	assert.Equal(t, "out1\nout2\n", stdout.String())
	assert.Contains(t, stderr.String(), "err1\n")
	assert.Equal(t, []outputLine{{"stdout", "out1"}, {"stderr", "err1"}, {"stdout", "out2"}}, rep.logLines)
	assert.True(t, rep.lastResult.Success)
}

// TestRunWith_LocalWriteNeverWaitsOnReporter proves Requirement 4.7's
// ordering guarantee deterministically: the local stdout/stderr write for
// a line happens even while the reporter's LogLine call for that same
// line is still blocked, never having returned. If run.go's OnOutput
// wiring ever called LogLine before writing locally (or made the write
// depend on LogLine returning), this test would hang forever instead of
// observing the write.
func TestRunWith_LocalWriteNeverWaitsOnReporter(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	rep := &fakeReporter{blockOn: "blocking-line", entered: entered, release: release}

	p := &fakePlugin{
		operations: []string{"diff"},
		script: []outputLine{
			{"stdout", "before"},
			{"stdout", "blocking-line"},
			{"stdout", "after"},
		},
		result: &plugin.ExecuteResult{ExitCode: 0},
	}
	var stdout, stderr safeBuffer

	done := make(chan int, 1)
	go func() {
		done <- runWith(context.Background(), testRunConfig(), rep, fakeSelector(p), noopClone, &stdout, &stderr)
	}()

	<-entered // LogLine("stdout", "blocking-line") has been entered and is now blocked
	assert.Contains(t, stdout.String(), "blocking-line", "the local write must complete before LogLine, not after it returns")
	close(release)

	exitCode := <-done
	assert.Equal(t, 0, exitCode)
	assert.Equal(t, "before\nblocking-line\nafter\n", stdout.String())
}

func TestRunWith_UnrecognizedToolFailsFastBeforeConnectOrClone(t *testing.T) {
	rep := &fakeReporter{}
	cloneCalled := false
	clone := func(context.Context, string, string, string, string) error {
		cloneCalled = true
		return nil
	}
	selectPlugin := func(tool string) (plugin.Plugin, error) {
		return nil, errors.New("unrecognized tool")
	}
	var stdout, stderr safeBuffer

	exitCode := runWith(context.Background(), testRunConfig(), rep, selectPlugin, clone, &stdout, &stderr)

	assert.Equal(t, 1, exitCode)
	assert.Equal(t, 0, rep.connectCalls, "Connect must not be attempted for an unrecognized tool")
	assert.False(t, cloneCalled, "clone must not be attempted for an unrecognized tool")
}

func TestRunWith_ConnectFailureFailsFastBeforeClone(t *testing.T) {
	rep := &fakeReporter{connectErr: errors.New("no route to server")}
	cloneCalled := false
	clone := func(context.Context, string, string, string, string) error {
		cloneCalled = true
		return nil
	}
	p := &fakePlugin{operations: []string{"diff"}}
	var stdout, stderr safeBuffer

	exitCode := runWith(context.Background(), testRunConfig(), rep, fakeSelector(p), clone, &stdout, &stderr)

	assert.Equal(t, 1, exitCode)
	assert.False(t, cloneCalled)
}

func TestRunWith_CloneFailureReportsFailureResult(t *testing.T) {
	rep := &fakeReporter{}
	clone := func(context.Context, string, string, string, string) error {
		return errors.New("commit not found")
	}
	p := &fakePlugin{operations: []string{"diff"}}
	var stdout, stderr safeBuffer

	exitCode := runWith(context.Background(), testRunConfig(), rep, fakeSelector(p), clone, &stdout, &stderr)

	// A clone failure is reported as the Operation's own result (a
	// successful report of a failure), not a Runner crash — Requirement
	// 5.3 — so the exit code reflects whether *reporting* succeeded, not
	// whether the Operation itself did.
	require.Equal(t, 0, exitCode)
	assert.False(t, rep.lastResult.Success)
	assert.Contains(t, rep.lastResult.ErrorMessage, "commit not found")
}

func TestRunWith_ReportFailureReturnsNonZero(t *testing.T) {
	rep := &fakeReporter{reportErr: errors.New("server unreachable")}
	p := &fakePlugin{operations: []string{"diff"}, result: &plugin.ExecuteResult{ExitCode: 0}}
	var stdout, stderr safeBuffer

	exitCode := runWith(context.Background(), testRunConfig(), rep, fakeSelector(p), noopClone, &stdout, &stderr)

	assert.Equal(t, 1, exitCode)
}
