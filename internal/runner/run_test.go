package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivanvc/turnip/internal/plugin"
	"github.com/ivanvc/turnip/internal/provisioning"
	"github.com/ivanvc/turnip/internal/rpc"
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

// streamOf maps a script's stream name to the Record's.
func streamOf(name string) plugin.Stream {
	if name == "stderr" {
		return plugin.Stderr
	}
	return plugin.Stdout
}

// fakePlugin records a scripted sequence of lines before returning a fixed
// result. It leaves the Record open, as a Plugin that ran no command does,
// so these tests also cover run.go closing it.
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
func (p *fakePlugin) ActsWithoutChanges() bool  { return false }
func (p *fakePlugin) Provisioning() provisioning.Spec {
	return provisioning.Spec{Strategy: provisioning.RunInImage, Image: "ghcr.io/helmfile/helmfile"}
}
func (p *fakePlugin) ImageTags() []string                 { return []string{"v*.*.*", "sha256:*"} }
func (p *fakePlugin) MutationChangesReported(string) bool { return true }
func (p *fakePlugin) Execute(_ context.Context, _ string, opts plugin.ExecuteOptions) (*plugin.ExecuteResult, error) {
	for _, l := range p.script {
		if opts.Record != nil {
			opts.Record.Append(streamOf(l.stream), l.line)
		}
	}
	return p.result, p.resultErr
}

func fakeSelector(p plugin.Plugin) pluginSelector {
	return func(tool string) (plugin.Plugin, error) { return p, nil }
}

// fakeReporter records every call run.go makes. Its Follow reads the
// Record as the real sender does, and can be told to block on one specific
// line, signaling entry via entered so a test can synchronize
// deterministically without polling.
type fakeReporter struct {
	mu           sync.Mutex
	connectCalls int
	connectErr   error
	logLines     []outputLine
	reportErr    error
	lastResult   OperationResult
	// followed is closed once the Record Follow was given has been read
	// to its end; nil when Follow was never called.
	followed chan struct{}

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

func (r *fakeReporter) Follow(rec *plugin.Record, clean func(string) string) {
	r.followed = make(chan struct{})
	go func() {
		defer close(r.followed)
		from := 0
		for {
			lines, done := rec.Next(context.Background(), from)
			for _, l := range lines {
				stream := "stdout"
				if l.Stream == plugin.Stderr {
					stream = "stderr"
				}
				r.mu.Lock()
				r.logLines = append(r.logLines, outputLine{stream, clean(l.Text)})
				r.mu.Unlock()

				if r.blockOn != "" && l.Text == r.blockOn {
					r.entered <- struct{}{}
					<-r.release
				}
			}
			from += len(lines)
			if done {
				return
			}
		}
	}()
}

func (r *fakeReporter) Report(_ context.Context, result OperationResult) error {
	// As the real reporter does: the result follows every log line.
	if r.followed != nil {
		<-r.followed
	}
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

// A configured workspace is a volume mount point the Pod owns. Using it
// as-is is the point; removing it would empty a mount whose lifecycle
// belongs to Kubernetes, not to turnip.
func TestResolveWorkspace_GivenDirectoryIsUsedAndLeftInPlace(t *testing.T) {
	dir := t.TempDir()

	got, cleanup, err := resolveWorkspace(dir)
	require.NoError(t, err)
	assert.Equal(t, dir, got)

	cleanup()
	assert.DirExists(t, dir, "a mounted workspace is not turnip's to delete")
}

// The fallback keeps a Runner working outside a turnip-built Job — a
// test, or a hand-run binary — exactly as it behaved before the
// workspace volume existed.
func TestResolveWorkspace_NoDirectoryCreatesATemporaryOneAndRemovesIt(t *testing.T) {
	got, cleanup, err := resolveWorkspace("")
	require.NoError(t, err)
	require.NotEmpty(t, got)
	assert.DirExists(t, got)

	cleanup()
	assert.NoDirExists(t, got, "a directory turnip created is turnip's to clean up")
}

func TestRunWith_OutputReachesTheMatchingLocalStreamAndTheReporterInOrder(t *testing.T) {
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

	exitCode := runWith(context.Background(), testRunConfig(), rep, fakeSelector(p), &stdout, &stderr)

	assert.Equal(t, 0, exitCode)
	assert.Equal(t, "out1\nout2\n", stdout.String())
	assert.Contains(t, stderr.String(), "err1\n")
	assert.Equal(t, []outputLine{{"stdout", "out1"}, {"stderr", "err1"}, {"stdout", "out2"}}, rep.logLines)
	assert.True(t, rep.lastResult.Success)
}

// TestRunWith_LocalWriteNeverWaitsOnReporter proves Requirement 4.7's
// guarantee deterministically: the Runner's own log has every line the
// tool wrote, the ones after it included, while the reporter is still
// blocked sending one of them to the Server. If the mirror ever waited on
// the reporter (as it did when both were fed by one callback), this test
// would hang forever instead of observing the writes.
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
		done <- runWith(context.Background(), testRunConfig(), rep, fakeSelector(p), &stdout, &stderr)
	}()

	<-entered // the reporter is now blocked sending "blocking-line"
	require.Eventually(t, func() bool { return strings.Contains(stderr.String(), "runner: operation result:") },
		5*time.Second, time.Millisecond,
		"the tool ran to its end and the Runner logged its result while the reporter was still blocked")
	assert.Equal(t, "before\nblocking-line\nafter\n", stdout.String(),
		"every line reached the Runner's own log without waiting on the reporter")
	close(release)

	exitCode := <-done
	assert.Equal(t, 0, exitCode)
	assert.Equal(t, "before\nblocking-line\nafter\n", stdout.String())
}

