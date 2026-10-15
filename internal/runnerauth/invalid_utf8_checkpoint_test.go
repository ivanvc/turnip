package runnerauth_test

// The checkpoint for fix-invalid-utf8-output (tasks.md 2.1).
//
// The fault only exists where a real message is marshaled: Go's protobuf
// refuses to encode a string field holding invalid UTF-8, so a unit test on
// the cleaning function can pass while some string bound for the Server
// still skips it. These tests run the real Runner (runner.Run, the real
// Helmfile plugin running a stand-in helmfile that prints a byte that is not
// valid UTF-8, and runner.NewReporter) over a real TCP connection.
//
// rpc.NewServer hands its handler only a stream's first log line, so the
// first test uses a Server of its own that keeps every message; the second
// uses rpc.NewServer, for the result as the real Server delivers it.

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	pb "github.com/ivanvc/turnip/internal/grpc/turnip/v1"
	"github.com/ivanvc/turnip/internal/rpc"
	"github.com/ivanvc/turnip/internal/runner"
)

const (
	// invalidLineAsPrinted is what the stand-in prints (and invalidLineAsSent what the Server gets): "caf" and a lone 0xE9, the
	// Latin-1 "é", which is not valid UTF-8.
	invalidLineAsPrinted = "caf\xe9"
	invalidLineAsSent    = "caf�"
)

// invalidUTF8Helmfile writes a stand-in helmfile that prints a line holding
// an invalid byte between valid lines, and exits 0.
func invalidUTF8Helmfile(t *testing.T) (toolsDir string) {
	t.Helper()
	toolsDir = t.TempDir()
	script := "#!/bin/sh\necho 'before'\nprintf 'caf\\351\\n'\necho 'after one'\necho 'after two'\nexit 0\n"
	require.NoError(t, os.WriteFile(filepath.Join(toolsDir, "helmfile"), []byte(script), 0o700)) //nolint:gosec // the stand-in must be executable
	return toolsDir
}

func invalidUTF8Config(t *testing.T, addr, tokenFile string) runner.Config {
	t.Helper()
	cfg := orderRunnerConfig(t, addr, tokenFile)
	cfg.ToolsDir = invalidUTF8Helmfile(t)
	return cfg
}

// A line with an invalid byte, more lines after it, exit 0, through a Server
// that keeps every message: one stream, every line arrives in order with the
// byte replaced, the result follows, and the Runner's own log has the byte as
// printed.
func TestCheckpoint_InvalidUTF8LogLinesArriveAndTheRunnersLogKeepsThem(t *testing.T) {
	srv := &slowRecordingServer{}
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	server := grpc.NewServer()
	pb.RegisterOperationServiceServer(server, srv)
	go func() { _ = server.Serve(lis) }()
	t.Cleanup(server.Stop)

	tokenFile := filepath.Join(t.TempDir(), "token")
	require.NoError(t, os.WriteFile(tokenFile, []byte("any-token\n"), 0o600))
	cfg := invalidUTF8Config(t, lis.Addr().String(), tokenFile)

	var exitCode int
	stdout, stderr := runQuietly(t, func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		exitCode = runner.Run(ctx, cfg)
	})
	require.Equal(t, 0, exitCode, "the Runner delivered its result; its log:\n%s", tail(stderr))

	srv.mu.Lock()
	defer srv.mu.Unlock()
	require.Len(t, srv.streams, 1, "one stream: nothing failed to encode, nothing was retried")
	sent := srv.streams[0]

	header := orderHeader()
	wantLines := append(append([]string{}, header...), "before", invalidLineAsSent, "after one", "after two")
	require.Len(t, sent, 1+len(wantLines)+1+1, "the start, each line of the record once, the trailer, the result")
	require.NotNil(t, sent[0].GetStart())
	for i, w := range wantLines {
		log := sent[1+i].GetLog()
		require.NotNil(t, log, "message %d is a log line", 1+i)
		assert.Equal(t, w, log.GetMessage(), "line %d", i)
	}
	trailer := sent[1+len(wantLines)].GetLog()
	require.NotNil(t, trailer)
	assert.Regexp(t, `^@@ turnip: exit 0 in \S+ @@$`, trailer.GetMessage())

	result := sent[len(sent)-1].GetResult()
	require.NotNil(t, result, "the result is the last message")
	assert.True(t, result.GetSuccess())
	assert.Equal(t, int32(0), result.GetExitCode())
	assert.Contains(t, result.GetOutput(), invalidLineAsSent)
	assert.NotContains(t, result.GetOutput(), invalidLineAsPrinted)

	// The Runner's own log keeps the bytes as the tool printed them.
	assert.Contains(t, string(stdout), invalidLineAsPrinted+"\n")
	assert.NotContains(t, string(stdout), invalidLineAsSent)
}

// The same run against the real Server: it hears of the stream once, and the
// result arrives once, on the first attempt, with the byte replaced.
func TestCheckpoint_InvalidUTF8ResultArrivesOnTheFirstAttempt(t *testing.T) {
	addr, tokenFile, handler, auth := largeResultServer(t)
	cfg := invalidUTF8Config(t, addr, tokenFile)

	var exitCode int
	_, stderr := runQuietly(t, func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		exitCode = runner.Run(ctx, cfg)
	})
	require.Equal(t, 0, exitCode, "the Runner delivered its result; its log:\n%s", tail(stderr))

	assert.Equal(t, int32(1), auth.calls.Load(), "the Runner opened one stream: no attempt was retried")
	_, resultFor, results := handler.snapshot()
	require.Len(t, results, 1, "the handler received the result exactly once")
	assert.Equal(t, []string{checkpointOperationID}, resultFor)
	got := results[0]
	assert.True(t, got.Success)
	assert.Equal(t, int32(0), got.ExitCode)
	assert.Equal(t, rpc.FailureUnspecified, got.FailureCategory)
	assert.Contains(t, got.Output, "before\n"+invalidLineAsSent+"\nafter one\nafter two\n")
	assert.True(t, utf8.ValidString(got.Output), "no invalid byte reaches the Server")
}
