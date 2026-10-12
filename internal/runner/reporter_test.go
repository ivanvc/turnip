package runner

import (
	"context"
	"fmt"
	"io"
	"net"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	pb "github.com/ivanvc/turnip/internal/grpc/turnip/v1"
	"github.com/ivanvc/turnip/internal/plugin"
	"github.com/ivanvc/turnip/internal/rpc"
)

// scriptedServer fails the first failCount stream opens — returning an
// error before ever reading anything, so the client's Start send fails
// fast — then accepts every subsequent stream fully, recording each
// stream's messages for inspection.
type scriptedServer struct {
	pb.UnimplementedOperationServiceServer

	mu        sync.Mutex
	attempts  int
	failCount int
	streams   [][]*pb.ExecuteOperationRequest
}

func (s *scriptedServer) ExecuteOperation(stream pb.OperationService_ExecuteOperationServer) error {
	s.mu.Lock()
	attempt := s.attempts
	s.attempts++
	fail := attempt < s.failCount
	s.mu.Unlock()

	if fail {
		return status.Error(codes.Unavailable, "simulated failure")
	}

	// Register this stream's slot immediately (not only once it finishes)
	// and update it live, so a caller inspecting an abandoned stream
	// (never sent EOF — e.g. one this test drops mid-flight to simulate a
	// connection loss) still sees what it received.
	s.mu.Lock()
	idx := len(s.streams)
	s.streams = append(s.streams, nil)
	s.mu.Unlock()

	for {
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
	}
}

func (s *scriptedServer) streamCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.streams)
}

func (s *scriptedServer) streamAt(i int) []*pb.ExecuteOperationRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	if i >= len(s.streams) {
		return nil
	}
	return s.streams[i]
}

// waitForStreamCount blocks until srv has registered at least n streams.
//
// A stream's slot is assigned by its own handler goroutine (see
// ExecuteOperation above), which is not ordered against the client call
// that opened it returning. A test that opens a second stream without
// waiting therefore races the two handlers for index 0, and streamAt's
// indices stop meaning "first" and "second" — the failure looks like
// dropped log lines rather than a reordering, which is what made this
// worth a helper instead of a sleep.
func waitForStreamCount(t *testing.T, srv *scriptedServer, n int) {
	t.Helper()
	require.Eventually(t, func() bool { return srv.streamCount() >= n }, 5*time.Second, time.Millisecond,
		"server never registered %d stream(s)", n)
}

