package runner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ivanvc/turnip/internal/plugin"
	"github.com/ivanvc/turnip/internal/rpc"
)

// resultReporter is the subset of *reporter's behavior run.go depends on,
// so tests can substitute a fake (task 15.8) without a real gRPC
// connection.
type resultReporter interface {
	Connect(ctx context.Context) error
	// Follow starts sending rec's lines to the Server, each passed
	// through clean first, and returns at once.
	Follow(rec *plugin.Record, clean func(string) string)
	Report(ctx context.Context, result OperationResult) error
}

// errStopRequested is the cause the Runner cancels the tool's context with
// when it is told to stop (a SIGTERM, from cmd/runner).
var errStopRequested = errors.New("stop requested")

// reportBoundAfterStop is how long the Runner has to report once it was
// told to stop: with the Pod's 60-second grace period, it leaves the tool
// its 30 seconds to exit and a margin before the kubelet's kill. A
// variable so tests can shorten it.
var reportBoundAfterStop = 20 * time.Second

// The error messages of a result whose tool turnip stopped. The idle
// timeout is formatted into the first, as a Go duration ("30m0s").
const (
	idleStopMessage      = "The tool wrote nothing for %s, so turnip stopped it."
	requestedStopMessage = "The Runner was told to stop (its deadline, an eviction or a deletion), so turnip stopped the tool."
)

type pluginSelector func(tool string) (plugin.Plugin, error)

type cloner func(ctx context.Context, dir, repoURL, commitSHA, baseRef, submodules string) error

// selectPlugin resolves cfg.Tool to a Plugin (Requirement 6.1) through the
// same registry the Server validates turnip.yaml against, so the Runner
// accepts exactly the tools the Server dispatches. An unregistered tool
// still fails here, before connecting or cloning: a Job naming one was
// built by a Server with a different registry, and nothing after this
// point could do anything useful with it.
func selectPlugin(tool string) (plugin.Plugin, error) {
	p, ok := plugin.Registry()[tool]
	if !ok {
		return nil, fmt.Errorf("runner: unrecognized tool %q", tool)
	}
	return p, nil
}

// pathWithToolsDir prepends toolsDir to path (PATH's existing value),
// composing with whatever the container image already provides. Kubernetes'
// Job spec can't express this itself — its $(VAR) env-value substitution
// only resolves references to other variables declared in the Pod spec,
// never a container image's own baked-in PATH — so Run does it once, in the
// Runner's own process, before anything looks up a binary. An empty
// toolsDir leaves path untouched.
func pathWithToolsDir(toolsDir, path string) string {
	if toolsDir == "" {
		return path
	}
	return toolsDir + string(os.PathListSeparator) + path
}

// Run executes cfg's Operation end-to-end (connect, dispatch, report) and
// returns the process exit code. It is RunWithStop with no stop.
func Run(ctx context.Context, cfg Config) int {
	return RunWithStop(ctx, cfg, nil)
}

// RunWithStop is Run for a Runner that can be told to stop: once stop is
// closed (cmd/runner closes it on SIGTERM), the tool's context is
// cancelled, which interrupts the tool (see plugin.ExecuteOptions), and the
// result is reported as stopped within reportBoundAfterStop. ctx itself is
// never cancelled by the stop: connecting and reporting use it, so a stop
// does not cut the Runner off from the Server it still has to report to.
func RunWithStop(ctx context.Context, cfg Config, stop <-chan struct{}) int {
	_ = os.Setenv("PATH", pathWithToolsDir(cfg.ToolsDir, os.Getenv("PATH")))

	rep, closeConn, err := NewReporter(cfg.ServerAddr, cfg)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "runner: %v\n", err)
		return 1
	}
	defer func() { _ = closeConn() }()

	return runWithStop(ctx, cfg, rep, selectPlugin, stop, os.Stdout, os.Stderr)
}

// RunClone performs one Operation's repository clone and returns the exit
// code cmd/runner/main.go should use. It runs in an initContainer, before
// the container that executes the tool exists.
//
// A failure is reported to the Server from here rather than left for the
// Runner to notice, because there is no Runner yet: an initContainer that
// exits non-zero means the main container never starts, so nothing would
// ever connect. Without this the failure would surface only when the
// Server's start-deadline sweep claimed the Operation minutes later, and
// would say "timed out" rather than whatever git actually said.
func RunClone(ctx context.Context, cfg Config) int {
	rep, closeConn, err := NewReporter(cfg.ServerAddr, cfg)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "runner: %v\n", err)
		return 1
	}
	defer func() { _ = closeConn() }()

	return runCloneWith(ctx, cfg, rep, Clone, os.Stderr)
}

