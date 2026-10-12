package runnerauth_test

// The checkpoint for fix-output-order (tasks.md 6.1).
//
// Every piece of this change passes its own tests while the fault it fixes
// can still be there: the order is lost between pieces, when something
// downstream of the record makes a reader wait. These tests join them: the
// real Runner (runner.Run, with the real Helmfile plugin running a stand-in
// helmfile that prints 20,000 lines to standard output in one burst and
// then 5 errors, and runner.NewReporter) reports over a real TCP
// connection to a Server that reads slowly.
//
// rpc.NewServer hands its handler only a stream's first log line, so it
// cannot show the order of the rest. The first test therefore uses a
// Server of its own that keeps every message; the second uses rpc.NewServer
// with a slow handler, for what only it can show.

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	pb "github.com/ivanvc/turnip/internal/grpc/turnip/v1"
	"github.com/ivanvc/turnip/internal/jobs"
	"github.com/ivanvc/turnip/internal/rpc"
	"github.com/ivanvc/turnip/internal/runner"
	"github.com/ivanvc/turnip/internal/runnerauth"
)

const (
	orderStdoutLines = 20000
	orderStderrLines = 5
	orderProjectDir  = "infra/web"
)

// failingBurstHelmfile writes a stand-in helmfile that prints
// orderStdoutLines lines to standard output in one burst, then
// orderStderrLines errors, and exits 1: the shape of a real failed
// `helmfile diff`, which prints every release's diff at once and its
// errors after.
func failingBurstHelmfile(t *testing.T) (toolsDir string) {
	t.Helper()
	toolsDir = t.TempDir()
	script := fmt.Sprintf("#!/bin/sh\nseq -f 'manifest line %%g' 1 %d\ni=1; while [ $i -le %d ]; do echo \"error $i\" >&2; i=$((i+1)); done\nexit 1\n",
		orderStdoutLines, orderStderrLines)
	require.NoError(t, os.WriteFile(filepath.Join(toolsDir, "helmfile"), []byte(script), 0o700)) //nolint:gosec // the stand-in must be executable
	return toolsDir
}

func orderRunnerConfig(t *testing.T, addr, tokenFile string) runner.Config {
	t.Helper()
	workspace := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(workspace, orderProjectDir), 0o750))

	// runner.Run prepends ToolsDir to PATH; restore it afterwards.
	t.Setenv("PATH", os.Getenv("PATH"))

	return runner.Config{
		ServerAddr:   addr,
		OperationID:  checkpointOperationID,
		ProjectName:  "web",
		ProjectDir:   orderProjectDir,
		Tool:         "helmfile",
		ToolVersion:  provisioningShapes["run-in-image"].version,
		Operation:    "diff",
		RepoURL:      "https://github.com/acme/repo.git",
		CommitSHA:    "abc123",
		ToolsDir:     failingBurstHelmfile(t),
		WorkspaceDir: workspace,
		TokenFile:    tokenFile,
	}
}

func orderHeader() []string {
	return []string{
		"@@ turnip: " + orderProjectDir + ", helmfile " + provisioningShapes["run-in-image"].version + " @@",
		"@@ turnip: helmfile --no-color diff @@",
	}
}

func orderManifestLines() []string {
	lines := make([]string, orderStdoutLines)
	for i := range lines {
		lines[i] = fmt.Sprintf("manifest line %d", i+1)
	}
	return lines
}

func orderErrorLines() []string {
	lines := make([]string, orderStderrLines)
	for i := range lines {
		lines[i] = fmt.Sprintf("error %d", i+1)
	}
	return lines
}

// slowRecordingServer keeps every message of every stream, and reads
// slowly: it waits before it reads anything after a stream's start, and
// again every thousand messages, so the Runner's sender is held up for the
// whole run.
type slowRecordingServer struct {
	pb.UnimplementedOperationServiceServer

	mu      sync.Mutex
	streams [][]*pb.ExecuteOperationRequest
}