func dialScriptedServer(t *testing.T, srv *scriptedServer) pb.OperationServiceClient {
	t.Helper()

	lis := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	pb.RegisterOperationServiceServer(server, srv)
	go func() { _ = server.Serve(lis) }()
	t.Cleanup(server.Stop)

	conn, err := grpc.NewClient("passthrough:///bufconn",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	return pb.NewOperationServiceClient(conn)
}

// fakeClock provides deterministic now/sleep seams: Sleep advances the
// fake clock instantly rather than actually waiting, so a multi-minute
// backoff budget elapses in real time as fast as the retry loop can spin.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock() *fakeClock { return &fakeClock{now: time.Unix(0, 0)} }

// Polling bounds for the Eventually assertions that wait on a gRPC
// handler goroutine to register what it received.
const (
	waitTimeout = 5 * time.Second
	waitTick    = time.Millisecond
)

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Sleep(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// stubToken stands in for reading the projected ServiceAccount token
// from disk. Every reporter test needs one, because openStream now
// presents a credential before it sends anything; the tests that care
// about the reading itself substitute their own counting version.
func stubToken() (string, error) { return "test-token", nil }

func testCfg() Config {
	return Config{
		OperationID: "op-1",
		ProjectName: "web",
		ProjectDir:  "infra/web",
		Tool:        "helmfile",
		Operation:   "diff",
		RepoURL:     "https://github.com/acme/repo.git",
		CommitSHA:   "abc123",
	}
}

// fakeStream implements pb.OperationService_ExecuteOperationClient by
// delegating just the two methods reporter.go actually calls; embedding a
// nil grpc.ClientStream is safe as long as nothing else is invoked.
type fakeStream struct {
	grpc.ClientStream
	sendFunc         func(*pb.ExecuteOperationRequest) error
	closeAndRecvFunc func() (*pb.ExecuteOperationResponse, error)
}

func (f *fakeStream) Send(req *pb.ExecuteOperationRequest) error { return f.sendFunc(req) }
func (f *fakeStream) CloseAndRecv() (*pb.ExecuteOperationResponse, error) {
	return f.closeAndRecvFunc()
}

// fakeOperationClient lets a test script exactly what each successive
// ExecuteOperation call returns — used for reporter retry-orchestration
// tests where the failure needs to be observed synchronously (a bare
// Send(), unlike CloseAndRecv(), does not reliably surface an async
// server-side rejection — see server_test.go / reporter_test.go's bufconn
// tests for real wire-level coverage of that).
type fakeOperationClient struct {
	pb.OperationServiceClient

	mu        sync.Mutex
	attempts  int
	executeFn func(attempt int) (pb.OperationService_ExecuteOperationClient, error)
}

func (c *fakeOperationClient) ExecuteOperation(ctx context.Context, opts ...grpc.CallOption) (pb.OperationService_ExecuteOperationClient, error) {
	c.mu.Lock()
	attempt := c.attempts
	c.attempts++
	c.mu.Unlock()
	return c.executeFn(attempt)
}

func (c *fakeOperationClient) attemptCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.attempts
}

func workingStream() pb.OperationService_ExecuteOperationClient {
	return &fakeStream{
		sendFunc:         func(*pb.ExecuteOperationRequest) error { return nil },
		closeAndRecvFunc: func() (*pb.ExecuteOperationResponse, error) { return &pb.ExecuteOperationResponse{}, nil },
	}
}

func TestReporter_ConnectSucceedsFirstAttemptNoRetries(t *testing.T) {
	client := &fakeOperationClient{
		executeFn: func(attempt int) (pb.OperationService_ExecuteOperationClient, error) {
			return workingStream(), nil
		},
	}
	clock := newFakeClock()
	r := newReporter(client, testCfg(), clock.Now, clock.Sleep, func() float64 { return 0.5 })
	r.readToken = stubToken

	require.NoError(t, r.Connect(context.Background()))
	assert.Equal(t, 1, client.attemptCount())
}

func TestReporter_ConnectRetriesThenSucceeds(t *testing.T) {
	client := &fakeOperationClient{
		executeFn: func(attempt int) (pb.OperationService_ExecuteOperationClient, error) {
			if attempt < 2 {
				return nil, status.Error(codes.Unavailable, "simulated dial failure")
			}
			return workingStream(), nil
		},
	}
	clock := newFakeClock()
	r := newReporter(client, testCfg(), clock.Now, clock.Sleep, func() float64 { return 0 })
	r.readToken = stubToken

	require.NoError(t, r.Connect(context.Background()))
	assert.Equal(t, 3, client.attemptCount())
}

func TestReporter_ConnectGivesUpOnceBudgetElapses(t *testing.T) {
	client := &fakeOperationClient{
		executeFn: func(attempt int) (pb.OperationService_ExecuteOperationClient, error) {
			return nil, status.Error(codes.Unavailable, "simulated dial failure")
		},
	}
	clock := newFakeClock()
	r := newReporter(client, testCfg(), clock.Now, clock.Sleep, func() float64 { return 1 })
	r.readToken = stubToken

	err := r.Connect(context.Background())
	require.Error(t, err)
	assert.Greater(t, client.attemptCount(), 1, "should have retried at least once before giving up")
}

func TestReporter_ReportSucceeds(t *testing.T) {
	srv := &scriptedServer{}
	client := dialScriptedServer(t, srv)
	clock := newFakeClock()
	r := newReporter(client, testCfg(), clock.Now, clock.Sleep, func() float64 { return 0 })
	r.readToken = stubToken

	require.NoError(t, r.Connect(context.Background()))
	require.NoError(t, r.Report(context.Background(), OperationResult{Success: true, Output: "ok", ExitCode: 0}))

	assert.Equal(t, 1, srv.streamCount())
}

func TestReporter_ReportReturnsSentinelWhenBudgetExhausted(t *testing.T) {
	srv := &scriptedServer{failCount: 1 << 30}
	client := dialScriptedServer(t, srv)
	clock := newFakeClock()
	r := newReporter(client, testCfg(), clock.Now, clock.Sleep, func() float64 { return 1 })
	r.readToken = stubToken

	err := r.Report(context.Background(), OperationResult{Success: true})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrReportFailed)
}

