package runner

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	pb "github.com/ivanvc/turnip/internal/grpc/turnip/v1"
	"github.com/ivanvc/turnip/internal/plugin"
	"github.com/ivanvc/turnip/internal/rpc"
)

const (
	// Backoff parameters per design.md's table.
	connectBaseDelay = time.Second
	connectMaxDelay  = 30 * time.Second
	connectBudget    = 2 * time.Minute

	reportBaseDelay = time.Second
	reportMaxDelay  = 60 * time.Second
	reportBudget    = 15 * time.Minute

	// logBufferMaxBytes bounds the reporter's log ring buffer (Requirement
	// 4.3).
	logBufferMaxBytes = 256 * 1024
)

// ErrReportFailed wraps the error returned by Report when its retry budget
// is exhausted, so a caller can distinguish "we never found out if the
// Operation succeeded" from the Operation's own tool having failed
// (Requirement 4.6), via errors.Is.
var ErrReportFailed = errors.New("runner: exhausted retry budget delivering operation result")

// OperationResult is the Runner's translation of a Plugin's ExecuteResult
// into what Report sends to the Server.
type OperationResult struct {
	Success      bool
	Output       string
	ExitCode     int
	ErrorMessage string
	Changes      plugin.ChangeSummary
	PlanData     []byte
}

// reporter is the retrying gRPC client that streams an Operation's start,
// log lines, and final result to the Server, tolerating connection drops
// without ever touching the Operation's own subprocess (design.md's
// Requirement 2 & 4 section).
type reporter struct {
	client pb.OperationServiceClient
	cfg    Config

	now   func() time.Time
	sleep func(time.Duration)
	rnd   func() float64

	// readToken is re-invoked on every openStream rather than its result
	// being cached, because kubelet rotates the projected token in place:
	// a reconnect minutes into a long Operation must present whatever is
	// in the file now, not whatever was there when the process started.
	// A function rather than a path so a test can count the reads.
	readToken func() (string, error)

	buf *logRingBuffer

	mu       sync.Mutex
	stream   pb.OperationService_ExecuteOperationClient
	everSent bool
}

// NewReporter dials addr and returns a reporter backed by a real gRPC
// connection, plus that connection's Closer for the caller to close on
// exit.
//
// The connection is not encrypted. Encryption is a separate axis from
// authentication and is tracked as its own slice; until it lands, the
// Runner presents its credential over a plaintext pod-to-pod channel.
// The credential is audience-scoped to turnip, expires in minutes, and
// authorizes writing to exactly one Operation — so capturing it requires
// network position and yields the power to forge results for an
// operation whose output the captor can already read.
func NewReporter(addr string, cfg Config) (*reporter, func() error, error) {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, nil, fmt.Errorf("runner: dial %s: %w", addr, err)
	}
	return newReporter(pb.NewOperationServiceClient(conn), cfg, time.Now, time.Sleep, rand.Float64), conn.Close, nil
}

func newReporter(client pb.OperationServiceClient, cfg Config, now func() time.Time, sleep func(time.Duration), rnd func() float64) *reporter {
	return &reporter{
		client: client,
		cfg:    cfg,
		now:    now,
		sleep:  sleep,
		rnd:    rnd,
		buf:    newLogRingBuffer(logBufferMaxBytes),
		readToken: func() (string, error) {
			raw, err := os.ReadFile(cfg.TokenFile)
			if err != nil {
				return "", fmt.Errorf("runner: reading %s: %w", cfg.TokenFile, err)
			}
			return strings.TrimSpace(string(raw)), nil
		},
	}
}

// Connect establishes the first stream to the Server and sends the initial
// OperationStart, retrying with the "initial connection" backoff row
// (Requirement 2.2/2.3). The underlying gRPC channel (created by
// NewReporter) manages its own low-level transport reconnection; what this
// retries is actually getting a stream opened and a Start message
// accepted, which is what surfaces whether the Server is reachable at all.
func (r *reporter) Connect(ctx context.Context) error {
	rt := retrier{base: connectBaseDelay, max: connectMaxDelay, budget: connectBudget, now: r.now, sleep: r.sleep, rnd: r.rnd}
	return rt.run(func() error {
		return r.openStream(ctx)
	})
}

func (r *reporter) openStream(ctx context.Context) error {
	token, err := r.readToken()
	if err != nil {
		return err
	}
	// The Operation id travels beside the credential because the Server's
	// interceptor runs before any message is received: it has to decide
	// whether this caller may write to this Operation in order to admit
	// the stream at all, so it cannot wait for the Start message that
	// also names one.
	ctx = metadata.AppendToOutgoingContext(ctx,
		rpc.AuthorizationKey, rpc.BearerPrefix+token,
		rpc.OperationIDKey, r.cfg.OperationID,
	)

	stream, err := r.client.ExecuteOperation(ctx)
	if err != nil {
		return err
	}

	if err := stream.Send(&pb.ExecuteOperationRequest{
		Payload: &pb.ExecuteOperationRequest_Start{Start: r.startMessage(r.everSent)},
	}); err != nil {
		return err
	}

	r.mu.Lock()
	r.stream = stream
	r.everSent = true
	r.mu.Unlock()
	return nil
}

func (r *reporter) startMessage(resumed bool) *pb.OperationStart {
	return &pb.OperationStart{
		OperationId: r.cfg.OperationID,
		ProjectName: r.cfg.ProjectName,
		ProjectDir:  r.cfg.ProjectDir,
		Tool:        r.cfg.Tool,
		Operation:   r.cfg.Operation,
		RepoUrl:     r.cfg.RepoURL,
		CommitSha:   r.cfg.CommitSHA,
		GithubToken: r.cfg.GitHubToken,
		ToolConfig:  r.cfg.ToolConfig,
		ExtraArgs:   r.cfg.ExtraArgs,
		PlanData:    r.cfg.PlanData,
		Resumed:     resumed,
	}
}