// runCloneWith is RunClone's testable core, mirroring runWith's seam so a
// test can substitute a fake reporter and cloner.
func runCloneWith(ctx context.Context, cfg Config, rep resultReporter, clone cloner, stderr io.Writer) int {
	// Unlike execute's workspace, this one cannot fall back to a
	// temporary directory: it would be removed when this process exits,
	// leaving the container that runs the tool with an empty checkout.
	// An unset workspace is a configuration error here, not a default.
	if cfg.WorkspaceDir == "" {
		return reportCloneFailure(ctx, rep, "clone: TURNIP_WORKSPACE_DIR is required when cloning", stderr)
	}

	if err := clone(ctx, cfg.WorkspaceDir, cfg.RepoURL, cfg.CommitSHA, cfg.BaseRef, cfg.CloneSubmodules); err != nil {
		// Same wording and same path-stripping the Runner used when it
		// owned the clone, so what reaches the pull request is unchanged.
		message := cleanForServer(cfg.WorkspaceDir, fmt.Sprintf("clone failed: %v", err))
		return reportCloneFailure(ctx, rep, message, stderr)
	}

	return 0
}

// reportCloneFailure delivers the failure as the Operation's own result,
// then returns a non-zero exit code. Every step is best-effort: if the
// Server cannot be reached, the start-deadline sweep remains the backstop
// it already is, so a reporting failure must not mask the clone failure.
func reportCloneFailure(ctx context.Context, rep resultReporter, message string, stderr io.Writer) int {
	result := OperationResult{Success: false, ExitCode: -1, ErrorMessage: message, FailureCategory: rpc.FailureCloneFailed}
	// The same two lines every finished Operation logs: the error, then
	// the summary. A clone failure has no output.
	logResultSummary(stderr, result, 0)

	if err := rep.Connect(ctx); err != nil {
		_, _ = fmt.Fprintf(stderr, "runner: connect to server: %v\n", err)
		return 1
	}
	if err := rep.Report(ctx, result); err != nil {
		_, _ = fmt.Fprintf(stderr, "runner: report result: %v\n", err)
	}
	return 1
}

// runWith is runWithStop for a Runner never told to stop.
func runWith(ctx context.Context, cfg Config, rep resultReporter, selectPlugin pluginSelector, stdout, stderr io.Writer) int {
	return runWithStop(ctx, cfg, rep, selectPlugin, nil, stdout, stderr)
}

// runWithStop is RunWithStop's testable core: stop is injected, so a test
// stops the Runner without sending a signal to its own process. A nil stop
// never fires.
func runWithStop(ctx context.Context, cfg Config, rep resultReporter, selectPlugin pluginSelector, stop <-chan struct{}, stdout, stderr io.Writer) int {
	p, err := selectPlugin(cfg.Tool)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "runner: %v\n", err)
		return 1
	}

	// Connect before cloning/executing so a Runner that can never reach
	// the Server at all fails fast (Requirement 2.3), rather than running
	// a full clone and plan/apply first only to discover it can't report
	// the outcome.
	if err := rep.Connect(ctx); err != nil {
		_, _ = fmt.Fprintf(stderr, "runner: connect to server: %v\n", err)
		return 1
	}

	// The stop cancels the tool's context only. Reporting below stays on
	// ctx, so the result of a stopped tool can still be delivered.
	toolCtx, cancelTool := context.WithCancelCause(ctx)
	defer cancelTool(nil)
	go func() {
		select {
		case <-stop:
			_, _ = fmt.Fprintln(stderr, "runner: told to stop; interrupting the tool")
			cancelTool(errStopRequested)
		case <-toolCtx.Done():
		}
	}()

	opResult, outputBytes := execute(toolCtx, cfg, p, rep, stdout, stderr)

	// Checked here as well as in Report, so the summary below describes
	// the result that is sent, not one that never will be.
	opResult, _ = deliverable(opResult)

	// Logged unconditionally, before reporting, so a `kubectl logs` is
	// never empty-handed even if the Server never acknowledges the result
	// (Requirement 4.5). A summary, never the output: execute already
	// mirrored every line of it, and waited for the last.
	logResultSummary(stderr, opResult, outputBytes)

	if err := reportBounded(ctx, rep, opResult, stop); err != nil {
		_, _ = fmt.Fprintf(stderr, "runner: report result: %v\n", err)
		return 1
	}
	return 0
}

