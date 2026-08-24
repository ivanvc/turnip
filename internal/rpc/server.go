package rpc

import (
	"context"
	"io"

	"google.golang.org/grpc"

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
}

// NewServer constructs a *grpc.Server hosting OperationService, dispatching
// every message a Runner sends to handler.
func NewServer(handler OperationHandler) *grpc.Server {
	s := grpc.NewServer()
	pb.RegisterOperationServiceServer(s, &operationServer{handler: handler})
	return s
}

type operationServer struct {
	pb.UnimplementedOperationServiceServer
	handler OperationHandler
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
	var operationID string

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
			operationID = payload.Start.GetOperationId()

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