func TestRunWith_UnrecognizedToolFailsFastBeforeConnect(t *testing.T) {
	rep := &fakeReporter{}
	selectPlugin := func(tool string) (plugin.Plugin, error) {
		return nil, errors.New("unrecognized tool")
	}
	var stdout, stderr safeBuffer

	exitCode := runWith(context.Background(), testRunConfig(), rep, selectPlugin, &stdout, &stderr)

	assert.Equal(t, 1, exitCode)
	assert.Equal(t, 0, rep.connectCalls, "Connect must not be attempted for an unrecognized tool")
}

// TestSelectPlugin_UsesRegistry pins the real selector to plugin.Registry():
// every registered tool resolves to its own Plugin, and anything else fails
// with the error runWith reports before connecting.
func TestSelectPlugin_UsesRegistry(t *testing.T) {
	for name := range plugin.Registry() {
		p, err := selectPlugin(name)
		require.NoError(t, err, name)
		assert.Equal(t, name, p.Name())
	}

	_, err := selectPlugin("not-a-tool")
	require.EqualError(t, err, `runner: unrecognized tool "not-a-tool"`)
}

func TestRunWith_ConnectFailureFailsFast(t *testing.T) {
	rep := &fakeReporter{connectErr: errors.New("no route to server")}
	p := &fakePlugin{operations: []string{"diff"}}
	var stdout, stderr safeBuffer

	exitCode := runWith(context.Background(), testRunConfig(), rep, fakeSelector(p), &stdout, &stderr)

	assert.Equal(t, 1, exitCode)
}

// The clone runs in an initContainer now, so its tests exercise
// runCloneWith rather than runWith. The exit-code expectation inverts in
// the move: the Runner returned 0 on a clone failure because *reporting*
// had succeeded, but an initContainer must exit non-zero so the Job fails
// and the container that runs the tool never starts.
func testCloneConfig() Config {
	cfg := testRunConfig()
	cfg.WorkspaceDir = "/turnip/src"
	return cfg
}

