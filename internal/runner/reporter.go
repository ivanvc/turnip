package runner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

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

	// replayMaxBytes bounds what is replayed on a stream that replaces a
	// broken one: the most recent log lines of the record up to this
	// size (Requirement 4.3).
	replayMaxBytes = 256 * 1024
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
	// FailureCategory names the step that failed, set at each failure
	// point in run.go, so the Server can title the check run without
	// reading ErrorMessage. Unspecified on success.
	FailureCategory rpc.FailureCategory
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

	// rec is the record of the tool's output the sender follows, clean
	// what each line passes through before it leaves for the Server, and
	// sent is closed when the sender has stopped: it sent the record's
	// last line, or a send failed. All three are set by Follow and nil
	// for a reporter that has no output to send (the clone container's).
	rec   *plugin.Record
	clean func(string) string
	sent  chan struct{}

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
// Runner presents its Runner identity over a plaintext pod-to-pod
// channel. The Runner identity is audience-scoped to turnip, expires in
// minutes, and is accepted for exactly one Operation, so capturing it
// requires network position. A captured report identity yields the power
// to forge results for an operation whose output the captor can already
// read; a captured clone identity also yields that Operation's GitHub
// credential until it expires.
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
		ToolConfig:  r.cfg.ToolConfig,
		ExtraArgs:   r.cfg.ExtraArgs,
		PlanData:    r.cfg.PlanData,
		Resumed:     resumed,
	}
}

// Follow starts sending rec's lines to the Server as log lines, each
// passed through clean first, and returns at once. It is called once,
// after Connect and before the tool runs.
//
// The sender reads the record through a cursor of its own, so it takes as
// long as the Server takes without holding anything else up: the tool, the
// record and the Runner's own log never wait on it. It sends every line
// once, in the record's order, until it has sent the last line of the
// closed record or a send fails. It does not reconnect: without an
// acknowledgment per line there is no knowing which lines a broken stream
// delivered, so what follows a break is Report's replay.
func (r *reporter) Follow(rec *plugin.Record, clean func(string) string) {
	r.rec, r.clean = rec, clean
	r.sent = make(chan struct{})
	go r.send()
}

func (r *reporter) send() {
	defer close(r.sent)

	from := 0
	for {
		lines, done := r.rec.Next(context.Background(), from)
		for _, line := range lines {
			r.mu.Lock()
			s := r.stream
			r.mu.Unlock()
			if s == nil {
				return
			}
			if err := s.Send(logRequest(r.logLine(line))); err != nil {
				r.clearStream(s)
				return
			}
		}
		from += len(lines)
		if done {
			return
		}
	}
}

// logLine is line as the Server receives it: stamped with when the tool
// wrote it, not when it is sent, so a line delivered late still says when
// it happened.
func (r *reporter) logLine(line plugin.Line) *pb.LogLine {
	level := "info"
	if line.Stream == plugin.Stderr {
		level = "error"
	}
	return &pb.LogLine{Timestamp: line.At.Format(time.RFC3339Nano), Level: level, Message: r.clean(line.Text)}
}

func logRequest(line *pb.LogLine) *pb.ExecuteOperationRequest {
	return &pb.ExecuteOperationRequest{Payload: &pb.ExecuteOperationRequest_Log{Log: line}}
}

// replay is what a stream that replaces a broken one carries before the
// result: the record's most recent log lines, as many as fit
// replayMaxBytes counting back from the end, preceded by a "N lines
// dropped" marker when the record has more.
func (r *reporter) replay() []*pb.LogLine {
	if r.rec == nil {
		return nil
	}
	all, _ := r.rec.Next(context.Background(), 0)

	var tail []*pb.LogLine
	size := 0
	first := len(all)
	for first > 0 {
		line := r.logLine(all[first-1])
		if n := logLineSize(line); size+n > replayMaxBytes {
			break
		} else {
			size += n
		}
		tail = append(tail, line)
		first--
	}
	slices.Reverse(tail)

	if first == 0 {
		return tail
	}
	marker := &pb.LogLine{Level: "warn", Message: fmt.Sprintf("%d lines dropped", first)}
	return append([]*pb.LogLine{marker}, tail...)
}