// logsOf returns the log lines among reqs, in order.
func logsOf(reqs []*pb.ExecuteOperationRequest) []*pb.LogLine {
	var out []*pb.LogLine
	for _, req := range reqs {
		if l := req.GetLog(); l != nil {
			out = append(out, l)
		}
	}
	return out
}

func messagesOf(logs []*pb.LogLine) []string {
	out := make([]string, len(logs))
	for i, l := range logs {
		out[i] = l.GetMessage()
	}
	return out
}

func noClean(s string) string { return s }

// On a stream that never broke every line of the record is sent once, in
// order, with the level its stream gives it and the time it was recorded,
// and the result follows the last of them. Nothing is replayed.
func TestReporter_AnUnbrokenStreamCarriesEachLineOnceThenTheResult(t *testing.T) {
	srv := &scriptedServer{}
	client := dialScriptedServer(t, srv)
	clock := newFakeClock()
	r := newReporter(client, testCfg(), clock.Now, clock.Sleep, func() float64 { return 0 })
	r.readToken = stubToken

	require.NoError(t, r.Connect(context.Background()))
	rec := plugin.NewRecord()
	r.Follow(rec, func(s string) string { return strings.ReplaceAll(s, "/turnip/src/", "") })
	rec.Append(plugin.Turnip, "@@ turnip: helmfile --no-color diff @@")
	rec.Append(plugin.Stdout, "reading /turnip/src/infra/web/values.yaml")
	rec.Append(plugin.Stderr, "a warning")
	rec.Close()

	require.NoError(t, r.Report(context.Background(), OperationResult{Success: true, Output: "done"}))
	require.Equal(t, 1, srv.streamCount(), "no second stream: nothing broke")

	sent := srv.streamAt(0)
	require.Len(t, sent, 5, "the start, three log lines, the result")
	require.NotNil(t, sent[0].GetStart())
	logs := logsOf(sent)
	assert.Equal(t, []string{
		"@@ turnip: helmfile --no-color diff @@",
		"reading infra/web/values.yaml",
		"a warning",
	}, messagesOf(logs), "each line once, cleaned, in the record's order")
	assert.Equal(t, []string{"info", "info", "error"}, []string{logs[0].GetLevel(), logs[1].GetLevel(), logs[2].GetLevel()})

	recorded, _ := rec.Next(context.Background(), 0)
	for i, l := range logs {
		assert.Equal(t, recorded[i].At.Format(time.RFC3339Nano), l.GetTimestamp(), "stamped when the tool wrote it")
	}
	require.NotNil(t, sent[4].GetResult(), "the result is the last message")
	assert.Equal(t, "done", sent[4].GetResult().GetOutput())
}

// More output than a replay would carry, on a stream that never broke:
// every line arrives and nothing is reported as dropped.
func TestReporter_AVerboseUnbrokenStreamDropsNothing(t *testing.T) {
	srv := &scriptedServer{}
	client := dialScriptedServer(t, srv)
	clock := newFakeClock()
	r := newReporter(client, testCfg(), clock.Now, clock.Sleep, func() float64 { return 0 })
	r.readToken = stubToken

	require.NoError(t, r.Connect(context.Background()))
	rec := plugin.NewRecord()
	r.Follow(rec, noClean)
	const lines = 6000 // about 600 KiB, more than twice replayMaxBytes
	for i := range lines {
		rec.Append(plugin.Stdout, fmt.Sprintf("%05d %s", i, strings.Repeat("x", 94)))
	}
	rec.Close()

	require.NoError(t, r.Report(context.Background(), OperationResult{Success: true}))
	require.Equal(t, 1, srv.streamCount())

	logs := logsOf(srv.streamAt(0))
	require.Len(t, logs, lines)
	for i, l := range logs {
		require.True(t, strings.HasPrefix(l.GetMessage(), fmt.Sprintf("%05d ", i)), "line %d in its place", i)
		require.NotEqual(t, "warn", l.GetLevel(), "no lines dropped marker")
	}
}

