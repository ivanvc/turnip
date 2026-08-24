// Package rpc hosts the Server side of the Runner-to-Server gRPC contract
// (proto/turnip/v1/operation.proto). It translates the generated,
// client-streaming ExecuteOperation RPC into plain calls on a
// caller-supplied OperationHandler and contains no business logic of its
// own — deciding what a log line or result means (locks, comments, check
// runs) belongs to Slice 6.
package rpc
