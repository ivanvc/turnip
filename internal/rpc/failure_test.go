package rpc

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/ivanvc/turnip/internal/grpc/turnip/v1"
)

var allFailureCategories = []FailureCategory{
	FailureUnspecified, FailureToolExited, FailureCloneFailed, FailureWorkspaceFailed, FailureToolNotStarted,
}

func TestFailureCategory_RoundTripsThroughTheProtoEnum(t *testing.T) {
	for _, c := range allFailureCategories {
		assert.Equal(t, c, FailureCategoryFromProto(FailureCategoryToProto(c)))
	}
	// Every wire value has a Go value, so nothing the proto defines is
	// silently read as unspecified.
	assert.Len(t, pb.FailureCategory_name, len(allFailureCategories))
}

// A category the Runner sets arrives at HandleResult.
func TestExecuteOperation_FailureCategoryPropagated(t *testing.T) {
	for _, c := range allFailureCategories {
		handler := &fakeHandler{}
		client := dialServer(t, handler)

		stream, err := client.ExecuteOperation(authedContext("op-4"))
		require.NoError(t, err)
		require.NoError(t, stream.Send(&pb.ExecuteOperationRequest{
			Payload: &pb.ExecuteOperationRequest_Start{Start: &pb.OperationStart{OperationId: "op-4"}},
		}))
		require.NoError(t, stream.Send(&pb.ExecuteOperationRequest{
			Payload: &pb.ExecuteOperationRequest_Result{Result: &pb.OperationResult{
				ExitCode:        1,
				FailureCategory: FailureCategoryToProto(c),
			}},
		}))
		_, err = stream.CloseAndRecv()
		require.NoError(t, err)

		handler.mu.Lock()
		require.Len(t, handler.calls, 1)
		assert.Equal(t, c, handler.calls[0].result.FailureCategory)
		handler.mu.Unlock()
	}
}
