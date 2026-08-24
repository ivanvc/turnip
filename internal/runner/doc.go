// Package runner implements the Runner binary's logic: reading its
// configuration from the environment, cloning the target repository at the
// requested commit, dispatching to the matching Plugin, and reporting
// incremental progress and a final result back to the Server over gRPC,
// tolerating connection drops without aborting the Operation's subprocess.
package runner