func TestRunCloneWith_SuccessExitsZeroWithoutContactingTheServer(t *testing.T) {
	rep := &fakeReporter{}
	var stderr safeBuffer

	clone := func(_ context.Context, dir, _, _, _, _ string) error {
		assert.Equal(t, "/turnip/src", dir, "the clone lands in the mounted workspace, not a temporary directory")
		return nil
	}

	exitCode := runCloneWith(context.Background(), testCloneConfig(), rep, clone, &stderr)

	assert.Equal(t, 0, exitCode)
	assert.Equal(t, 0, rep.connectCalls, "a successful clone does no gRPC work at all")
}

func TestRunCloneWith_FailureReportsBeforeExitingNonZero(t *testing.T) {
	rep := &fakeReporter{}
	var stderr safeBuffer

	clone := func(context.Context, string, string, string, string, string) error {
		return errors.New("commit not found")
	}

	exitCode := runCloneWith(context.Background(), testCloneConfig(), rep, clone, &stderr)

	assert.Equal(t, 1, exitCode, "the Job must fail so the tool container never starts")
	assert.Equal(t, 1, rep.connectCalls)
	assert.False(t, rep.lastResult.Success)
	assert.Contains(t, rep.lastResult.ErrorMessage, "commit not found",
		"git's own message reaches the pull request, not a later generic timeout")
	assert.Equal(t, rpc.FailureCloneFailed, rep.lastResult.FailureCategory)
}

// A clone failure is a finished Operation too, so its log carries the
// same error line and summary line as any other.
func TestRunCloneWith_FailureLogsTheErrorAndTheSummary(t *testing.T) {
	rep := &fakeReporter{}
	var stderr safeBuffer

	clone := func(context.Context, string, string, string, string, string) error {
		return errors.New("commit not found")
	}

	runCloneWith(context.Background(), testCloneConfig(), rep, clone, &stderr)

	log := stderr.String()
	require.Contains(t, log, "runner: operation error: ")
	assert.Contains(t, log, "commit not found")
	assert.Contains(t, log, "runner: operation result: success=false exit_code=-1 category=clone_failed ")
	assert.Less(t, strings.Index(log, "runner: operation error: "), strings.Index(log, "runner: operation result: "),
		"the error comes just before the summary")
}

func TestRunCloneWith_MergeConflictStaysDistinguishable(t *testing.T) {
	rep := &fakeReporter{}
	var stderr safeBuffer

	clone := func(context.Context, string, string, string, string, string) error {
		return &MergeConflictError{Output: "CONFLICT (content): Merge conflict in main.tf"}
	}

	exitCode := runCloneWith(context.Background(), testCloneConfig(), rep, clone, &stderr)

	assert.Equal(t, 1, exitCode)
	assert.Contains(t, rep.lastResult.ErrorMessage, "merge conflict")
	assert.Contains(t, rep.lastResult.ErrorMessage, "main.tf")
}

// A temporary directory would be removed when this process exits, leaving
// the container that runs the tool with an empty checkout — so an unset
// workspace is a configuration error here rather than the fallback it is
// for the Runner.
func TestRunCloneWith_MissingWorkspaceDirIsAConfigurationError(t *testing.T) {
	rep := &fakeReporter{}
	var stderr safeBuffer

	cloneCalled := false
	clone := func(context.Context, string, string, string, string, string) error {
		cloneCalled = true
		return nil
	}

	exitCode := runCloneWith(context.Background(), testRunConfig(), rep, clone, &stderr)

	assert.Equal(t, 1, exitCode)
	assert.False(t, cloneCalled, "nothing is cloned when there is nowhere durable to clone into")
	assert.Contains(t, rep.lastResult.ErrorMessage, "TURNIP_WORKSPACE_DIR")
	assert.Equal(t, rpc.FailureCloneFailed, rep.lastResult.FailureCategory)
}