// Report is the top-level driver of delivering an Operation's final
// result. It first waits for the sender Follow started to stop, so the
// result follows every log line on the stream. On a stream that never
// broke it then sends the result and waits for the Server's
// acknowledgment: every line was sent once, and nothing is replayed. When
// the stream broke, it opens a new one and replays the record's most
// recent lines before the result (there is no per-message ack, so there's
// no way to know how much of a broken stream's content already arrived).
// Any failure at any point retries from a fresh stream, replay included,
// applying the "reconnection"/"final result delivery" backoff row
// (Requirement 2.4, 2.5, 4.4). Report is only ever called once Plugin.Execute has already
// returned, so nothing here ever touches or cancels the subprocess —
// Requirement 2.4's "don't abort the subprocess" guarantee holds
// structurally, by Report and subprocess execution never sharing a
// cancellation path.
//
// A result whose message would not fit what the Server accepts is never
// sent: the failed result deliverable returns goes in its place. A result
// the Server refuses for its size anyway (ResourceExhausted, from a Server
// configured differently) is not retried either; the same failed result
// replaces it, under the same budget.
func (r *reporter) Report(ctx context.Context, result OperationResult) error {
	if r.sent != nil {
		// Outside the retry budget: this is the Server reading the log
		// lines, not a delivery failing.
		select {
		case <-r.sent:
		case <-ctx.Done():
			return fmt.Errorf("%w: %w", ErrReportFailed, ctx.Err())
		}
	}

	current, replaced := deliverable(result)
	rt := retrier{base: reportBaseDelay, max: reportMaxDelay, budget: reportBudget, now: r.now, sleep: r.sleep, rnd: r.rnd}
	if err := rt.run(func() error {
		err := r.reportOnce(ctx, current)
		if !replaced && errors.Is(err, errResultRefusedForSize) {
			current, replaced = tooLargeResult(current, proto.Size(resultRequest(current))), true
		}
		return err
	}); err != nil {
		return fmt.Errorf("%w: %w", ErrReportFailed, err)
	}
	return nil
}

// errResultRefusedForSize marks a result the Server answered with
// ResourceExhausted, which no retry of the same message can change.
var errResultRefusedForSize = errors.New("runner: the Server refused the result for its size")

// resultRequest is the message that carries result to the Server. Its
// encoded size is what the Server's limit applies to.
func resultRequest(result OperationResult) *pb.ExecuteOperationRequest {
	return &pb.ExecuteOperationRequest{Payload: &pb.ExecuteOperationRequest_Result{Result: &pb.OperationResult{
		Success:      result.Success,
		Output:       result.Output,
		ExitCode:     int32(result.ExitCode),
		ErrorMessage: result.ErrorMessage,
		Changes: &pb.ChangeSummary{
			Add:     int32(result.Changes.Add),
			Change:  int32(result.Changes.Change),
			Destroy: int32(result.Changes.Destroy),
		},
		PlanData:        result.PlanData,
		FailureCategory: rpc.FailureCategoryToProto(result.FailureCategory),
	}}}
}

// deliverable returns result when its message fits rpc.MaxResultMessageBytes,
// and otherwise the failed result that replaces it, with replaced true.
func deliverable(result OperationResult) (OperationResult, bool) {
	size := proto.Size(resultRequest(result))
	if size <= rpc.MaxResultMessageBytes {
		return result, false
	}
	return tooLargeResult(result, size), true
}

// tooLargeResult is the failed result delivered in place of one whose
// message was size bytes, too large for the Server: no output, no plan
// data, no counts and no category, only an error message saying why. The
// tool's exit code is kept, since the tool did run.
func tooLargeResult(result OperationResult, size int) OperationResult {
	// A size just over the limit rounds to the limit itself ("4.0 MiB;
	// the Server accepts at most 4 MiB"), which reads as a contradiction;
	// the exact byte count says what the rounding hides.
	shown := formatSize(size)
	if shown == formatSize(rpc.MaxResultMessageBytes) {
		shown = fmt.Sprintf("%d bytes", size)
	}
	return OperationResult{
		Success:  false,
		ExitCode: result.ExitCode,
		ErrorMessage: fmt.Sprintf("the result was too large to deliver (%s; the Server accepts at most %d MiB)",
			shown, rpc.MaxResultMessageBytes>>20),
	}
}

func (r *reporter) reportOnce(ctx context.Context, result OperationResult) error {
	r.mu.Lock()
	stream := r.stream
	r.mu.Unlock()

	if stream == nil {
		// The stream the log lines went out on is gone: the sender's, or
		// an earlier attempt's here. Its replacement replays.
		if err := r.openStream(ctx); err != nil {
			return err
		}
		r.mu.Lock()
		stream = r.stream
		r.mu.Unlock()

		for _, line := range r.replay() {
			if err := stream.Send(logRequest(line)); err != nil {
				r.clearStream(stream)
				return err
			}
		}
	}

	if err := stream.Send(resultRequest(result)); err != nil {
		r.clearStream(stream)
		// io.EOF means the Server ended the stream; its status, which
		// says why, is only read by receiving.
		if errors.Is(err, io.EOF) {
			if _, recvErr := stream.CloseAndRecv(); recvErr != nil {
				err = recvErr
			}
		}
		return classifyResultError(err)
	}

	if _, err := stream.CloseAndRecv(); err != nil {
		r.clearStream(stream)
		return classifyResultError(err)
	}

	return nil
}

// classifyResultError marks a ResourceExhausted answer to a result with
// errResultRefusedForSize, leaving every other error as it is.
func classifyResultError(err error) error {
	if status.Code(err) == codes.ResourceExhausted {
		return fmt.Errorf("%w: %w", errResultRefusedForSize, err)
	}
	return err
}

func (r *reporter) clearStream(s pb.OperationService_ExecuteOperationClient) {
	r.mu.Lock()
	if r.stream == s {
		r.stream = nil
	}
	r.mu.Unlock()
}

// logLineSize is what a log line counts for against replayMaxBytes.
func logLineSize(l *pb.LogLine) int {
	return len(l.GetTimestamp()) + len(l.GetLevel()) + len(l.GetMessage())
}