func (s *slowRecordingServer) ExecuteOperation(stream pb.OperationService_ExecuteOperationServer) error {
	s.mu.Lock()
	idx := len(s.streams)
	s.streams = append(s.streams, nil)
	s.mu.Unlock()

	for n := 0; ; n++ {
		req, err := stream.Recv()
		if err == io.EOF {
			return stream.SendAndClose(&pb.ExecuteOperationResponse{})
		}
		if err != nil {
			return err
		}
		s.mu.Lock()
		s.streams[idx] = append(s.streams[idx], req)
		s.mu.Unlock()

		switch {
		case req.GetStart() != nil:
			time.Sleep(500 * time.Millisecond)
		case n%1000 == 0:
			time.Sleep(20 * time.Millisecond)
		}
	}
}

// 20,000 lines of standard output in one burst, then 5 errors, through a
// Server that reads slowly. The tool is not held up, its output is recorded
// in the order it was written, the Runner's own log has all of it, every
// line reaches the Server in that order before the result, and the result's
// output is the errors alone.
func TestCheckpoint_OutputOrderSurvivesASlowServer(t *testing.T) {
	srv := &slowRecordingServer{}
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	server := grpc.NewServer()
	pb.RegisterOperationServiceServer(server, srv)
	go func() { _ = server.Serve(lis) }()
	t.Cleanup(server.Stop)

	tokenFile := filepath.Join(t.TempDir(), "token")
	require.NoError(t, os.WriteFile(tokenFile, []byte("any-token\n"), 0o600))
	cfg := orderRunnerConfig(t, lis.Addr().String(), tokenFile)

	var exitCode int
	stdout, stderr := runQuietly(t, func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		exitCode = runner.Run(ctx, cfg)
	})
	require.Equal(t, 0, exitCode, "the Runner delivered its result; its log:\n%s", tail(stderr))

	srv.mu.Lock()
	defer srv.mu.Unlock()
	require.Len(t, srv.streams, 1, "one stream: nothing broke, nothing was retried")
	sent := srv.streams[0]

	// What the Server received: the start, every line of the record in
	// the record's order, then the result.
	header, manifests, errorLines := orderHeader(), orderManifestLines(), orderErrorLines()
	wantLines := len(header) + len(manifests) + len(errorLines) + 1
	require.Len(t, sent, 1+wantLines+1, "the start, each line of the record once, the result")
	require.NotNil(t, sent[0].GetStart())

	logs := sent[1 : 1+wantLines]
	at := 0
	expect := func(what string, level string, want []string) {
		for i, w := range want {
			log := logs[at].GetLog()
			require.NotNil(t, log, "message %d is a log line", 1+at)
			require.Equal(t, w, log.GetMessage(), "%s line %d, at position %d of the record", what, i, at)
			require.Equal(t, level, log.GetLevel())
			at++
		}
	}
	expect("turnip's leading", "info", header)
	expect("standard output", "info", manifests)
	expect("standard error", "error", errorLines)
	trailer := logs[at].GetLog()
	require.NotNil(t, trailer)
	assert.Regexp(t, `^@@ turnip: exit 1 in \S+ @@$`, trailer.GetMessage(), "the trailer closes the record, after the last error")

	// The result: Helmfile's errors alone, between turnip's lines, with
	// the line saying how much standard output was left out.
	result := sent[len(sent)-1].GetResult()
	require.NotNil(t, result, "the result is the last message, after every log line")
	assert.False(t, result.GetSuccess())
	assert.Equal(t, int32(1), result.GetExitCode())
	wantOutput := append([]string{}, header...)
	wantOutput = append(wantOutput, fmt.Sprintf("@@ turnip: %d lines of standard output omitted because the operation failed @@", orderStdoutLines))
	wantOutput = append(wantOutput, errorLines...)
	gotOutput := strings.Split(result.GetOutput(), "\n")
	require.Len(t, gotOutput, len(wantOutput)+1)
	assert.Equal(t, wantOutput, gotOutput[:len(wantOutput)])
	assert.Equal(t, trailer.GetMessage(), gotOutput[len(gotOutput)-1])

	// The Runner's own log has the whole output, the diffs the result
	// leaves out included: standard output and turnip's lines on its
	// stdout, the errors on its stderr ahead of the summary.
	wantStdout := append([]string{}, header...)
	wantStdout = append(wantStdout, manifests...)
	// The mirror keeps the workspace path the Server never sees.
	gotStdout := strings.Split(strings.TrimSuffix(string(stdout), "\n"), "\n")
	require.Len(t, gotStdout, len(wantStdout)+1)
	assert.True(t, strings.HasSuffix(gotStdout[0], header[0][len("@@ turnip: "):]), "the provenance line, with its absolute path: %q", gotStdout[0])
	assert.Equal(t, wantStdout[1:], gotStdout[1:len(wantStdout)])
	assert.Equal(t, trailer.GetMessage(), gotStdout[len(gotStdout)-1])

	log := string(stderr)
	lastError := strings.Index(log, errorLines[len(errorLines)-1]+"\n")
	require.GreaterOrEqual(t, lastError, 0)
	assert.Equal(t, 0, strings.Index(log, strings.Join(errorLines, "\n")+"\n"), "the tool's errors open the Runner's stderr, in order")
	assert.Greater(t, strings.Index(log, "runner: operation result: success=false exit_code=1 category=tool_exited"), lastError,
		"the summary follows the whole output")
}