// Each failure point reports which step failed, so the Server can title
// the check run without reading the message (check-run-titles
// Requirement 5.2).
func TestRunWith_ReportsTheFailureCategory(t *testing.T) {
	cases := map[string]struct {
		plugin *fakePlugin
		want   rpc.FailureCategory
	}{
		"success is unspecified": {
			plugin: &fakePlugin{operations: []string{"diff"}, result: &plugin.ExecuteResult{ExitCode: 0}},
			want:   rpc.FailureUnspecified,
		},
		"the tool exited non-zero": {
			plugin: &fakePlugin{operations: []string{"diff"}, result: &plugin.ExecuteResult{ExitCode: 2}},
			want:   rpc.FailureToolExited,
		},
		"the tool could not be started": {
			plugin: &fakePlugin{operations: []string{"diff"}, resultErr: errors.New(`exec: "helmfile": not found`)},
			want:   rpc.FailureToolNotStarted,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			rep := &fakeReporter{}
			var stdout, stderr safeBuffer

			runWith(context.Background(), testRunConfig(), rep, fakeSelector(tc.plugin), &stdout, &stderr)

			assert.Equal(t, tc.want, rep.lastResult.FailureCategory)
		})
	}
}

func TestRunWith_WorkspaceFailureIsCategorized(t *testing.T) {
	// No configured workspace falls back to a temporary directory, which
	// cannot be created under a TMPDIR that does not exist.
	t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "missing"))
	rep := &fakeReporter{}
	var stdout, stderr safeBuffer

	runWith(context.Background(), testRunConfig(), rep, fakeSelector(&fakePlugin{operations: []string{"diff"}, result: &plugin.ExecuteResult{}}), &stdout, &stderr)

	assert.False(t, rep.lastResult.Success)
	assert.Equal(t, rpc.FailureWorkspaceFailed, rep.lastResult.FailureCategory)
}

// Reporting is best-effort: if the Server cannot be reached, the clone
// failure must still fail the Job rather than being masked.
func TestRunCloneWith_ReportFailureStillExitsNonZero(t *testing.T) {
	rep := &fakeReporter{reportErr: errors.New("server unreachable")}
	var stderr safeBuffer

	clone := func(context.Context, string, string, string, string, string) error {
		return errors.New("commit not found")
	}

	exitCode := runCloneWith(context.Background(), testCloneConfig(), rep, clone, &stderr)

	assert.Equal(t, 1, exitCode)
}

func TestRunWith_ReportFailureReturnsNonZero(t *testing.T) {
	rep := &fakeReporter{reportErr: errors.New("server unreachable")}
	p := &fakePlugin{operations: []string{"diff"}, result: &plugin.ExecuteResult{ExitCode: 0}}
	var stdout, stderr safeBuffer

	exitCode := runWith(context.Background(), testRunConfig(), rep, fakeSelector(p), &stdout, &stderr)

	assert.Equal(t, 1, exitCode)
}

// echoWorkingDirPlugin reports its own WorkingDir through both a streamed
// log line and the final Output. That is the only way a test can observe
// the workspace at all: testRunConfig sets no WorkspaceDir, so execute()
// takes the temporary-directory fallback and its name is random and
// unknowable from outside.
type echoWorkingDirPlugin struct{ operations []string }

func (p *echoWorkingDirPlugin) Name() string              { return "fake" }
func (p *echoWorkingDirPlugin) GetOperations() []string   { return p.operations }
func (p *echoWorkingDirPlugin) GetPlanOperation() string  { return p.operations[0] }
func (p *echoWorkingDirPlugin) GetApplyOperation() string { return p.operations[0] }
func (p *echoWorkingDirPlugin) ActsWithoutChanges() bool  { return false }
func (p *echoWorkingDirPlugin) Provisioning() provisioning.Spec {
	return provisioning.Spec{Strategy: provisioning.RunInImage, Image: "ghcr.io/helmfile/helmfile"}
}
func (p *echoWorkingDirPlugin) ImageTags() []string                 { return []string{"v*.*.*", "sha256:*"} }
func (p *echoWorkingDirPlugin) MutationChangesReported(string) bool { return true }
func (p *echoWorkingDirPlugin) Execute(_ context.Context, _ string, opts plugin.ExecuteOptions) (*plugin.ExecuteResult, error) {
	if opts.Record != nil {
		opts.Record.Append(plugin.Stdout, "reading "+opts.WorkingDir+"/values.yaml")
	}
	return &plugin.ExecuteResult{
		Output:   `failed to read "` + opts.WorkingDir + `/values.yaml"`,
		ExitCode: 1,
	}, nil
}

