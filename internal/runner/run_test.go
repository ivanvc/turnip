package runner

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"

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
func (p *fakePlugin) ActsWithoutChanges() bool  { return false }
func (p *fakePlugin) Provisioning() provisioning.Spec {
	return provisioning.Spec{Strategy: provisioning.RunInImage, Image: "ghcr.io/helmfile/helmfile"}
}
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

	exitCode := runWith(context.Background(), testRunConfig(), rep, fakeSelector(p), &stdout, &stderr)

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
		done <- runWith(context.Background(), testRunConfig(), rep, fakeSelector(p), &stdout, &stderr)
	}()

	<-entered // LogLine("stdout", "blocking-line") has been entered and is now blocked
	assert.Contains(t, stdout.String(), "blocking-line", "the local write must complete before LogLine, not after it returns")
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
func (p *echoWorkingDirPlugin) Execute(_ context.Context, _ string, opts plugin.ExecuteOptions) (*plugin.ExecuteResult, error) {
	if opts.OnOutput != nil {
		opts.OnOutput("stdout", "reading "+opts.WorkingDir+"/values.yaml")
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
