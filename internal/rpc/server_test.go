package rpc

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/ivanvc/turnip/internal/grpc/turnip/v1"
)

type recordedHandlerCall struct {
	kind        string // "log" or "result"
	operationID string
	log         LogLine
	result      OperationResult
}

type fakeHandler struct {
	mu        sync.Mutex
	calls     []recordedHandlerCall
	resultErr error
}

func (h *fakeHandler) HandleLog(ctx context.Context, operationID string, line LogLine) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.calls = append(h.calls, recordedHandlerCall{kind: "log", operationID: operationID, log: line})
	return nil
}

func (h *fakeHandler) HandleResult(ctx context.Context, operationID string, result OperationResult) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.calls = append(h.calls, recordedHandlerCall{kind: "result", operationID: operationID, result: result})
	return h.resultErr
}

// dialServer starts a server hosting handler on an in-process bufconn
// listener and returns a connected client.
func dialServer(t *testing.T, handler OperationHandler) pb.OperationServiceClient {
	t.Helper()

	lis := bufconn.Listen(1024 * 1024)
	server := NewServer(handler)
	go func() {
		_ = server.Serve(lis)
	}()
	t.Cleanup(server.Stop)

	conn, err := grpc.NewClient("passthrough:///bufconn",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	return pb.NewOperationServiceClient(conn)
}

func TestExecuteOperation_FullStreamDispatchesInOrder(t *testing.T) {
	handler := &fakeHandler{}
	client := dialServer(t, handler)

	stream, err := client.ExecuteOperation(context.Background())
	require.NoError(t, err)

	require.NoError(t, stream.Send(&pb.ExecuteOperationRequest{
		Payload: &pb.ExecuteOperationRequest_Start{Start: &pb.OperationStart{OperationId: "op-1"}},
	}))
	require.NoError(t, stream.Send(&pb.ExecuteOperationRequest{
		Payload: &pb.ExecuteOperationRequest_Log{Log: &pb.LogLine{Level: "info", Message: "first"}},
	}))
	require.NoError(t, stream.Send(&pb.ExecuteOperationRequest{
		Payload: &pb.ExecuteOperationRequest_Log{Log: &pb.LogLine{Level: "info", Message: "second"}},
	}))
	require.NoError(t, stream.Send(&pb.ExecuteOperationRequest{
		Payload: &pb.ExecuteOperationRequest_Result{Result: &pb.OperationResult{Success: true, Output: "done"}},
	}))

	resp, err := stream.CloseAndRecv()
	require.NoError(t, err)
	assert.NotNil(t, resp)

	handler.mu.Lock()
	defer handler.mu.Unlock()
	require.Len(t, handler.calls, 3)
	assert.Equal(t, "log", handler.calls[0].kind)
	assert.Equal(t, "op-1", handler.calls[0].operationID)
	assert.Equal(t, "first", handler.calls[0].log.Message)
	assert.Equal(t, "log", handler.calls[1].kind)
	assert.Equal(t, "second", handler.calls[1].log.Message)
	assert.Equal(t, "result", handler.calls[2].kind)
	assert.Equal(t, "op-1", handler.calls[2].operationID)
	assert.True(t, handler.calls[2].result.Success)
	assert.Equal(t, "done", handler.calls[2].result.Output)
}

func TestExecuteOperation_HandleResultErrorAbortsRPC(t *testing.T) {
	handler := &fakeHandler{resultErr: errors.New("boom")}
	client := dialServer(t, handler)

	stream, err := client.ExecuteOperation(context.Background())
	require.NoError(t, err)

	require.NoError(t, stream.Send(&pb.ExecuteOperationRequest{
		Payload: &pb.ExecuteOperationRequest_Start{Start: &pb.OperationStart{OperationId: "op-2"}},
	}))
	require.NoError(t, stream.Send(&pb.ExecuteOperationRequest{
		Payload: &pb.ExecuteOperationRequest_Result{Result: &pb.OperationResult{Success: true}},
	}))

	_, err = stream.CloseAndRecv()
	require.Error(t, err)
}

func TestExecuteOperation_ChangeSummaryPropagated(t *testing.T) {
	handler := &fakeHandler{}
	client := dialServer(t, handler)

	stream, err := client.ExecuteOperation(context.Background())
	require.NoError(t, err)

	require.NoError(t, stream.Send(&pb.ExecuteOperationRequest{
		Payload: &pb.ExecuteOperationRequest_Start{Start: &pb.OperationStart{OperationId: "op-3"}},
	}))
	require.NoError(t, stream.Send(&pb.ExecuteOperationRequest{
		Payload: &pb.ExecuteOperationRequest_Result{Result: &pb.OperationResult{
			Success: true,
			Changes: &pb.ChangeSummary{Add: 1, Change: 2, Destroy: 3},
		}},
	}))

	_, err = stream.CloseAndRecv()
	require.NoError(t, err)

	handler.mu.Lock()
	defer handler.mu.Unlock()
	require.Len(t, handler.calls, 1)
	assert.Equal(t, ChangeSummary{Add: 1, Change: 2, Destroy: 3}, handler.calls[0].result.Changes)
}