// After the stream breaks mid-run, its replacement is marked resumed and
// replays the record from the start (it fits), lines written after the
// break included, then carries the result.
func TestReporter_ABrokenStreamIsReplacedAndReplayed(t *testing.T) {
	srv := &scriptedServer{}
	client := dialScriptedServer(t, srv)
	clock := newFakeClock()
	r := newReporter(client, testCfg(), clock.Now, clock.Sleep, func() float64 { return 0 })
	r.readToken = stubToken

	require.NoError(t, r.Connect(context.Background()))
	// Pin the first stream to index 0 before anything opens a second one;
	// without this the reconnect's handler can register first and the
	// assertions below inspect the wrong stream.
	waitForStreamCount(t, srv, 1)

	rec := plugin.NewRecord()
	r.Follow(rec, noClean)
	rec.Append(plugin.Stdout, "line one")
	rec.Append(plugin.Stderr, "line two")
	require.Eventually(t, func() bool { return len(logsOf(srv.streamAt(0))) == 2 }, 5*time.Second, time.Millisecond)

	// Simulate the connection dropping mid-Operation: the stream is no
	// longer usable, but the record keeps everything.
	r.mu.Lock()
	r.stream = nil
	r.mu.Unlock()

	rec.Append(plugin.Stdout, "line three") // recorded; no stream to send on
	rec.Close()

	require.NoError(t, r.Report(context.Background(), OperationResult{Success: true, Output: "done"}))
	require.Equal(t, 2, srv.streamCount(), "expected the initial stream plus one reconnect")

	first := srv.streamAt(0)
	require.NotEmpty(t, first)
	require.NotNil(t, first[0].GetStart())
	assert.False(t, first[0].GetStart().GetResumed())

	second := srv.streamAt(1)
	require.NotEmpty(t, second)
	require.NotNil(t, second[0].GetStart())
	assert.True(t, second[0].GetStart().GetResumed(), "every stream after the first must resume, not start over")

	assert.Equal(t, []string{"line one", "line two", "line three"}, messagesOf(logsOf(second)),
		"the record is replayed, not only the lines written after the drop")

	result := second[len(second)-1].GetResult()
	require.NotNil(t, result)
	assert.True(t, result.Success)
	assert.Equal(t, "done", result.Output)
}

// A replay carries the most recent 256 KiB of the record, and says how
// many lines came before them.
func TestReporter_AReplayCarriesTheRecordsTailAndCountsTheRest(t *testing.T) {
	recording := &recordingStream{}
	client := &fakeOperationClient{executeFn: func(int) (pb.OperationService_ExecuteOperationClient, error) {
		return recording.stream(), nil
	}}
	clock := newFakeClock()
	r := newReporter(client, testCfg(), clock.Now, clock.Sleep, func() float64 { return 0 })
	r.readToken = stubToken

	// No stream when the sender starts, as after a break before the
	// first line: it stops at once, and everything is left to the replay.
	rec := plugin.NewRecord()
	r.Follow(rec, noClean)
	const lines = 6000
	for i := range lines {
		rec.Append(plugin.Stdout, fmt.Sprintf("%05d %s", i, strings.Repeat("x", 94)))
	}
	rec.Close()

	require.NoError(t, r.Report(context.Background(), OperationResult{Success: true}))

	recording.mu.Lock()
	logs := logsOf(recording.sent)
	recording.mu.Unlock()
	require.NotEmpty(t, logs)

	replayed := len(logs) - 1
	assert.Equal(t, "warn", logs[0].GetLevel())
	assert.Equal(t, fmt.Sprintf("%d lines dropped", lines-replayed), logs[0].GetMessage())

	size := 0
	for _, l := range logs[1:] {
		size += logLineSize(l)
	}
	assert.LessOrEqual(t, size, replayMaxBytes)
	assert.Greater(t, size+logLineSize(logs[1]), replayMaxBytes, "as many lines as fit, no fewer")
	assert.True(t, strings.HasPrefix(logs[1].GetMessage(), fmt.Sprintf("%05d ", lines-replayed)), "the tail starts where the dropped lines end")
	assert.True(t, strings.HasPrefix(logs[len(logs)-1].GetMessage(), fmt.Sprintf("%05d ", lines-1)), "and ends with the record's last line")
	require.Len(t, recording.results(), 1)
}