// reportBounded reports result on a context of its own, derived from ctx
// rather than from the tool's context, so a stop that interrupted the tool
// does not also cancel the report. Once stop has fired, whether before the
// report started or while it is under way, the report has
// reportBoundAfterStop from then on, after which reportBounded returns
// without waiting further: past it the Pod is about to be killed, and an
// unreported Operation is the sweep's to finalize.
func reportBounded(ctx context.Context, rep resultReporter, result OperationResult, stop <-chan struct{}) error {
	reportCtx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)

	bound := reportBoundAfterStop
	errBound := fmt.Errorf("no acknowledgment within %s of being told to stop", bound)
	go func() {
		select {
		case <-stop:
		case <-reportCtx.Done():
			return
		}
		timer := time.NewTimer(bound)
		defer timer.Stop()
		select {
		case <-timer.C:
			cancel(errBound)
		case <-reportCtx.Done():
		}
	}()

	// Report retries on its own budget and returns once reportCtx ends
	// only between attempts; the select ends the wait at the bound itself.
	reported := make(chan error, 1)
	go func() { reported <- rep.Report(reportCtx, result) }()
	select {
	case err := <-reported:
		return err
	case <-reportCtx.Done():
		return context.Cause(reportCtx)
	}
}

// resolveWorkspace returns the directory to clone into, together with a
// cleanup to run once the Operation finishes.
//
// A configured directory is a volume mount point the Pod owns: it is used
// as-is and never removed. Removing it would empty a mount turnip doesn't
// own the lifecycle of, and there is nothing to reclaim anyway — the Pod
// and its emptyDir are torn down together.
//
// An empty directory means nothing provided one — a test, or a Runner run
// by hand outside a turnip-built Job — so a temporary directory is
// created and removed afterwards, exactly as every Runner behaved before
// the workspace volume existed.
func resolveWorkspace(dir string) (string, func(), error) {
	if dir != "" {
		return dir, func() {}, nil
	}

	tmp, err := os.MkdirTemp("", "turnip-runner-")
	if err != nil {
		return "", func() {}, err
	}
	return tmp, func() { _ = os.RemoveAll(tmp) }, nil
}

