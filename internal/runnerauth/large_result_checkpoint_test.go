package runnerauth_test

// The checkpoint for fix-large-output (tasks.md 5.1).
//
// The Runner's unit tests prove the cut and the size check on their own;
// the rpc tests prove the Server refuses a message over its limit. Each can
// pass while a real result still does not fit: the result message carries
// more than its output (plan data, the other fields, protobuf framing), and
// the Server's limit is only as real as the option rpc.NewServer passes to
// gRPC. This test joins them: the real Runner (runner.Run, with the real
// Helmfile plugin running a stand-in helmfile that prints about 5 MB, and
// runner.NewReporter) reports over a real TCP connection to a Server built
// by rpc.NewServer, authenticated by the real runnerauth.Authenticator.

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivanvc/turnip/internal/jobs"
	"github.com/ivanvc/turnip/internal/plugin"
	"github.com/ivanvc/turnip/internal/rpc"
	"github.com/ivanvc/turnip/internal/runner"
	"github.com/ivanvc/turnip/internal/runnerauth"
)

const (
	// incidentOutputBytes is the size of the first-install plan output
	// that was lost (proposal.md), which the spec's scenario reuses.
	incidentOutputBytes = 5_069_133

	// maxSentOutputBytes is the most output a result carries, per the
	// operation-lifecycle delta.
	maxSentOutputBytes = 1 << 20

	largeResultProjectDir = "infra/web"

	// nominalTrailer is a trailer of typical length. The real one carries
	// the run's duration, so the whole output lands within a few bytes of
	// incidentOutputBytes rather than on it.
	nominalTrailer = "@@ turnip: exit 0 in 1.234s @@"

	// earlyReleases are releases whose diffs open the output and are cut
	// from what is sent; the last release's diff fills the rest.
	earlyReleases = 3
)

// countingAuthenticator wraps the real Authenticator and counts every
// stream or call the Server admitted, which is every attempt the Runner
// made: a reporter that retried would open a second stream.
type countingAuthenticator struct {
	inner rpc.Authenticator
	calls atomic.Int32
}

func (a *countingAuthenticator) Authenticate(ctx context.Context, token, operationID string) (rpc.Identity, error) {
	a.calls.Add(1)
	return a.inner.Authenticate(ctx, token, operationID)
}

// largeResultServer runs the real Server on a loopback TCP listener, since
// runner.NewReporter dials an address, and returns that address with the
// handler recording what reached it and the authenticator counting the
// Runner's attempts. It also writes the runner container's projected
// identity to a file, as kubelet would, and returns its path.
func largeResultServer(t *testing.T) (addr, tokenFile string, handler *checkpointHandler, auth *countingAuthenticator) {
	t.Helper()

	shape := provisioningShapes["run-in-image"]
	job, pod := buildOperationPod(t, shape.tool, shape.version, shape.spec)
	auth = &countingAuthenticator{inner: runnerauth.New(newCluster(t, job, pod), checkpointNamespace)}
	handler = &checkpointHandler{}

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	server := rpc.NewServer(handler, rpc.WithAuthenticator(auth))
	go func() { _ = server.Serve(lis) }()
	t.Cleanup(server.Stop)

	identity := identityOf(t, pod, runnerContainerName)
	require.Equal(t, jobs.ReportAudience, identity.audience)
	tokenFile = filepath.Join(t.TempDir(), "token")
	require.NoError(t, os.WriteFile(tokenFile, []byte(mint(string(pod.UID), identity.audience)+"\n"), 0o600))

	return lis.Addr().String(), tokenFile, handler, auth
}

// firstInstallPlan returns the tool's lines for a `helmfile diff` whose
// whole output, with the transcript's two header lines and a trailer of
// nominal length, is incidentOutputBytes: earlyReleases small diffs, then
// one release whose diff is every remaining line, numbered so the first
// line that survives the cut says how many came before it.
func firstInstallPlan(header []string) []string {
	var lines []string
	for i := range earlyReleases {
		lines = append(lines,
			fmt.Sprintf("Comparing release=crds-%d, chart=charts/crds", i),
			"+ kind: CustomResourceDefinition",
		)
	}
	lines = append(lines, "Comparing release=web, chart=charts/web")

	// Every line of the record but the last is followed by a newline.
	fixed := len(nominalTrailer)
	for _, l := range header {
		fixed += len(l) + 1
	}
	used := fixed
	for _, l := range lines {
		used += len(l) + 1
	}

	const width = 99
	for n := 0; incidentOutputBytes-used > 2*(width+1); n++ {
		l := fmt.Sprintf("+   description: field %07d %s", n, strings.Repeat("x", width))[:width]
		lines = append(lines, l)
		used += width + 1
	}
	last := "+ end of the CRDs "
	last += strings.Repeat("y", incidentOutputBytes-used-1-len(last))
	return append(lines, last)
}