func TestRunWith_WorkspacePathStrippedFromWhatTheServerSees(t *testing.T) {
	rep := &fakeReporter{}
	cfg := testRunConfig()
	cfg.ProjectDir = "environments/project"
	var stdout, stderr safeBuffer

	exitCode := runWith(context.Background(), cfg, rep, fakeSelector(&echoWorkingDirPlugin{operations: []string{"diff"}}), &stdout, &stderr)
	require.Equal(t, 0, exitCode)

	assert.NotContains(t, rep.lastResult.Output, "/tmp/turnip-runner-",
		"the workspace path must never reach the Server, and from there the PR comment")
	assert.Contains(t, rep.lastResult.Output, `"environments/project/values.yaml"`,
		"the repository-relative path a reviewer recognizes survives")

	require.Len(t, rep.logLines, 1)
	assert.Equal(t, "reading environments/project/values.yaml", rep.logLines[0].line,
		"streamed log lines are stripped too, not just the final result")

	// The local mirror deliberately keeps the absolute path: `kubectl
	// logs` is the one place it is still worth having.
	assert.Contains(t, stdout.String(), "/tmp/turnip-runner-")
}

// The transcript records argv and is given no access to the environment.
// Credentials reach a tool through the environment and mounted files, so
// this distinction is what makes recording the command safe at all.
func TestRunWith_TranscriptNeverCarriesTheEnvironment(t *testing.T) {
	t.Setenv("TURNIP_SECRET_PROBE", "super-secret-value")

	p := &fakePlugin{
		operations: []string{"diff"},
		result:     &plugin.ExecuteResult{ExitCode: 0, Output: "ok"},
	}
	rep := &fakeReporter{}
	var stdout, stderr safeBuffer

	exitCode := runWith(context.Background(), testRunConfig(), rep, fakeSelector(p), &stdout, &stderr)
	require.Equal(t, 0, exitCode)

	assert.NotContains(t, rep.lastResult.Output, "super-secret-value")
	assert.NotContains(t, stdout.String(), "super-secret-value")
}

// The Runner's log carries the tool's output once, mirrored line by line;
// the result line summarizes, with the whole output's size and the size
// of what is sent, and never repeats the output.
func TestRunWith_ResultLogLineSummarizesALargeOutput(t *testing.T) {
	output := strings.Repeat("+  a manifest line\n", 300_000)
	p := &fakePlugin{
		operations: []string{"diff"},
		script:     []outputLine{{"stdout", "+  a manifest line"}},
		result: &plugin.ExecuteResult{
			ExitCode:      0,
			Output:        output,
			ChangeSummary: plugin.ChangeSummary{Add: 4, Change: 1, Destroy: 2},
		},
	}
	rep := &fakeReporter{}
	var stdout, stderr safeBuffer

	exitCode := runWith(context.Background(), testRunConfig(), rep, fakeSelector(p), &stdout, &stderr)
	require.Equal(t, 0, exitCode)

	sent := len(rep.lastResult.Output)
	require.Less(t, sent, len(output))
	assert.Equal(t,
		fmt.Sprintf("runner: operation result: success=true exit_code=0 category=none add=4 change=1 destroy=2 output_bytes=%d sent_bytes=%d\n", len(output), sent),
		stderr.String(),
		"one summary line, with no error line and no copy of the output")
	assert.Equal(t, "+  a manifest line\n", stdout.String(), "the tool's output appears once, as mirrored")
}

