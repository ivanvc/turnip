# Requirements Document: gRPC & Runner (Slice 5)

## Introduction

This slice delivers the gRPC contract between Server and Runner, the
Runner process itself (repository cloning, Plugin dispatch, progress/result
reporting), and the Server-side primitives for launching and cleaning up
Runner pods as Kubernetes Jobs — including per-tool binary provisioning via
initContainers. It is consumed by Slice 6 (Server Orchestration), which
decides *when* to create a Runner Job for a given Project/Operation and
what to do with the result (acquire/release locks, post GitHub comments and
check runs) — this slice only implements the mechanism.

This slice implements Requirements 8 and 14 from the global spec
(`.kiro/specs/multi-iac-automation-platform/requirements.md`), plus the
tool-version-resolution sub-clauses of Requirement 18 (18.6-18.8) that
Requirement 14.2a depends on. It builds on Slice 2's `Plugin` interface
(the Runner loads and executes a Plugin) and Slice 1's `Project`/`Config`
types (the Job builder reads a matched Project's directory, tool, and
config map) — it does not depend on Slice 3 (locking), Slice 4 (GitHub
client), or their data.

One requirement in this set does not read as internally consistent, and
needs to be resolved as part of this slice rather than carried forward
as-is:

- **Requirement 8.5** ("THE Server SHALL stream operation logs back to the
  Runner via gRPC response streaming") is inverted from what the rest of
  Requirement 8 and the Runner's stated responsibilities require. Requirement
  8.3/8.4 establish the Runner as the one that connects to the Server and
  initiates the call; only the Runner has the cloned repository and the
  provisioned tool binary, so only the Runner can produce the log lines and
  final result an operation generates. A GitHub Actions runner or Buildkite
  agent-style architecture — an ephemeral worker that dials out to a
  stable coordinator and reports its own progress upward — is what the rest
  of this slice's requirements describe; Requirement 8.5's literal
  direction is backwards from that. This slice implements the behavior
  Requirement 8's user story and the Runner's responsibilities actually
  call for (the Runner's progress and result reach the Server over the
  connection Requirement 8.3 establishes), not Requirement 8.5's literal
  wording.

## Glossary

(Inherited from the global spec glossary.)

- **Runner Job**: The Kubernetes Job resource the Server creates to run one
  Operation; terminates and is deleted once the Operation completes.
- **Tool Binary**: The IaC_Tool's CLI executable (`terraform`, `pulumi`,
  `helmfile`), provisioned onto the Runner Job's `PATH` by a dedicated
  initContainer rather than baked into the Runner's own container image.

## Requirements

### Requirement 1: Protobuf Service Definition

**User Story:** As a platform developer, I want a versioned, generated gRPC
contract between Server and Runner, so that both sides share a type-safe,
efficient wire format.

#### Acceptance Criteria

1. THE protobuf schema SHALL define one RPC method carrying everything an
   Operation needs to execute: a Project's directory, tool, and
   tool-specific config; the Operation name; the repository URL and commit
   SHA; the installation token; any extra CLI arguments; and plan data (for
   an apply reusing a stored plan)
2. THE protobuf schema SHALL define message types for a single log line
   (with enough structure for a caller to distinguish informational output
   from errors) and for a final result (success/failure, combined output,
   exit code, an error message when applicable, a change summary, and plan
   data produced by a plan operation)
3. Generated Go code SHALL be produced via the existing `buf generate`
   pipeline (already wired in Slice 0) without requiring changes to that
   pipeline's configuration

### Requirement 2: Runner-to-Server Connection and Reconnection

**User Story:** As a platform operator, I want the Runner to find and
connect to the Server without hardcoding an address, and to weather a
Server restart or transient network blip without losing the Operation it's
running, so that a routine Server rollout doesn't strand every in-flight
plan/apply as a silent failure.

#### Acceptance Criteria

1. THE Runner SHALL read the Server's gRPC address from an environment
   variable at startup
2. THE Runner SHALL establish a gRPC connection to that address before
   attempting to execute an Operation, retrying with backoff rather than
   attempting to dial exactly once
3. IF the Runner cannot establish that initial connection within 2 minutes
   of retrying (the timeout the global design's error-handling notes
   already call for), THEN THE Runner SHALL exit with a non-zero status
   and a clear error message, rather than retrying indefinitely
4. IF the connection is lost after having been established — mid-Operation
   — THEN THE Runner SHALL attempt to reconnect using the same backoff
   policy as the initial connection, and SHALL NOT abort the Operation's
   locally-running subprocess merely because the connection dropped:
   killing a `terraform apply` mid-flight over a transient network blip
   risks leaving real infrastructure in an undocumented partial state,
   which is worse than a delayed report
5. A successful reconnection SHALL resume reporting for the same
   Operation (identified by the Operation ID Requirement 3 establishes),
   not begin a new one from the Server's point of view