// runQuietly runs fn with the process's stdout and stderr sent to files,
// so the Runner's mirror of 5 MB of tool output stays out of the test's
// own output, and returns what fn wrote to each.
func runQuietly(t *testing.T, fn func()) (stdout, stderr []byte) {
	t.Helper()
	dir := t.TempDir()
	outFile, err := os.Create(filepath.Join(dir, "stdout"))
	require.NoError(t, err)
	errFile, err := os.Create(filepath.Join(dir, "stderr"))
	require.NoError(t, err)

	prevOut, prevErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outFile, errFile
	func() {
		defer func() { os.Stdout, os.Stderr = prevOut, prevErr }()
		fn()
	}()

	require.NoError(t, outFile.Close())
	require.NoError(t, errFile.Close())
	stdout, err = os.ReadFile(outFile.Name())
	require.NoError(t, err)
	stderr, err = os.ReadFile(errFile.Name())
	require.NoError(t, err)
	return stdout, stderr
}

var (
	omissionLineRe = regexp.MustCompile(`^@@ turnip: (\d+) earlier lines omitted \(([^)]+)\) @@$`)
	trailerRe      = regexp.MustCompile(`^@@ turnip: exit 0 in \S+ @@$`)
)

// A first install's plan prints about 5 MB. The Runner sends at most 1 MiB
// of it, keeping the end and saying how much of the start it omitted, and
// the result reaches the Server on its first attempt with the change
// counts of the whole output.
func TestCheckpoint_LargeOutputArrivesCutOnTheFirstAttempt(t *testing.T) {
	addr, tokenFile, handler, auth := largeResultServer(t)

	header := []string{
		"@@ turnip: " + largeResultProjectDir + ", helmfile " + provisioningShapes["run-in-image"].version + " @@",
		"@@ turnip: helmfile --no-color diff @@",
	}
	toolLines := firstInstallPlan(header)

	// A stand-in helmfile on the Runner's tools path, printing the plan.
	toolsDir := t.TempDir()
	planFile := filepath.Join(t.TempDir(), "plan.txt")
	require.NoError(t, os.WriteFile(planFile, []byte(strings.Join(toolLines, "\n")+"\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(toolsDir, "helmfile"), []byte("#!/bin/sh\nexec cat '"+planFile+"'\n"), 0o700)) //nolint:gosec // the stand-in must be executable

	workspace := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(workspace, largeResultProjectDir), 0o750))

	// runner.Run prepends ToolsDir to PATH; restore it afterwards.
	t.Setenv("PATH", os.Getenv("PATH"))

	cfg := runner.Config{
		ServerAddr:   addr,
		OperationID:  checkpointOperationID,
		ProjectName:  "web",
		ProjectDir:   largeResultProjectDir,
		Tool:         "helmfile",
		ToolVersion:  provisioningShapes["run-in-image"].version,
		Operation:    "diff",
		RepoURL:      "https://github.com/acme/repo.git",
		CommitSHA:    "abc123",
		ToolsDir:     toolsDir,
		WorkspaceDir: workspace,
		TokenFile:    tokenFile,
	}

	var exitCode int
	stdout, stderr := runQuietly(t, func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		exitCode = runner.Run(ctx, cfg)
	})
	require.Equal(t, 0, exitCode, "the Runner delivered its result; its log:\n%s", stderr)

	assert.Equal(t, int32(1), auth.calls.Load(), "the Runner opened one stream: no attempt was retried")
	_, resultFor, results := handler.snapshot()
	require.Len(t, results, 1, "the handler received the result exactly once")
	assert.Equal(t, []string{checkpointOperationID}, resultFor)
	got := results[0]

	assert.True(t, got.Success)
	assert.Equal(t, int32(0), got.ExitCode)
	assert.Equal(t, rpc.FailureUnspecified, got.FailureCategory)
	assert.Empty(t, got.ErrorMessage)
	assert.Empty(t, got.PlanData)
	assert.Equal(t, rpc.ChangeSummary{Change: earlyReleases + 1}, got.Changes,
		"the counts are read from the whole output, including releases cut from what was sent")

	output := got.Output
	require.LessOrEqual(t, len(output), maxSentOutputBytes, "at most 1 MiB of output is sent")

	// The leading annotation lines, then the omission line, then the end
	// of the tool's output, then the trailer.
	lines := strings.Split(output, "\n")
	require.Greater(t, len(lines), len(header)+2)
	assert.Equal(t, header, lines[:len(header)], "the leading annotation lines are kept")

	m := omissionLineRe.FindStringSubmatch(lines[len(header)])
	require.NotNil(t, m, "the omission line follows the leading annotation lines: %q", lines[len(header)])
	omitted, err := strconv.Atoi(m[1])
	require.NoError(t, err)
	assert.Equal(t, "3.8 MiB", m[2])

	trailer := lines[len(lines)-1]
	assert.Regexp(t, trailerRe, trailer, "the closing annotation line is kept")

	require.Less(t, omitted, len(toolLines))
	assert.Positive(t, omitted)
	assert.Equal(t, toolLines[omitted:], lines[len(header)+1:len(lines)-1],
		"what follows the omission line is the end of the tool's output, from the first line not counted as omitted")

	// The whole output, as the plugin saw it, from the pieces the result
	// carries and the lines the cut removed.
	whole := len(trailer)
	for _, l := range header {
		whole += len(l) + 1
	}
	for _, l := range toolLines {
		whole += len(l) + 1
	}
	assert.InDelta(t, incidentOutputBytes, whole, 16, "the plan's output is the incident's size, give or take the trailer's duration")

	// The Runner's log has the tool's output once, on stdout beside the
	// transcript's own lines, and a summary of the result, never the
	// output again. Compared without a diff, which would print 5 MB.
	assert.Equal(t, 1, strings.Count(string(stdout), "\n"+strings.Join(toolLines, "\n")+"\n"), "the tool's whole output is mirrored to stdout, once")
	assert.NotContains(t, string(stderr), toolLines[0], "nor repeated on stderr")
	assert.Contains(t, string(stderr), fmt.Sprintf(
		"runner: operation result: success=true exit_code=0 category=none add=0 change=%d destroy=0 output_bytes=%d sent_bytes=%d\n",
		earlyReleases+1, whole, len(output)))
	assert.Less(t, len(stderr), 64<<10, "the Runner's own log does not repeat the output")
}

