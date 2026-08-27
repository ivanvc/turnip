// Package orchestrator wires Slices 1-5 into the complete
// webhook-to-operation flow: it implements github.EventHandler (reacting
// to pull_request and issue_comment webhooks) and rpc.OperationHandler
// (reacting to a Runner's log lines and final result), deciding when to
// call the primitives those and other slices already provide.
package orchestrator