6. Every retry loop this requirement describes (initial connection,
   reconnection) SHALL use jittered backoff, so a Server restart with many
   Runner Jobs reconnecting at once doesn't create a reconnection
   thundering herd against the freshly-restarted Server

### Requirement 3: Operation Execution Request

**User Story:** As a platform developer, I want the Runner to tell the
Server what it's about to do, so that the Server has a record of every
Operation a Runner Job ran.

#### Acceptance Criteria

1. THE Runner SHALL call the RPC defined by Requirement 1 exactly once per
   Runner Job, providing the Project, Operation, repository, and
   authentication details it was started with
2. THE Server's handling of that call SHALL NOT block the Runner from
   proceeding to execute the Operation — the Runner is the one doing the
   work, not waiting for the Server to do it

### Requirement 4: Operation Progress and Result Reporting

**User Story:** As a developer watching an Operation run, I want to see
its output as it happens and a clear final result, so that I'm not left
guessing whether a long-running plan or apply is still working — and I
want that final result to survive a brief Server hiccup rather than
vanishing along with it.

#### Acceptance Criteria

1. AS the Runner executes an Operation, its output SHALL reach the Server
   incrementally (as discrete log lines), not only after the entire
   Operation finishes
2. WHEN the Operation completes, THE Runner SHALL report a final result to
   the Server containing success/failure, combined output, exit code, and
   — for a plan operation — the plan data and change summary
3. Log lines produced while the connection is unavailable (Requirement
   2.4) SHALL be buffered locally and sent once the connection is
   reestablished, up to a bounded buffer size; IF that buffer is
   exceeded, THEN THE Runner SHALL drop the oldest buffered lines and
   record that lines were dropped, rather than growing memory without
   bound or blocking the Operation's own execution to wait for space
4. THE Runner SHALL retry delivering the final result more persistently
   than it retries any individual log line: the final result is what
   Slice 6 depends on for correctness (releasing a lock, keeping plan and
   apply consistent), whereas a dropped log line is only a minor
   observability gap
5. IF the Runner exhausts its retry budget for delivering the final
   result, THEN THE Runner SHALL log that result to its own process
   output before exiting non-zero, so the outcome isn't silently lost
   even though no Server ever acknowledged it
6. IF the gRPC connection is lost mid-Operation and every reconnection
   attempt (Requirement 2.4) fails, THEN THE Runner SHALL surface that as
   a distinct failure mode from the Operation's own tool exiting with an
   error — a caller needs to be able to tell "the plan failed" apart from
   "the plan may have succeeded or failed, but we never found out"
7. Independent of whether the Server is reachable at all, every line of
   output the Operation produces SHALL also be written immediately to the
   Runner's own process output (stdout/stderr, matching that line's
   originating stream) as it's produced — not buffered and only shown at
   exit — so a `kubectl logs -f` on a Runner Pod shows live progress
   throughout the Operation, whether or not Requirement 2.4's reconnection
   logic is actively retrying, has given up, or was never needed at all

### Requirement 5: Repository Cloning

**User Story:** As a developer, I want the Runner to check out the exact
commit my PR is at, so that plan/apply output reflects what I'm actually
reviewing.

#### Acceptance Criteria

1. THE Runner SHALL clone the repository at the commit SHA it was given
   before invoking any Plugin operation
2. THE Runner SHALL use the installation token it was given to
   authenticate the clone against a private repository
3. IF cloning fails (invalid token, network error, missing commit), THEN
   THE Runner SHALL report that failure as the Operation's result rather
   than crashing without a reported outcome

### Requirement 6: Plugin Dispatch

**User Story:** As a platform developer, I want the Runner to run the
right tool for a Project without the Server needing to know tool-specific
details, so that adding a new IaC tool doesn't require Server changes.

#### Acceptance Criteria

1. THE Runner SHALL select the Plugin matching the tool it was told to run
2. THE Runner SHALL invoke that Plugin's `Execute` with the Project's
   working directory, tool-specific config, extra arguments, and — for an
   apply — the plan data it was given
3. THE Runner SHALL translate the Plugin's `ExecuteResult` into the final
   result Requirement 4.2 describes reporting to the Server

### Requirement 7: Kubernetes Job Creation

**User Story:** As a platform operator, I want each Operation to run in
its own clean Kubernetes Job, so that Operations can't interfere with each
other and resources are naturally bounded.

#### Acceptance Criteria

1. THE Server SHALL provide a way to create a Kubernetes Job for a given
   Project, Operation, repository/commit, and installation token
2. THE created Job's Runner container SHALL receive the repository URL,
   commit SHA, Project directory, Operation, and the Server's gRPC address
   as environment variables
3. THE created Job SHALL NOT require the Runner's own container image to
   contain any IaC tool binary
4. THE created Job SHALL set a `ttlSecondsAfterFinished` bounding how long
   Kubernetes keeps it (and its Pod) around after it finishes — Requirement
   9 covers why this, not an explicit Server-issued delete, is the primary
   cleanup mechanism

