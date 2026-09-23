package runner

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/ivanvc/turnip/internal/grpc/turnip/v1"
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

func TestReporter_ReportReconnectsAndResendsFullBufferWithResumedTrue(t *testing.T) {
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

	r.LogLine("stdout", "line one")
	r.LogLine("stderr", "line two")

	// Simulate the connection dropping mid-Operation: the stream is no
	// longer usable, but nothing buffered is lost.
	r.mu.Lock()
	r.stream = nil
	r.mu.Unlock()

	r.LogLine("stdout", "line three") // buffered only; no stream to send on

	require.NoError(t, r.Report(context.Background(), OperationResult{Success: true, Output: "done"}))
	require.Equal(t, 2, srv.streamCount(), "expected the initial stream plus one reconnect")

	first := srv.streamAt(0)
	require.NotEmpty(t, first)
	firstStart := first[0].GetStart()
	require.NotNil(t, firstStart)
	assert.False(t, firstStart.GetResumed())

	second := srv.streamAt(1)
	require.NotEmpty(t, second)
	secondStart := second[0].GetStart()
	require.NotNil(t, secondStart)
	assert.True(t, secondStart.GetResumed(), "every stream after the first must resume, not start over")

	var gotLogs []string
	for _, req := range second {
		if log := req.GetLog(); log != nil {
			gotLogs = append(gotLogs, log.Message)
		}
	}
	assert.Equal(t, []string{"line one", "line two", "line three"}, gotLogs,
		"the entire buffered log history must be resent, not only lines produced after the drop")

	result := second[len(second)-1].GetResult()
	require.NotNil(t, result)
	assert.True(t, result.Success)
	assert.Equal(t, "done", result.Output)
}

func TestLogRingBuffer_DropsOldestPastBoundWithMarker(t *testing.T) {
	buf := newLogRingBuffer(64)
	for i := range 50 {
		buf.add(&pb.LogLine{Level: "info", Message: fmt.Sprintf("line-%02d", i)})
	}

	snap := buf.snapshot()
	require.NotEmpty(t, snap)
	assert.Equal(t, "warn", snap[0].Level)
	assert.Contains(t, snap[0].Message, "dropped")

	for _, line := range snap[1:] {
		assert.NotContains(t, line.Message, "dropped")
	}
}

func TestLogRingBuffer_NoMarkerWhenUnderBound(t *testing.T) {
	buf := newLogRingBuffer(logBufferMaxBytes)
	buf.add(&pb.LogLine{Level: "info", Message: "hello"})

	snap := buf.snapshot()
	require.Len(t, snap, 1)
	assert.Equal(t, "hello", snap[0].Message)
}