// execute dispatches to p, translating the outcome — including a dispatch
// failure (Requirement 5.3) — into an OperationResult to report, rather
// than ever crashing without a reported outcome.
//
// The repository is already present: an initContainer cloned it into the
// workspace before this container started (see RunClone). What remains
// here is resolving the path, which still falls back to a temporary
// directory for tests and hand-runs outside a turnip-built Job.
//
// The second return value is the size of the whole output, before
// cutOutput shortened the copy in the result.
func execute(ctx context.Context, cfg Config, p plugin.Plugin, rep resultReporter, stdout, stderr io.Writer) (OperationResult, int) {
	dir, cleanup, err := resolveWorkspace(cfg.WorkspaceDir)
	if err != nil {
		// There is no workspace path to strip yet, but the message names
		// TMPDIR, whose bytes may not be valid UTF-8.
		return OperationResult{Success: false, ExitCode: -1, ErrorMessage: cleanForServer("", fmt.Sprintf("create workdir: %v", err)), FailureCategory: rpc.FailureWorkspaceFailed}, 0
	}
	defer cleanup()

	// clean is what leaves for the Server: the workspace path removed (a
	// reviewer reading the PR comment knows repository-relative paths, not
	// the directory turnip cloned into) and each run of bytes that is not
	// valid UTF-8 replaced, since a message holding them cannot be
	// encoded. See cleanForServer.
	//
	// Redaction belongs here rather than at the command seam because this
	// is where the secret is — internal/plugin has no business holding
	// one. The execution transcript is inside result.Output by the time
	// this runs, so one pass covers turnip's own lines and the tool's
	// alike.
	//
	// There is no longer a token to scrub here. The Runner's process
	// holds no GitHub credential at all: git asks turnip's credential
	// helper for one at the moment it needs it, and that is a separate
	// invocation whose output goes to git rather than to this stream.
	// The redaction this used to perform guarded a hazard that no longer
	// exists — and Slice 24's tests assert the absence rather than trust
	// this comment.
	clean := func(s string) string { return cleanForServer(dir, s) }

	// The tool's output is recorded once, in rec, and two readers follow
	// the record while the tool runs, each at its own pace: the mirror,
	// for this container's own log, and the reporter's sender, for the
	// Server. Neither can slow the other, the record or the tool
	// (Requirement 4.7): a Server that reads slowly only delays its own
	// copy.
	//
	// The mirror keeps the absolute path and any secret the tool printed:
	// `kubectl logs` is the one place those are still worth having.
	// Only what leaves for the Server is cleaned, and it is cleaned the
	// same way the final output is, so a token cannot reach a live viewer
	// by a route the comment closes.
	rec := plugin.NewRecord()
	mirrored := mirror(rec, stdout, stderr)
	rep.Follow(rec, clean)

	result, err := p.Execute(ctx, cfg.Operation, plugin.ExecuteOptions{
		WorkingDir:  filepath.Join(dir, cfg.ProjectDir),
		Tool:        cfg.Tool,
		ToolVersion: cfg.ToolVersion,
		Config:      cfg.ToolConfig,
		ExtraArgs:   cfg.ExtraArgs,
		PlanData:    cfg.PlanData,
		Record:      rec,
		IdleTimeout: cfg.IdleTimeout,
	})
	// Execute closes the record of a command it ran. One it refused
	// before running anything is closed here, so both readers end.
	rec.Close()
	// The whole output is in this container's log before anything is said
	// about the result.
	<-mirrored
	if err != nil && errors.Is(context.Cause(ctx), errStopRequested) {
		// Told to stop before the tool could start: there is no tool to
		// interrupt, and the result says why there is no output.
		return OperationResult{Success: false, ExitCode: -1, ErrorMessage: requestedStopMessage, FailureCategory: rpc.FailureStopped}, 0
	}
	if err != nil {
		// No tool process produced an exit code: the binary is missing, or
		// the Plugin refused the Operation before running anything.
		return OperationResult{Success: false, ExitCode: -1, ErrorMessage: clean(fmt.Sprintf("execute failed: %v", err)), FailureCategory: rpc.FailureToolNotStarted}, 0
	}

	// Cut after the plugin, which has read its change counts from the
	// whole output, and after cleaning, so the limit applies to what the
	// Server receives. PlanData is never cut.
	output := clean(result.Output)
	opResult := OperationResult{
		Success:  result.ExitCode == 0,
		Output:   cutOutput(output),
		ExitCode: result.ExitCode,
		Changes:  result.ChangeSummary,
		PlanData: result.PlanData,
	}
	if result.Error != nil {
		opResult.ErrorMessage = clean(result.Error.Error())
	}
	if result.ExitCode != 0 {
		opResult.FailureCategory = rpc.FailureToolExited
	}
	// A tool turnip stopped failed, whatever it exited with: an exit 0 on
	// the interrupt is not the Operation succeeding. Its exit code is
	// kept, and the message says why it was stopped.
	switch result.Stopped {
	case plugin.StopIdle:
		opResult.Success = false
		opResult.FailureCategory = rpc.FailureTimedOut
		opResult.ErrorMessage = fmt.Sprintf(idleStopMessage, cfg.IdleTimeout)
	case plugin.StopRequested:
		opResult.Success = false
		opResult.FailureCategory = rpc.FailureStopped
		opResult.ErrorMessage = requestedStopMessage
	}
	return opResult, len(output)
}

// mirror copies rec to this container's own output as it grows: the
// tool's standard error to stderr, everything else (its standard output
// and turnip's annotation lines) to stdout. The channel it returns is
// closed once the last line of the closed record has been written.
func mirror(rec *plugin.Record, stdout, stderr io.Writer) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		from := 0
		for {
			lines, closed := rec.Next(context.Background(), from)
			for _, l := range lines {
				w := stdout
				if l.Stream == plugin.Stderr {
					w = stderr
				}
				_, _ = fmt.Fprintln(w, l.Text)
			}
			from += len(lines)
			if closed {
				return
			}
		}
	}()
	return done
}

// logResultSummary writes the Runner's one line about its result, with the
// result's error message on a line of its own just before it when there is
// one. outputBytes is the whole output's size; sent_bytes is the size of
// the output the result carries.
func logResultSummary(w io.Writer, result OperationResult, outputBytes int) {
	if result.ErrorMessage != "" {
		_, _ = fmt.Fprintf(w, "runner: operation error: %s\n", result.ErrorMessage)
	}
	// The wire enum's name without its prefix: "tool_exited", say.
	category := "none"
	if result.FailureCategory != rpc.FailureUnspecified {
		name := rpc.FailureCategoryToProto(result.FailureCategory).String()
		category = strings.ToLower(strings.TrimPrefix(name, "FAILURE_CATEGORY_"))
	}
	_, _ = fmt.Fprintf(w, "runner: operation result: success=%t exit_code=%d category=%s add=%d change=%d destroy=%d output_bytes=%d sent_bytes=%d\n",
		result.Success, result.ExitCode, category,
		result.Changes.Add, result.Changes.Change, result.Changes.Destroy,
		outputBytes, len(result.Output))
}