// A result whose plan data alone exceeds what the Server accepts is never
// sent: the failed "too large to deliver" result goes in its place, on the
// stream already open, in one attempt and in seconds rather than the
// 15-minute delivery budget.
func TestCheckpoint_ResultTooLargeToDeliverFailsFast(t *testing.T) {
	addr, tokenFile, handler, auth := largeResultServer(t)

	rep, closeConn, err := runner.NewReporter(addr, runner.Config{
		OperationID: checkpointOperationID,
		ProjectName: "web",
		ProjectDir:  largeResultProjectDir,
		Tool:        "helmfile",
		Operation:   "diff",
		TokenFile:   tokenFile,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = closeConn() })

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	started := time.Now()
	require.NoError(t, rep.Connect(ctx))
	require.NoError(t, rep.Report(ctx, runner.OperationResult{
		Success:  true,
		Output:   "planned",
		Changes:  plugin.ChangeSummary{Change: 1},
		PlanData: []byte(strings.Repeat("p", 5<<20)),
	}))
	elapsed := time.Since(started)

	assert.Less(t, elapsed, 5*time.Second, "the exchange takes seconds, not the retry budget")
	assert.Equal(t, int32(1), auth.calls.Load(),
		"one stream: the oversized result was never sent, so the Server never refused it and nothing was retried")

	_, resultFor, results := handler.snapshot()
	require.Len(t, results, 1, "the handler received exactly one result")
	assert.Equal(t, []string{checkpointOperationID}, resultFor)
	assert.Equal(t, rpc.OperationResult{
		Success:      false,
		ExitCode:     0,
		ErrorMessage: "the result was too large to deliver (5.0 MiB; the Server accepts at most 4 MiB)",
	}, normalized(results[0]), "a failed result with no output, no plan data, no counts and no category")
}

// normalized maps an empty plan data slice to nil, since the Server hands
// the handler whatever protobuf decoded and the test asserts there is none.
func normalized(r rpc.OperationResult) rpc.OperationResult {
	if len(r.PlanData) == 0 {
		r.PlanData = nil
	}
	return r
}
