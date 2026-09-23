package rpc

import (
	"context"
	"io"
	"log/slog"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/ivanvc/turnip/internal/grpc/turnip/v1"
)

// LogLine is one line of an Operation's output, decoupled from the
// generated protobuf type so OperationHandler implementations don't need
// to depend on it directly.
type LogLine struct {
	Timestamp string
	Level     string
	Message   string
}

// ChangeSummary reports the add/change/destroy counts from a completed
// Operation.
type ChangeSummary struct {
	Add     int32
	Change  int32
	Destroy int32
}

// OperationResult is an Operation's final outcome.
type OperationResult struct {
	Success      bool
	Output       string
	ExitCode     int32
	ErrorMessage string
	Changes      ChangeSummary
	PlanData     []byte
}

// OperationHandler reacts to a Runner's incremental log lines and final
// result for one Operation, identified by operationID. Implementations
// decide what a log line or result *means* (e.g. releasing a lock, posting
// a GitHub comment) — that's Slice 6's responsibility, not this package's.
type OperationHandler interface {
	HandleLog(ctx context.Context, operationID string, line LogLine) error
	HandleResult(ctx context.Context, operationID string, result OperationResult) error
	// CloneCredential returns the GitHub credential operationID's clone
	// should use. operationID is always the authenticated one — see
	// FetchCloneCredential.
	CloneCredential(ctx context.Context, operationID string) (CloneCredential, error)
}

// CloneCredential is what a Runner fetches instead of being handed a
// token in its Pod spec.
type CloneCredential struct {
	Token     string
	ExpiresAt time.Time
}

// Option configures a Server. NewServer took no options before Slice 25,
// which is precisely why the interceptor goes here: there is exactly one
// place to add it, and it then applies to every RPC registered
// afterwards.
type Option func(*serverOptions)

type serverOptions struct {
	auth Authenticator
}

// WithAuthenticator installs the check that decides which Operation a
// caller may write to. Without it every stream is refused — see
// streamAuthInterceptor.
func WithAuthenticator(auth Authenticator) Option {
	return func(o *serverOptions) { o.auth = auth }
}

// NewServer constructs a *grpc.Server hosting OperationService, dispatching
// every message a Runner sends to handler.
func NewServer(handler OperationHandler, opts ...Option) *grpc.Server {
	var o serverOptions
	for _, opt := range opts {
		opt(&o)
	}

	// The channel is not encrypted. That is a separate axis, deliberately
	// split out (see the TLS slice): what this interceptor establishes is
	// *who* is calling, which holds whether or not the transport is
	// encrypted. Without TLS a caller with network position can capture a
	// token — a far higher bar than reading TURNIP_OPERATION_ID out of a
	// Pod spec, which is what this replaces — and the token it captures is
	// audience-scoped, short-lived, and authorizes writing to exactly one
	// Operation.
	s := grpc.NewServer(
		grpc.StreamInterceptor(streamAuthInterceptor(o.auth)),
		// Both kinds, so "an RPC added later is authenticated" is true
		// whichever kind its author reaches for. Installing only one is
		// the defect Slice 24 found in this function.
		grpc.UnaryInterceptor(unaryAuthInterceptor(o.auth)),
	)
	pb.RegisterOperationServiceServer(s, &operationServer{handler: handler})
	return s
}

type operationServer struct {
	pb.UnimplementedOperationServiceServer
	handler OperationHandler
}