// A replacement stream that fails in turn is replaced again, and the next
// one replays again: the Server may have seen none of the earlier attempt.
func TestReporter_EveryReplacementStreamReplays(t *testing.T) {
	failing := &fakeStream{
		sendFunc: func(req *pb.ExecuteOperationRequest) error {
			if req.GetResult() != nil {
				return status.Error(codes.Unavailable, "gone")
			}
			return nil
		},
		closeAndRecvFunc: func() (*pb.ExecuteOperationResponse, error) { return nil, status.Error(codes.Unavailable, "gone") },
	}
	recording := &recordingStream{}
	client := &fakeOperationClient{executeFn: func(attempt int) (pb.OperationService_ExecuteOperationClient, error) {
		if attempt == 0 {
			return failing, nil
		}
		return recording.stream(), nil
	}}
	clock := newFakeClock()
	r := newReporter(client, testCfg(), clock.Now, clock.Sleep, func() float64 { return 0 })
	r.readToken = stubToken

	rec := plugin.NewRecord()
	r.Follow(rec, noClean) // no stream yet: the sender stops, the replay carries everything
	rec.Append(plugin.Stdout, "one")
	rec.Append(plugin.Stdout, "two")
	rec.Close()

	require.NoError(t, r.Report(context.Background(), OperationResult{Success: true}))
	assert.Equal(t, 2, client.attemptCount())

	recording.mu.Lock()
	defer recording.mu.Unlock()
	assert.Equal(t, []string{"one", "two"}, messagesOf(logsOf(recording.sent)))
}

// A Server that stops reading holds up only its own copy of the output.
// The record still takes every line and closes, as it would with the tool
// writing to it; once the stream moves again every line is sent, once.
func TestReporter_ABlockedStreamHoldsUpNothingButItself(t *testing.T) {
	release := make(chan struct{})
	entered := make(chan struct{}, 1)
	var mu sync.Mutex
	var sent []*pb.ExecuteOperationRequest
	blocking := &fakeStream{
		sendFunc: func(req *pb.ExecuteOperationRequest) error {
			if req.GetLog() != nil {
				select {
				case entered <- struct{}{}:
				default:
				}
				<-release
			}
			mu.Lock()
			defer mu.Unlock()
			sent = append(sent, req)
			return nil
		},
		closeAndRecvFunc: func() (*pb.ExecuteOperationResponse, error) { return &pb.ExecuteOperationResponse{}, nil },
	}
	client := &fakeOperationClient{executeFn: func(int) (pb.OperationService_ExecuteOperationClient, error) { return blocking, nil }}
	clock := newFakeClock()
	r := newReporter(client, testCfg(), clock.Now, clock.Sleep, func() float64 { return 0 })
	r.readToken = stubToken

	require.NoError(t, r.Connect(context.Background()))
	rec := plugin.NewRecord()
	r.Follow(rec, noClean)

	const lines = 500
	rec.Append(plugin.Stdout, "line 0")
	<-entered // the sender is now stuck on the first line
	for i := 1; i < lines; i++ {
		rec.Append(plugin.Stdout, fmt.Sprintf("line %d", i))
	}
	rec.Close()
	require.Equal(t, lines, rec.Len(), "the record took every line while the stream was blocked")

	reported := make(chan error, 1)
	go func() { reported <- r.Report(context.Background(), OperationResult{Success: true}) }()
	select {
	case <-reported:
		require.Fail(t, "the result was sent before the log lines")
	case <-time.After(20 * time.Millisecond):
	}

	close(release)
	require.NoError(t, <-reported)

	mu.Lock()
	defer mu.Unlock()
	logs := logsOf(sent)
	require.Len(t, logs, lines, "every line, once")
	for i, l := range logs {
		require.Equal(t, fmt.Sprintf("line %d", i), l.GetMessage())
	}
	require.NotNil(t, sent[len(sent)-1].GetResult(), "and the result after the last of them")
	assert.Equal(t, 1, client.attemptCount(), "on the one stream")
}

// Report gives up waiting for the log lines when its context ends.
func TestReporter_ReportStopsWaitingWhenItsContextEnds(t *testing.T) {
	client := &fakeOperationClient{executeFn: func(int) (pb.OperationService_ExecuteOperationClient, error) { return workingStream(), nil }}
	clock := newFakeClock()
	r := newReporter(client, testCfg(), clock.Now, clock.Sleep, func() float64 { return 0 })
	r.readToken = stubToken

	require.NoError(t, r.Connect(context.Background()))
	rec := plugin.NewRecord() // still open: the sender has not finished
	defer rec.Close()
	r.Follow(rec, noClean)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := r.Report(ctx, OperationResult{Success: true})
	require.ErrorIs(t, err, ErrReportFailed)
	require.ErrorIs(t, err, context.Canceled)
}

