package rpc

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
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
	mu            sync.Mutex
	calls         []recordedHandlerCall
	logErr        error
	resultErr     error
	credentialErr error
	// credentialFor records the Operation each credential request was
	// served for — the authenticated one, never one the caller named.
	credentialFor []string
}

func (h *fakeHandler) HandleLog(ctx context.Context, operationID string, line LogLine) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.calls = append(h.calls, recordedHandlerCall{kind: "log", operationID: operationID, log: line})
	return h.logErr
}

// CloneCredential records what it was asked for and answers with a
// scripted credential. operationID is whatever the interceptor
// established, which is the property the credential tests assert.
func (h *fakeHandler) CloneCredential(ctx context.Context, operationID string) (CloneCredential, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.credentialFor = append(h.credentialFor, operationID)
	if h.credentialErr != nil {
		return CloneCredential{}, h.credentialErr
	}
	return CloneCredential{Token: "ghs_for_" + operationID}, nil
}

func (h *fakeHandler) HandleResult(ctx context.Context, operationID string, result OperationResult) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.calls = append(h.calls, recordedHandlerCall{kind: "result", operationID: operationID, result: result})
	return h.resultErr
}

// acceptAll is the authenticator these dispatch tests use: they are about
// what the handler does with a stream's messages, not about who may open
// one. auth_test.go covers the decision itself.
type acceptAll struct{}

func (acceptAll) Authenticate(context.Context, string, string) (Identity, error) {
	return IdentityReport, nil
}

// authedContext produces the metadata a Runner attaches to every stream.
// Every test here needs it, because the interceptor is installed
// unconditionally — there is no unauthenticated path to the handler.
func authedContext(operationID string) context.Context {
	return metadata.AppendToOutgoingContext(context.Background(),
		AuthorizationKey, BearerPrefix+"test-token",
		OperationIDKey, operationID,
	)
}

// dialServer starts a server hosting handler on an in-process bufconn
// listener and returns a connected client.
func dialServer(t *testing.T, handler OperationHandler) pb.OperationServiceClient {
	t.Helper()

	lis := bufconn.Listen(1024 * 1024)
	server := NewServer(handler, WithAuthenticator(acceptAll{}))
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

	stream, err := client.ExecuteOperation(authedContext("op-1"))
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
	require.Len(t, handler.calls, 2, "the stream's first log line, then the result")
	assert.Equal(t, "log", handler.calls[0].kind)
	assert.Equal(t, "op-1", handler.calls[0].operationID)
	assert.Equal(t, "first", handler.calls[0].log.Message)
	assert.Equal(t, "result", handler.calls[1].kind)
	assert.Equal(t, "op-1", handler.calls[1].operationID)
	assert.True(t, handler.calls[1].result.Success)
	assert.Equal(t, "done", handler.calls[1].result.Output)
}

// sendLogStream opens a stream for operationID, sends lines log lines and a
// result, and waits for the Server's answer.
func sendLogStream(t *testing.T, client pb.OperationServiceClient, operationID string, lines int) {
	t.Helper()

	stream, err := client.ExecuteOperation(authedContext(operationID))
	require.NoError(t, err)
	require.NoError(t, stream.Send(&pb.ExecuteOperationRequest{
		Payload: &pb.ExecuteOperationRequest_Start{Start: &pb.OperationStart{OperationId: operationID}},
	}))
	for i := range lines {
		require.NoError(t, stream.Send(&pb.ExecuteOperationRequest{
			Payload: &pb.ExecuteOperationRequest_Log{Log: &pb.LogLine{Level: "info", Message: fmt.Sprintf("line %d", i)}},
		}))
	}
	require.NoError(t, stream.Send(&pb.ExecuteOperationRequest{
		Payload: &pb.ExecuteOperationRequest_Result{Result: &pb.OperationResult{Success: true}},
	}))
	_, err = stream.CloseAndRecv()
	require.NoError(t, err)
}

func (h *fakeHandler) logCalls() []recordedHandlerCall {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []recordedHandlerCall
	for _, c := range h.calls {
		if c.kind == "log" {
			out = append(out, c)
		}
	}
	return out
}