func TestRunWith_ResultLogLineFollowsTheErrorMessage(t *testing.T) {
	p := &fakePlugin{operations: []string{"diff"}, resultErr: errors.New(`exec: "helmfile": executable file not found in $PATH`)}
	rep := &fakeReporter{}
	var stdout, stderr safeBuffer

	exitCode := runWith(context.Background(), testRunConfig(), rep, fakeSelector(p), &stdout, &stderr)
	require.Equal(t, 0, exitCode)

	assert.Equal(t,
		"runner: operation error: execute failed: exec: \"helmfile\": executable file not found in $PATH\n"+
			"runner: operation result: success=false exit_code=-1 category=tool_not_started add=0 change=0 destroy=0 output_bytes=0 sent_bytes=0\n",
		stderr.String())
}

// A result too large to deliver is summarized as what is sent: the
// failed result, its error on the line before.
func TestRunWith_ResultLogLineDescribesTheReplacement(t *testing.T) {
	p := &fakePlugin{
		operations: []string{"diff"},
		result:     &plugin.ExecuteResult{ExitCode: 0, Output: "planned", PlanData: make([]byte, 5<<20)},
	}
	rep := &fakeReporter{}
	var stdout, stderr safeBuffer

	exitCode := runWith(context.Background(), testRunConfig(), rep, fakeSelector(p), &stdout, &stderr)
	require.Equal(t, 0, exitCode)

	assert.False(t, rep.lastResult.Success)
	assert.Equal(t,
		"runner: operation error: the result was too large to deliver (5.0 MiB; the Server accepts at most 4 MiB)\n"+
			"runner: operation result: success=false exit_code=0 category=none add=0 change=0 destroy=0 output_bytes=7 sent_bytes=0\n",
		stderr.String())
}

// The summary line comes after the last line of the tool's output in the
// Runner's log, both streams included: the mirror is waited for before
// anything is said about the result.
func TestRunWith_ResultSummaryFollowsTheWholeOutput(t *testing.T) {
	var script []outputLine
	for i := range 2000 {
		script = append(script, outputLine{"stderr", fmt.Sprintf("line %d", i)})
	}
	p := &fakePlugin{operations: []string{"diff"}, script: script, result: &plugin.ExecuteResult{ExitCode: 0}}
	rep := &fakeReporter{}
	var stdout, stderr safeBuffer

	require.Equal(t, 0, runWith(context.Background(), testRunConfig(), rep, fakeSelector(p), &stdout, &stderr))

	log := stderr.String()
	last := strings.Index(log, "line 1999\n")
	require.GreaterOrEqual(t, last, 0)
	assert.Greater(t, strings.Index(log, "runner: operation result:"), last)
	assert.Len(t, rep.logLines, 2000, "and the reporter was handed every line")
}

// A Plugin that refuses the Operation before running anything leaves the
// Record open and empty; the Runner closes it, so neither reader hangs,
// and still logs the error and the summary.
func TestRunWith_AToolThatCannotStartStillEndsAndLogs(t *testing.T) {
	p := &fakePlugin{operations: []string{"diff"}, resultErr: errors.New("exec: \"helmfile\": executable file not found in $PATH")}
	rep := &fakeReporter{}
	var stdout, stderr safeBuffer

	require.Equal(t, 0, runWith(context.Background(), testRunConfig(), rep, fakeSelector(p), &stdout, &stderr))

	assert.Empty(t, stdout.String())
	assert.Contains(t, stderr.String(), "runner: operation error: execute failed:")
	assert.Contains(t, stderr.String(), "runner: operation result: success=false")
	assert.Empty(t, rep.logLines)
	assert.Equal(t, rpc.FailureToolNotStarted, rep.lastResult.FailureCategory)
}