// Feature: fix-output-order, Property 3: An unbroken stream carries the record once, in order
func TestProperty_AnUnbrokenStreamCarriesTheRecordOnceInOrder(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		recording := &recordingStream{}
		client := &fakeOperationClient{executeFn: func(int) (pb.OperationService_ExecuteOperationClient, error) {
			return recording.stream(), nil
		}}
		clock := newFakeClock()
		r := newReporter(client, testCfg(), clock.Now, clock.Sleep, func() float64 { return 0 })
		r.readToken = stubToken
		require.NoError(t, r.Connect(context.Background()))

		type want struct{ level, message string }
		var wants []want
		rec := plugin.NewRecord()
		appendLine := func() {
			stream := rapid.SampledFrom([]plugin.Stream{plugin.Stdout, plugin.Stderr, plugin.Turnip}).Draw(t, "stream")
			text := rapid.StringMatching(`[a-z @:]{0,20}`).Draw(t, "text")
			level := "info"
			if stream == plugin.Stderr {
				level = "error"
			}
			wants = append(wants, want{level, text})
			rec.Append(stream, text)
		}

		// Lines recorded before the sender starts, while it runs (it may
		// be ahead, level or behind at each append), and none at all.
		for range rapid.IntRange(0, 20).Draw(t, "before") {
			appendLine()
		}
		r.Follow(rec, noClean)
		for range rapid.IntRange(0, 40).Draw(t, "during") {
			if rapid.Bool().Draw(t, "yield") {
				runtime.Gosched()
			}
			appendLine()
		}
		rec.Close()

		require.NoError(t, r.Report(context.Background(), OperationResult{Success: true}))

		recording.mu.Lock()
		defer recording.mu.Unlock()
		require.Len(t, recording.sent, 1+len(wants)+1, "the start, each line once, the result")
		require.NotNil(t, recording.sent[0].GetStart())
		for i, w := range wants {
			log := recording.sent[1+i].GetLog()
			require.NotNil(t, log, "message %d is a log line", 1+i)
			require.Equal(t, w.message, log.GetMessage())
			require.Equal(t, w.level, log.GetLevel())
		}
		require.NotNil(t, recording.sent[len(recording.sent)-1].GetResult(), "the result is last")
	})
}

// The failure category the Runner sets reaches the wire, so the Server
// can title the check run by it (check-run-titles Requirement 5.1).
func TestReporter_ReportSendsTheFailureCategory(t *testing.T) {
	srv := &scriptedServer{}
	client := dialScriptedServer(t, srv)
	clock := newFakeClock()
	r := newReporter(client, testCfg(), clock.Now, clock.Sleep, func() float64 { return 0 })
	r.readToken = stubToken

	require.NoError(t, r.Connect(context.Background()))
	waitForStreamCount(t, srv, 1)
	require.NoError(t, r.Report(context.Background(), OperationResult{ExitCode: 1, FailureCategory: rpc.FailureToolExited}))

	sent := srv.streamAt(0)
	result := sent[len(sent)-1].GetResult()
	require.NotNil(t, result)
	assert.Equal(t, pb.FailureCategory_FAILURE_CATEGORY_TOOL_EXITED, result.GetFailureCategory())
}

// recordingStream is a working stream that keeps every message sent on
// it, so a test can count what reached the wire.
type recordingStream struct {
	mu   sync.Mutex
	sent []*pb.ExecuteOperationRequest
}

func (s *recordingStream) stream() pb.OperationService_ExecuteOperationClient {
	return &fakeStream{
		sendFunc: func(req *pb.ExecuteOperationRequest) error {
			s.mu.Lock()
			defer s.mu.Unlock()
			s.sent = append(s.sent, req)
			return nil
		},
		closeAndRecvFunc: func() (*pb.ExecuteOperationResponse, error) { return &pb.ExecuteOperationResponse{}, nil },
	}
}

func (s *recordingStream) results() []*pb.OperationResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*pb.OperationResult
	for _, req := range s.sent {
		if r := req.GetResult(); r != nil {
			out = append(out, r)
		}
	}
	return out
}

