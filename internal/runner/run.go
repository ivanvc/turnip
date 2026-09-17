package runner

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/ivanvc/turnip/internal/plugin"
)

// resultReporter is the subset of *reporter's behavior run.go depends on,
// so tests can substitute a fake (task 15.8) without a real gRPC
// connection.
type resultReporter interface {
	Connect(ctx context.Context) error
	LogLine(stream, line string)
	Report(ctx context.Context, result OperationResult) error
}

type pluginSelector func(tool string) (plugin.Plugin, error)

type cloner func(ctx context.Context, dir, repoURL, commitSHA, baseRef, token string) error

// selectPlugin resolves cfg.Tool to a Plugin (Requirement 6.1). Today only
// "helmfile" (Slice 2) exists; Slice 7 adds Terraform and Pulumi.
func selectPlugin(tool string) (plugin.Plugin, error) {
	switch tool {
	case "helmfile":
		return plugin.NewHelmfilePlugin(), nil
	default:
		return nil, fmt.Errorf("runner: unrecognized tool %q", tool)
	}
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

// Run executes cfg's Operation end-to-end — connect, clone, dispatch,
// report — and returns the process exit code cmd/runner/main.go should
// use.
func Run(ctx context.Context, cfg Config) int {
	_ = os.Setenv("PATH", pathWithToolsDir(cfg.ToolsDir, os.Getenv("PATH")))

	rep, closeConn, err := NewReporter(cfg.ServerAddr, cfg)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "runner: %v\n", err)
		return 1
	}
	defer func() { _ = closeConn() }()

	return runWith(ctx, cfg, rep, selectPlugin, Clone, os.Stdout, os.Stderr)
}

func runWith(ctx context.Context, cfg Config, rep resultReporter, selectPlugin pluginSelector, clone cloner, stdout, stderr io.Writer) int {
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

	opResult := execute(ctx, cfg, p, clone, rep, stdout, stderr)

	// Logged unconditionally, in addition to (not instead of) the
	// real-time mirroring OnOutput already did throughout execution, so a
	// `kubectl logs` is never empty-handed even if the Server never
	// acknowledged the result (Requirement 4.5).
	_, _ = fmt.Fprintf(stderr, "runner: operation result: %+v\n", opResult)

	if err := rep.Report(ctx, opResult); err != nil {
		_, _ = fmt.Fprintf(stderr, "runner: report result: %v\n", err)
		return 1
	}
	return 0
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

// execute clones the repository and dispatches to p, translating the
// outcome — including a clone or dispatch failure (Requirement 5.3) — into
// an OperationResult to report, rather than ever crashing without a
// reported outcome.
func execute(ctx context.Context, cfg Config, p plugin.Plugin, clone cloner, rep resultReporter, stdout, stderr io.Writer) OperationResult {
	dir, cleanup, err := resolveWorkspace(cfg.WorkspaceDir)
	if err != nil {
		return OperationResult{Success: false, ExitCode: -1, ErrorMessage: fmt.Sprintf("create workdir: %v", err)}
	}
	defer cleanup()

	// strip removes dir from anything headed for the Server: a reviewer
	// reading the PR comment knows repository-relative paths, not the
	// directory turnip cloned into. See stripWorkspacePath.
	strip := func(s string) string { return stripWorkspacePath(dir, s) }

	if err := clone(ctx, dir, cfg.RepoURL, cfg.CommitSHA, cfg.BaseRef, cfg.GitHubToken); err != nil {
		return OperationResult{Success: false, ExitCode: -1, ErrorMessage: strip(fmt.Sprintf("clone failed: %v", err))}
	}

	// OnOutput fans out to two places for every (stream, line), and the
	// first must never wait on the second (Requirement 4.7): write it to
	// the matching local stream synchronously, then hand it to the
	// reporter, which may block or take time reporting to the Server.
	onOutput := func(stream, line string) {
		switch stream {
		case "stdout":
			_, _ = fmt.Fprintln(stdout, line)
		case "stderr":
			_, _ = fmt.Fprintln(stderr, line)
		}
		// The local mirror above keeps the absolute path; only what
		// leaves for the Server is stripped.
		rep.LogLine(stream, strip(line))
	}

	result, err := p.Execute(ctx, cfg.Operation, plugin.ExecuteOptions{
		WorkingDir: filepath.Join(dir, cfg.ProjectDir),
		Config:     cfg.ToolConfig,
		ExtraArgs:  cfg.ExtraArgs,
		PlanData:   cfg.PlanData,
		OnOutput:   onOutput,
	})
	if err != nil {
		return OperationResult{Success: false, ExitCode: -1, ErrorMessage: strip(fmt.Sprintf("execute failed: %v", err))}
	}

	opResult := OperationResult{
		Success:  result.ExitCode == 0,
		Output:   strip(result.Output),
		ExitCode: result.ExitCode,
		Changes:  result.ChangeSummary,
		PlanData: result.PlanData,
	}
	if result.Error != nil {
		opResult.ErrorMessage = strip(result.Error.Error())
	}
	return opResult
}