// LogLine buffers one log line and, if a stream is currently open, makes
// one best-effort attempt to send it immediately. A send failure here does
// not itself retry or reconnect — it just marks the stream not-open and
// leaves the line in the buffer for the next successful stream (opened by
// Report) to carry. This may block briefly on the gRPC send; callers that
// also need an unconditional, non-blocking local echo must do that before
// calling this (Requirement 4.7 — see run.go).
func (r *reporter) LogLine(stream, line string) {
	level := "info"
	if stream == "stderr" {
		level = "error"
	}
	pbLine := &pb.LogLine{Timestamp: r.now().Format(time.RFC3339Nano), Level: level, Message: line}
	r.buf.add(pbLine)

	r.mu.Lock()
	s := r.stream
	r.mu.Unlock()
	if s == nil {
		return
	}

	if err := s.Send(&pb.ExecuteOperationRequest{Payload: &pb.ExecuteOperationRequest_Log{Log: pbLine}}); err != nil {
		r.clearStream(s)
	}
}

// Report is the top-level driver of delivering an Operation's final
// result: it opens a stream if one isn't already open, resends the entire
// buffered log content (there is no per-message ack, so there's no way to
// know how much of a broken stream's content already arrived), sends the
// result, and waits for the Server's acknowledgment. Any failure at any
// point retries the whole sequence from a fresh stream, applying the
// "reconnection"/"final result delivery" backoff row (Requirement 2.4,
// 2.5, 4.4). Report is only ever called once Plugin.Execute has already
// returned, so nothing here ever touches or cancels the subprocess —
// Requirement 2.4's "don't abort the subprocess" guarantee holds
// structurally, by Report and subprocess execution never sharing a
// cancellation path.
func (r *reporter) Report(ctx context.Context, result OperationResult) error {
	rt := retrier{base: reportBaseDelay, max: reportMaxDelay, budget: reportBudget, now: r.now, sleep: r.sleep, rnd: r.rnd}
	if err := rt.run(func() error {
		return r.reportOnce(ctx, result)
	}); err != nil {
		return fmt.Errorf("%w: %w", ErrReportFailed, err)
	}
	return nil
}

func (r *reporter) reportOnce(ctx context.Context, result OperationResult) error {
	r.mu.Lock()
	stream := r.stream
	r.mu.Unlock()

	if stream == nil {
		if err := r.openStream(ctx); err != nil {
			return err
		}
		r.mu.Lock()
		stream = r.stream
		r.mu.Unlock()
	}

	for _, line := range r.buf.snapshot() {
		if err := stream.Send(&pb.ExecuteOperationRequest{Payload: &pb.ExecuteOperationRequest_Log{Log: line}}); err != nil {
			r.clearStream(stream)
			return err
		}
	}

	pbResult := &pb.OperationResult{
		Success:      result.Success,
		Output:       result.Output,
		ExitCode:     int32(result.ExitCode),
		ErrorMessage: result.ErrorMessage,
		Changes: &pb.ChangeSummary{
			Add:     int32(result.Changes.Add),
			Change:  int32(result.Changes.Change),
			Destroy: int32(result.Changes.Destroy),
		},
		PlanData: result.PlanData,
	}
	if err := stream.Send(&pb.ExecuteOperationRequest{Payload: &pb.ExecuteOperationRequest_Result{Result: pbResult}}); err != nil {
		r.clearStream(stream)
		return err
	}

	if _, err := stream.CloseAndRecv(); err != nil {
		r.clearStream(stream)
		return err
	}

	return nil
}

func (r *reporter) clearStream(s pb.OperationService_ExecuteOperationClient) {
	r.mu.Lock()
	if r.stream == s {
		r.stream = nil
	}
	r.mu.Unlock()
}

// logRingBuffer accumulates LogLines up to a bounded byte size, dropping
// the oldest entries and tracking a count once that bound is exceeded
// (Requirement 4.3).
type logRingBuffer struct {
	mu           sync.Mutex
	maxBytes     int
	lines        []*pb.LogLine
	size         int
	droppedCount int
}

func newLogRingBuffer(maxBytes int) *logRingBuffer {
	return &logRingBuffer{maxBytes: maxBytes}
}

func (b *logRingBuffer) add(line *pb.LogLine) {
	b.mu.Lock()
	defer b.mu.Unlock()

	entrySize := logLineSize(line)
	for b.size+entrySize > b.maxBytes && len(b.lines) > 0 {
		dropped := b.lines[0]
		b.lines = b.lines[1:]
		b.size -= logLineSize(dropped)
		b.droppedCount++
	}
	b.lines = append(b.lines, line)
	b.size += entrySize
}

// snapshot returns every line currently buffered, prefixed with a
// synthetic "N lines dropped" marker if any have been evicted.
func (b *logRingBuffer) snapshot() []*pb.LogLine {
	b.mu.Lock()
	defer b.mu.Unlock()

	out := make([]*pb.LogLine, 0, len(b.lines)+1)
	if b.droppedCount > 0 {
		out = append(out, &pb.LogLine{Level: "warn", Message: fmt.Sprintf("%d lines dropped", b.droppedCount)})
	}
	out = append(out, b.lines...)
	return out
}

func logLineSize(l *pb.LogLine) int {
	return len(l.GetTimestamp()) + len(l.GetLevel()) + len(l.GetMessage())
}
