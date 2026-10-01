// Package runnerauth answers one question for the Server's gRPC
// interceptor: may the holder of this Runner identity act for this
// Operation, and which Runner identity is it?
//
// It answers it in two steps that are easy to confuse and must both hold.
// Authentication establishes *which Pod* is calling, by asking the API
// server to validate a Runner identity minted for one of turnip's two
// audiences (jobs.CloneAudience, jobs.ReportAudience). The audience the
// review reports also decides which Runner identity the caller holds: the
// clone identity, which may fetch a GitHub credential, or the report
// identity, which may only report. Binding establishes that that Pod is
// the one Kubernetes created for this Operation's Job. An implementation
// with only the first is the gap Slice 25 exists to close: every Runner
// holds a valid Runner identity, so any Runner could otherwise write to
// any Operation whose id it knew, and the id travels in every Job's
// environment.
package runnerauth
