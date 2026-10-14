# Roadmap

For whoever decides what turnip works on next. This page orders the
open work and nothing more: each item's detail lives in its OpenSpec
change under [`changes/`](changes/), and finished work under
[`changes/archive/`](changes/archive/).

The platform's original spec (the north-star requirements and design)
and the full roadmap it was delivered by (every slice's analysis and
decisions, and the unsliced backlog) are archived unchanged in
[`changes/archive/2026-10-09-multi-iac-automation-platform/`](changes/archive/2026-10-09-multi-iac-automation-platform/).
Slice numbers on this page refer to its `roadmap.md`.

## Next

In the order discussed so far; reorder here.

1. Invalid UTF-8 in output, not yet a change: a byte the protobuf string
   fields refuse makes every delivery attempt fail, so the Runner
   retries for 15 minutes and the check stays in progress (found by the
   review of `fix-large-output`)
2. `fix-results-comment-on-replica-loss`: implemented, awaiting deploy
3. Comment parts split mid-line, not yet a change: a Project's output
   too long for one comment is split into `output part N/M` pieces at an
   exact byte count, so a line (or a multi-byte character) can be cut
   across two comments. Parts should end at the last whole line that
   fits, splitting inside a line, at a character boundary, only for a
   line longer than a part (found 2026-10-09; present since the first
   comment code)

## Open changes

| Change | What it does | Stage |
|---|---|---|
| [`fix-results-comment-on-replica-loss`](changes/fix-results-comment-on-replica-loss/) | a trigger's results comment and later commands survive the loss of the replica that dispatched them | implemented; archived once a self-upgrading apply posts its comment |
| [`runner-timeouts`](changes/runner-timeouts/) | an idle timeout, an absolute deadline, and a Runner that ended without a result is finalized (Slice 40) | proposal |
| [`fix-plan-survives-push`](changes/fix-plan-survives-push/) | every push marks the pull request's ready plans stale, and says so | proposal |
| [`fix-concurrent-mutations`](changes/fix-concurrent-mutations/) | a second mutating command for a Project is refused while the first runs | proposal |
| [`fix-lock-release-on-close`](changes/fix-lock-release-on-close/) | closing a pull request releases every Lock it holds, whatever its configuration | proposal |
| [`fix-invalid-config-check-on-comment`](changes/fix-invalid-config-check-on-comment/) | a comment that finds an invalid `turnip.yaml` marks the `turnip` check, as a push does | proposal |
| [`helmfile-hook-policy`](changes/helmfile-hook-policy/) | an operator setting that refuses helmfiles declaring hooks | proposal |
| [`split-server-runner-namespaces`](changes/split-server-runner-namespaces/) | the Server and its Secret in one namespace, Runner Jobs in another | proposal |
| [`operation-scheduling`](changes/operation-scheduling/) | a cap on how many Operations run at once, with a fair queue, and per-Project dependencies that order applies (Slice 30) | proposal |
| [`runner-server-tls`](changes/runner-server-tls/) | TLS on the Runner's gRPC channel, and a Runner that verifies the Server (Slice 38); where the certificate comes from is still open | proposal |
| [`org-images`](changes/org-images/) | the project under its organization, and multi-architecture images published on every push to `main` (Slice 49) | proposal |

## Planned slices without a change yet

Each becomes an OpenSpec change when its work starts. Dependencies list
only open work; every other slice they depend on is complete.

| Slice | Goal | Depends on | Notes |
|---|---|---|---|
| 7: Terraform and Pulumi plugins | the remaining IaC tools | | |
| 22: Refuse tool arguments that name an executable | a comment's trailing arguments cannot choose the binary the Runner runs | | |
| 26: Real-time operation output | watch a plan or apply while it runs, from its first line, however late the viewer arrives | | the full log, never capped: every line in a Redis stream per Operation, purged by a TTL (supersedes the archived entry's capped stream, 2026-10-09); the Runner already streams every line and `fix-output-order` keeps it lossless; the replay after a broken stream (today the last 256 KiB) must resume from the last line stored |
| new: Long-term operation output | an operator opts into blob storage (S3, a GCS bucket, Azure Blob), and a finished Operation's log is flushed there at once, kept by the bucket's own lifecycle rules, and linked from the comment through turnip's UI | 26 | written by the Server, never the Runner, which runs pull request code; who may read it is the same question 26 raises |
| 28: Seeing and dropping Locks | every held Lock on one page, releasable from there | | |
| 31: Runner Pod placement | node selectors and tolerations for Runner Pods | | |
| 39: Runner Pod resources | requests equal to limits for every Runner Pod | | |
| 41: Runner Pods run without disruption | nothing evicts a Runner partway through | 39, `runner-timeouts` | |
| 42: The workspace on a per-Runner volume | an ephemeral volume for a heavy workspace | 39 | |
| 46: Kustomize plugin | Kustomize overlays planned on the pull request and applied by comment | | |

## Backlog

Ideas not yet sliced, each written up under "Backlog (not yet sliced)"
in the archived roadmap. Each is still to be triaged: made a change,
kept as an idea, or dropped. Where the archived write-up is out of
date, the entry says what has changed.

Reviewed against the code and the specs 2026-10-12. Entries of the
archived Backlog not listed here were closed then: done (a tool build
turnip does not ship, a stated security model, a floating tool version,
a top-level `runner:` block), covered by an open change (a Runner that
dies after starting: `runner-timeouts`; resuming a scheduled Run:
`operation-scheduling`), or decided against (verifying a Helmfile plan
is still fresh at apply).

- Matching Projects by directory rather than name: selectors still
  match Project names only
- Contain `Project.directory` to the workspace: validation still checks
  only that it is set; Slice 22 needs the same check
- Azure Workload Identity needs Runner Pod labels: the Job has labels,
  its Pod template has none
- Runner Pod affinity: after Slice 31
- `whenModified` cannot see through a symlink: the workaround (name the
  link's target too) is documented; resolving links automatically is
  what remains
- Expose the workspace path as a variable rather than a literal: the
  Runner sets `TURNIP_WORKSPACE_DIR` and the tool inherits it, but a
  `runner.env` value cannot refer to it and the documentation gives the
  literal path
- Per-repository server-side configuration: every Server setting is
  still global; the repository's Secret is the only per-repository
  thing
- Authenticating to a cluster turnip is not running in: documented for
  EKS (a kubeconfig with an `exec` stanza, an image that carries `aws`,
  the repository's Secret). GKE and AKS are expected to work the same
  way and are not verified: there is no cluster of either to test
  against
- A failed Operation's output loses the transcript's highlighting: the
  spec and the code now agree that a failed output sits in an untagged
  fence; whether turnip's own lines should be highlighted there is
  still undecided, and matters more since `fix-output-order` put an
  omission line in a failed Helmfile output
- An intermittent failure in the orchestrator's property tests: its
  suspected cause (the test fake asserting on a publish from a
  goroutine) went away with `fix-results-comment-on-replica-loss`,
  which removed the publish; drop this entry if no sighting follows
- The tests that need a real Redis do not run in CI: `ha_test.go` is
  skipped unless `TURNIP_TEST_REDIS_ADDR` is set, which let its fakes
  rot unnoticed until 2026-10-13