// A verbose plan costs the handler one call, not one per line: the first
// line marks the Operation started and the Server has no use for the rest.
func TestExecuteOperation_HandlerHearsOnlyAStreamsFirstLogLine(t *testing.T) {
	handler := &fakeHandler{}
	client := dialServer(t, handler)

	sendLogStream(t, client, "op-1", 20000)

	logs := handler.logCalls()
	require.Len(t, logs, 1)
	assert.Equal(t, "line 0", logs[0].log.Message)
}

// Once per stream, not once per Operation: a resumed stream may reach a
// replica that never saw the first, so it marks the Operation again.
func TestExecuteOperation_ASecondStreamIsHeardOnceMore(t *testing.T) {
	handler := &fakeHandler{}
	client := dialServer(t, handler)

	sendLogStream(t, client, "op-1", 50)
	sendLogStream(t, client, "op-1", 50)

	logs := handler.logCalls()
	require.Len(t, logs, 2)
	assert.Equal(t, "op-1", logs[0].operationID)
	assert.Equal(t, "op-1", logs[1].operationID)
}

func TestExecuteOperation_HandleLogErrorAbortsRPC(t *testing.T) {
	handler := &fakeHandler{logErr: errors.New("redis is down")}
	client := dialServer(t, handler)

	stream, err := client.ExecuteOperation(authedContext("op-1"))
	require.NoError(t, err)
	require.NoError(t, stream.Send(&pb.ExecuteOperationRequest{
		Payload: &pb.ExecuteOperationRequest_Log{Log: &pb.LogLine{Level: "info", Message: "first"}},
	}))

	_, err = stream.CloseAndRecv()
	require.Error(t, err, "a failure to mark the Operation started ends the stream")
}

func TestExecuteOperation_HandleResultErrorAbortsRPC(t *testing.T) {
	handler := &fakeHandler{resultErr: errors.New("boom")}
	client := dialServer(t, handler)

	stream, err := client.ExecuteOperation(authedContext("op-2"))
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

	stream, err := client.ExecuteOperation(authedContext("op-3"))
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

// sendResultOfSize sends a start message and then a result whose Output
// makes the message roughly size bytes, and returns the stream's close
// error.
func sendResultOfSize(t *testing.T, client pb.OperationServiceClient, operationID string, size int) error {
	t.Helper()

	stream, err := client.ExecuteOperation(authedContext(operationID))
	require.NoError(t, err)
	require.NoError(t, stream.Send(&pb.ExecuteOperationRequest{
		Payload: &pb.ExecuteOperationRequest_Start{Start: &pb.OperationStart{OperationId: operationID}},
	}))
	// A refused message can surface on Send or on CloseAndRecv, so only
	// the final error is asserted on.
	_ = stream.Send(&pb.ExecuteOperationRequest{
		Payload: &pb.ExecuteOperationRequest_Result{Result: &pb.OperationResult{
			Success: true,
			Output:  strings.Repeat("a", size),
		}},
	})
	_, err = stream.CloseAndRecv()
	return err
}

func TestExecuteOperation_ResultOverLimitIsResourceExhausted(t *testing.T) {
	handler := &fakeHandler{}
	client := dialServer(t, handler)

	err := sendResultOfSize(t, client, "op-big", MaxResultMessageBytes+1)
	require.Error(t, err)
	assert.Equal(t, codes.ResourceExhausted, status.Code(err))

	handler.mu.Lock()
	defer handler.mu.Unlock()
	assert.Empty(t, handler.calls)
}

func TestExecuteOperation_ResultUnderLimitIsAccepted(t *testing.T) {
	handler := &fakeHandler{}
	client := dialServer(t, handler)

	// Leave room for the field tags and length prefixes around Output.
	err := sendResultOfSize(t, client, "op-fits", MaxResultMessageBytes-1024)
	require.NoError(t, err)

	handler.mu.Lock()
	defer handler.mu.Unlock()
	require.Len(t, handler.calls, 1)
	assert.Equal(t, "result", handler.calls[0].kind)
}