### Requirement 8: Per-Tool Binary Provisioning

**User Story:** As a platform operator, I want each Runner Job to get the
exact tool version a Project asks for, so that turnip never lags behind a
tool's latest release the way a bundled-binary approach would.

#### Acceptance Criteria

1. THE Job builder SHALL add one initContainer per Runner Job, using the
   matched Project's IaC_Tool's vendor-published image tagged with the
   resolved version, that copies that tool's CLI binary onto a volume the
   Runner's main container also mounts
2. THE Runner's main container SHALL be able to invoke the tool binary
   from that shared volume without any additional configuration
3. WHERE a Project's config specifies a `version`, THE Job builder SHALL
   validate it against a known-good list for that Project's tool before
   creating the Job
4. IF a specified `version` is not recognized for that tool, THEN THE Job
   builder SHALL return an error and SHALL NOT create the Job
5. IF a Project's config specifies no `version`, THEN THE Job builder
   SHALL use a documented default version for that Project's tool

### Requirement 9: Job Cleanup

**User Story:** As a platform operator, I want completed Runner Jobs
removed without the Server having to remember to do it, so that a Server
crash or a forgotten cleanup call doesn't leave the cluster accumulating
terminated pods forever.

#### Acceptance Criteria

1. Kubernetes' own `ttlSecondsAfterFinished` (Requirement 7.4) SHALL be
   the primary cleanup mechanism, not a Server-issued delete the Server
   has to remember to make: that's the whole point of expressing a Runner
   Job as a Job resource rather than a bare Pod the Server would have to
   track and reap itself
2. THE Server MAY ALSO provide a way to delete a Runner Job immediately
   (before its TTL expires) — e.g. to reclaim resources right away once a
   result has been received and processed successfully — but this is a
   supplementary, eager path, not the path anything else in this slice
   depends on for correctness
3. Deleting a Runner Job, whether via the TTL or an explicit delete, SHALL
   also remove its terminated Pod(s), not just the Job resource itself
4. Because a Job isn't "finished" — and so its TTL doesn't start counting
   down — until the Runner's own container process exits, and because the
   Pod's logs (Requirement 4.5's last-resort audit trail if the Server
   never acknowledged a result) are gone once that Pod is cleaned up,
   cleanup timing is only safe because Requirement 4.4 already requires
   the Runner to exhaust its final-result retry budget *before* it exits.
   This requirement doesn't add a new obligation on top of that one — it
   depends on it: cleanup must never end up racing ahead of the Runner's
   own attempts to call back home, and tying cleanup to container exit
   (rather than, say, a fixed wall-clock delay from Job creation) is what
   guarantees that ordering for free

## Out of Scope

- Deciding *when* to create a Runner Job for a Project/Operation, and what
  to do with its reported result (acquire/release locks, post GitHub
  comments and check runs) — Slice 6 (Server Orchestration)
- Deciding *whether* to call Requirement 9.2's eager delete at all (e.g.
  only after a successfully-processed result, leaving failed Jobs for the
  TTL so an operator has a window to inspect them) — Slice 6's call; this
  slice only provides the primitive and the TTL safety net under it
- Deciding when a Runner Job has taken too long to start (Requirement
  14.5's 5-minute window) and reporting that failure via GitHub comment
  and check run — Slice 6's orchestration decision; this slice's Job
  creation primitive (Requirement 7) is what Slice 6 calls, and Slice 6 is
  responsible for timing out its own wait
- Generating the installation token this slice's Job/request carries —
  Slice 4 (`AppAuth.InstallationClient(...).GenerateInstallationToken`);
  this slice only accepts an already-generated token as an input
- Terraform and Pulumi Plugin implementations — Slice 7; this slice's
  Runner dispatches to whatever Plugin `internal/plugin` provides,
  currently only Helmfile (Slice 2)
- Matching a PR's modified files against Project `whenModified` rules —
  Slice 1; this slice receives an already-matched Project
- A Server-side HTTP or gRPC surface for anything other than the one RPC
  Requirement 1 defines — no other cross-process API is needed by this
  slice
- Treating a retried final-result delivery for the same Operation ID as
  idempotent on the Server side (not double-releasing a lock, not
  double-posting a comment) — Slice 6, since that's a decision about what
  the Server *does* with a result, and this slice doesn't implement that
  handler (see Requirement 1's Introduction note). This slice's
  responsibility is narrower: guaranteeing the Operation ID stays stable
  and identifiable across a Runner's own reconnect/retry attempts
  (Requirement 2.5), so that whichever handler Slice 6 provides *can*
  de-duplicate
- Retrying the Runner's clone of the repository (Requirement 5) on a
  transient GitHub-side network error — that's a Runner-to-GitHub
  communication path, not the Runner-to-Server one this slice's retry
  requirements (2, 4) are about; a clone failure is reported as-is per
  Requirement 5.3