// A result whose message would not fit the Server's limit is never sent;
// the failed "too large to deliver" result goes instead, once.
func TestReporter_ResultTooLargeIsNeverSent(t *testing.T) {
	rec := &recordingStream{}
	client := &fakeOperationClient{
		executeFn: func(int) (pb.OperationService_ExecuteOperationClient, error) { return rec.stream(), nil },
	}
	clock := newFakeClock()
	r := newReporter(client, testCfg(), clock.Now, clock.Sleep, func() float64 { return 0 })
	r.readToken = stubToken

	require.NoError(t, r.Connect(context.Background()))
	require.NoError(t, r.Report(context.Background(), OperationResult{
		Success:         true,
		Output:          "planned",
		ExitCode:        0,
		Changes:         plugin.ChangeSummary{Add: 2},
		PlanData:        make([]byte, 5<<20),
		FailureCategory: rpc.FailureUnspecified,
	}))

	results := rec.results()
	require.Len(t, results, 1, "only the replacement is sent, once")
	got := results[0]
	assert.False(t, got.GetSuccess())
	assert.Empty(t, got.GetOutput())
	assert.Empty(t, got.GetPlanData())
	assert.Equal(t, pb.FailureCategory_FAILURE_CATEGORY_UNSPECIFIED, got.GetFailureCategory())
	assert.Equal(t, "the result was too large to deliver (5.0 MiB; the Server accepts at most 4 MiB)", got.GetErrorMessage())
	assert.Equal(t, 1, client.attemptCount(), "no stream is opened beyond the first")
}

// gRPC accepts a message exactly at its limit, so one that size is sent
// as is, and one a byte larger is replaced.
func TestDeliverable_TheLimitIsInclusive(t *testing.T) {
	result := OperationResult{Success: true}
	for n := rpc.MaxResultMessageBytes - 16; ; n++ {
		result.Output = strings.Repeat("x", n)
		if proto.Size(resultRequest(result)) == rpc.MaxResultMessageBytes {
			break
		}
		require.Less(t, proto.Size(resultRequest(result)), rpc.MaxResultMessageBytes)
	}

	got, replaced := deliverable(result)
	assert.False(t, replaced)
	assert.Equal(t, result, got)

	result.Output += "x"
	got, replaced = deliverable(result)
	assert.True(t, replaced)
	assert.Empty(t, got.Output)
}

// dialLimitedServer is dialScriptedServer with a Server that accepts
// messages of at most maxRecv bytes, standing in for a Server configured
// differently from the Runner's constant.
func dialLimitedServer(t *testing.T, srv *scriptedServer, maxRecv int) pb.OperationServiceClient {
	t.Helper()

	lis := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer(grpc.MaxRecvMsgSize(maxRecv))
	pb.RegisterOperationServiceServer(server, srv)
	go func() { _ = server.Serve(lis) }()
	t.Cleanup(server.Stop)

	conn, err := grpc.NewClient("passthrough:///bufconn",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	return pb.NewOperationServiceClient(conn)
}

// A ResourceExhausted answer to a result is final: the original is not
// sent again, and the failed result is delivered in its place.
func TestReporter_ResourceExhaustedSwitchesToTheFailedResult(t *testing.T) {
	srv := &scriptedServer{}
	client := dialLimitedServer(t, srv, 64<<10)
	clock := newFakeClock()
	r := newReporter(client, testCfg(), clock.Now, clock.Sleep, func() float64 { return 0 })
	r.readToken = stubToken

	require.NoError(t, r.Connect(context.Background()))
	waitForStreamCount(t, srv, 1)
	require.NoError(t, r.Report(context.Background(), OperationResult{
		Success: true,
		Output:  strings.Repeat("x", 100<<10),
	}))

	require.Equal(t, 2, srv.streamCount(), "one stream refused the result, one carried the replacement; none retried the original")
	assert.Nil(t, lastResult(srv.streamAt(0)), "the oversized result never reached the handler")

	got := lastResult(srv.streamAt(1))
	require.NotNil(t, got)
	assert.False(t, got.GetSuccess())
	assert.Empty(t, got.GetOutput())
	assert.Empty(t, got.GetPlanData())
	assert.Equal(t, pb.FailureCategory_FAILURE_CATEGORY_UNSPECIFIED, got.GetFailureCategory())
	assert.Regexp(t, `^the result was too large to deliver \(100\.\d KiB; the Server accepts at most 4 MiB\)$`, got.GetErrorMessage())
}

func lastResult(reqs []*pb.ExecuteOperationRequest) *pb.OperationResult {
	for i := len(reqs) - 1; i >= 0; i-- {
		if r := reqs[i].GetResult(); r != nil {
			return r
		}
	}
	return nil
}