// FetchCloneCredential returns the credential for the Operation the
// caller was authenticated as.
//
// The request is ignored, and that is the design rather than an
// oversight: the parameter is named "_" so that adding a field to it and
// reading it here requires deleting this comment first. A Runner
// authenticated for one Operation asking for another's token is the
// cross-repository theft this whole slice narrows the blast radius of;
// the way it is prevented is by there being nothing to read.
func (s *operationServer) FetchCloneCredential(ctx context.Context, _ *pb.FetchCloneCredentialRequest) (*pb.FetchCloneCredentialResponse, error) {
	operationID, ok := OperationIDFromContext(ctx)
	if !ok {
		// Unreachable while the unary interceptor is installed, which it
		// unconditionally is. Refusing rather than proceeding means a
		// future refactor that removes it fails loudly instead of handing
		// credentials to anyone who asks.
		return nil, status.Error(codes.Unauthenticated, "rpc: credential request reached handler unauthenticated")
	}

	cred, err := s.handler.CloneCredential(ctx, operationID)
	if err != nil {
		slog.ErrorContext(ctx, "could not mint a clone credential", "operation_id", operationID, "error", err)
		return nil, status.Error(codes.Internal, "rpc: could not mint a clone credential")
	}

	return &pb.FetchCloneCredentialResponse{
		Token:         cred.Token,
		ExpiresAtUnix: cred.ExpiresAt.Unix(),
	}, nil
}

// ExecuteOperation receives a Runner's start/log/result stream and
// dispatches each message to the configured OperationHandler. A "start"
// message is a no-op at this layer beyond recording the Operation ID —
// nothing in this slice's scope reacts to an Operation merely starting. A
// handler error aborts the loop and is returned as the RPC's status, so
// the Runner's reporter sees the call fail and retries rather than the
// failure being silently swallowed.
func (s *operationServer) ExecuteOperation(stream pb.OperationService_ExecuteOperationServer) error {
	ctx := stream.Context()

	// The Operation id comes from what the interceptor established, never
	// from the stream's own Start message. A Runner that knows an
	// Operation id is not thereby entitled to write to it — that was the
	// entire gap Slice 25 closed, and reading the id back off the wire
	// would reopen it with a one-line change that looks like a
	// simplification.
	operationID, ok := OperationIDFromContext(ctx)
	if !ok {
		// Unreachable while the interceptor is installed, which it
		// unconditionally is. Refusing rather than proceeding with an
		// empty id means a future refactor that removes it fails loudly.
		return status.Error(codes.Unauthenticated, "rpc: stream reached handler unauthenticated")
	}

	for {
		req, err := stream.Recv()
		if err == io.EOF {
			return stream.SendAndClose(&pb.ExecuteOperationResponse{})
		}
		if err != nil {
			return err
		}

		switch payload := req.GetPayload().(type) {
		case *pb.ExecuteOperationRequest_Start:
			// OperationStart.operation_id is now decorative: the
			// authenticated id above supersedes it. The field is not
			// removed, because pruning the message is a protocol question
			// (Slice 24 records it) rather than a security one — but it is
			// logged when it disagrees, since a mismatch means a Runner
			// built against a different Job than the one that authenticated.
			if claimed := payload.Start.GetOperationId(); claimed != "" && claimed != operationID {
				slog.WarnContext(ctx, "operation id in start message disagrees with the authenticated one; ignoring it",
					"authenticated", operationID, "claimed", claimed)
			}

		case *pb.ExecuteOperationRequest_Log:
			log := payload.Log
			if err := s.handler.HandleLog(ctx, operationID, LogLine{
				Timestamp: log.GetTimestamp(),
				Level:     log.GetLevel(),
				Message:   log.GetMessage(),
			}); err != nil {
				return err
			}

		case *pb.ExecuteOperationRequest_Result:
			result := payload.Result
			if err := s.handler.HandleResult(ctx, operationID, OperationResult{
				Success:      result.GetSuccess(),
				Output:       result.GetOutput(),
				ExitCode:     result.GetExitCode(),
				ErrorMessage: result.GetErrorMessage(),
				Changes: ChangeSummary{
					Add:     result.GetChanges().GetAdd(),
					Change:  result.GetChanges().GetChange(),
					Destroy: result.GetChanges().GetDestroy(),
				},
				PlanData: result.GetPlanData(),
			}); err != nil {
				return err
			}
		}
	}
}
