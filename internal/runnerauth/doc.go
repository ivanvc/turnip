// Package runnerauth answers one question for the Server's gRPC
// interceptor: may the holder of this token write to this Operation?
//
// It answers it in two steps that are easy to confuse and must both hold.
// Authentication establishes *which Pod* is calling, by asking the API
// server to validate a token minted for turnip's own audience. Binding
// establishes that that Pod is the one Kubernetes created for this
// Operation's Job. An implementation with only the first is the gap Slice
// 25 exists to close: every Runner holds a valid token, so any Runner
// could otherwise write to any Operation whose id it knew — and the id
// travels in every Job's environment.
package runnerauth