// countingSlowHandler takes its time over every log line it is handed, and
// counts them.
type countingSlowHandler struct {
	checkpointHandler
	logs atomic.Int32
}

func (h *countingSlowHandler) HandleLog(context.Context, string, rpc.LogLine) error {
	h.logs.Add(1)
	time.Sleep(300 * time.Millisecond)
	return nil
}

// The same run against the real Server, whose handler is slow: it is asked
// about one log line, not twenty thousand, and the result arrives once, on
// the first attempt, with the errors alone.
func TestCheckpoint_ServerHearsOneLogLineOfAVerboseRun(t *testing.T) {
	shape := provisioningShapes["run-in-image"]
	job, pod := buildOperationPod(t, shape.tool, shape.version, shape.spec)
	auth := &countingAuthenticator{inner: runnerauth.New(newCluster(t, job, pod), checkpointNamespace)}
	handler := &countingSlowHandler{}

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	server := rpc.NewServer(handler, rpc.WithAuthenticator(auth))
	go func() { _ = server.Serve(lis) }()
	t.Cleanup(server.Stop)

	identity := identityOf(t, pod, runnerContainerName)
	require.Equal(t, jobs.ReportAudience, identity.audience)
	tokenFile := filepath.Join(t.TempDir(), "token")
	require.NoError(t, os.WriteFile(tokenFile, []byte(mint(string(pod.UID), identity.audience)+"\n"), 0o600))
	cfg := orderRunnerConfig(t, lis.Addr().String(), tokenFile)

	var exitCode int
	_, stderr := runQuietly(t, func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		exitCode = runner.Run(ctx, cfg)
	})
	require.Equal(t, 0, exitCode, "the Runner delivered its result; its log:\n%s", tail(stderr))

	assert.Equal(t, int32(1), auth.calls.Load(), "the Runner opened one stream")
	assert.Equal(t, int32(1), handler.logs.Load(), "the handler heard of the stream's first log line and no other")

	_, resultFor, results := handler.snapshot()
	require.Len(t, results, 1, "the handler received the result exactly once")
	assert.Equal(t, []string{checkpointOperationID}, resultFor)
	got := results[0]
	assert.False(t, got.Success)
	assert.Equal(t, int32(1), got.ExitCode)
	assert.Equal(t, rpc.FailureToolExited, got.FailureCategory)
	assert.Contains(t, got.Output, fmt.Sprintf("@@ turnip: %d lines of standard output omitted because the operation failed @@", orderStdoutLines))
	assert.Contains(t, got.Output, strings.Join(orderErrorLines(), "\n"))
	assert.NotContains(t, got.Output, "manifest line")
}

// tail is the end of a Runner's log, for a failure message that would
// otherwise print megabytes.
func tail(log []byte) string {
	const keep = 2000
	if len(log) > keep {
		return "…" + string(log[len(log)-keep:])
	}
	return string(log)
}
