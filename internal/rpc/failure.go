package rpc

import (
	pb "github.com/ivanvc/turnip/internal/grpc/turnip/v1"
)

// FailureCategory is which step of an Operation failed, as the Runner
// reports it. It exists so the Server can title a check run by what
// failed without reading the error message's text, which would break the
// first time a message was reworded (check-run-titles Requirement 5).
//
// One type for both sides: the Runner sets it and the Server reads it,
// and each translates it at the wire exactly as it already does the rest
// of OperationResult.
type FailureCategory int

const (
	// FailureUnspecified is the zero value: a success, or a failure the
	// Runner did not categorize.
	FailureUnspecified FailureCategory = iota
	// FailureToolExited: the tool ran and exited non-zero.
	FailureToolExited
	// FailureCloneFailed: the repository could not be cloned.
	FailureCloneFailed
	// FailureWorkspaceFailed: the workspace directory could not be created.
	FailureWorkspaceFailed
	// FailureToolNotStarted: the Plugin could not start the tool, so there
	// is no exit code.
	FailureToolNotStarted
)

var failureToProto = map[FailureCategory]pb.FailureCategory{
	FailureUnspecified:     pb.FailureCategory_FAILURE_CATEGORY_UNSPECIFIED,
	FailureToolExited:      pb.FailureCategory_FAILURE_CATEGORY_TOOL_EXITED,
	FailureCloneFailed:     pb.FailureCategory_FAILURE_CATEGORY_CLONE_FAILED,
	FailureWorkspaceFailed: pb.FailureCategory_FAILURE_CATEGORY_WORKSPACE_FAILED,
	FailureToolNotStarted:  pb.FailureCategory_FAILURE_CATEGORY_TOOL_NOT_STARTED,
}

// FailureCategoryToProto translates for the wire. An unknown value sends
// unspecified, which the Server titles honestly as a plain failure.
func FailureCategoryToProto(c FailureCategory) pb.FailureCategory {
	return failureToProto[c]
}

// FailureCategoryFromProto translates from the wire. A value this Server
// does not know — sent by a newer Runner — reads as unspecified.
func FailureCategoryFromProto(c pb.FailureCategory) FailureCategory {
	for category, wire := range failureToProto {
		if wire == c {
			return category
		}
	}
	return FailureUnspecified
}
