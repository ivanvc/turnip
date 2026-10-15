# Roadmap

For whoever decides what turnip works on next. This page orders the
open work and nothing more: each item's detail lives in its OpenSpec
change under [`changes/`](changes/), and finished work under
[`changes/archive/`](changes/archive/).

The platform's original spec (the north-star requirements and design)
and the full roadmap it was delivered by (every slice's analysis and
decisions, and the unsliced backlog) are archived unchanged in
[`changes/archive/2026-10-09-multi-iac-automation-platform/`](changes/archive/2026-10-09-multi-iac-automation-platform/).
Work is named by the change it is, or will become. A slice number on
this page points to that slice's write-up in the archived `roadmap.md`;
new work gets a name, never a number.

## Next

In the order discussed so far; reorder here.

1. `fix-comment-part-split`: proposal
2. `fix-helmfile-diff-removals`: proposal
3. `fix-apply-suggested-with-lock`: proposal

## Open changes

| Change | What it does | Depends on | Stage |
|---|---|---|---|
| [`runner-timeouts`](changes/runner-timeouts/) | an idle timeout, an absolute deadline, and a Runner that ended without a result is finalized | | proposal |
| [`fix-plan-survives-push`](changes/fix-plan-survives-push/) | every push marks the pull request's ready plans stale, and says so | | proposal |
| [`fix-concurrent-mutations`](changes/fix-concurrent-mutations/) | a second mutating command for a Project is refused while the first runs | | proposal |
| [`fix-lock-release-on-close`](changes/fix-lock-release-on-close/) | closing a pull request releases every Lock it holds, whatever its configuration | | proposal |
| [`fix-invalid-config-check-on-comment`](changes/fix-invalid-config-check-on-comment/) | a comment that finds an invalid `turnip.yaml` marks the `turnip` check, as a push does | | proposal |
| [`helmfile-hook-policy`](changes/helmfile-hook-policy/) | an operator setting that refuses helmfiles declaring hooks | | proposal |
| [`split-server-runner-namespaces`](changes/split-server-runner-namespaces/) | the Server and its Secret in one namespace, Runner Jobs in another | | proposal |
| [`operation-scheduling`](changes/operation-scheduling/) | a cap on how many Operations run at once, with a fair queue, and per-Project dependencies that order applies | `fix-concurrent-mutations`, `fix-invalid-config-check-on-comment`, `runner-timeouts` (in part) | proposal |
| [`runner-server-tls`](changes/runner-server-tls/) | TLS on the Runner's gRPC channel, and a Runner that verifies the Server; where the certificate comes from is still open | | proposal |
| [`org-images`](changes/org-images/) | the project under its organization, and multi-architecture images published on every push to `main` | | proposal |
| [`fix-comment-part-split`](changes/fix-comment-part-split/) | an output part ends at a whole line, never inside a line or a character | | proposal |
| [`fix-helmfile-diff-removals`](changes/fix-helmfile-diff-removals/) | a Helmfile plan counts the releases it will remove | | proposal |
| [`fix-apply-suggested-with-lock`](changes/fix-apply-suggested-with-lock/) | a plan whose Lock is held offers the apply; an apply never offers itself again | | proposal |

Changes that overlap without depending on each other are best done
one at a time: `fix-plan-survives-push`, `fix-lock-release-on-close` and
`fix-concurrent-mutations` all change the Lock's transitions;
`split-server-runner-namespaces` before `runner-server-tls`, which has to
reach the Runner's namespace; and `org-images`, which changes every Go
file, when nothing else is in flight. Each proposal's Dependencies
section says why.

## Planned work without a change yet

Each becomes an OpenSpec change, under the name given here, when its
work starts. Dependencies list only open work; everything else they
depend on is complete.

| Change to be | Goal | Depends on | Notes |
|---|---|---|---|
| `plugins-terraform-pulumi` | the remaining IaC tools | | archived Slice 7 |
| `refuse-executable-arguments` | a comment's trailing arguments cannot choose the binary the Runner runs | | archived Slice 22 |
| `live-operation-output` | watch a plan or apply while it runs, from its first line, however late the viewer arrives | | archived Slice 26; the full log, never capped: every line in a Redis stream per Operation, purged by a TTL (supersedes the archived entry's capped stream, 2026-10-09); the Runner already streams every line and `fix-output-order` keeps it lossless; the replay after a broken stream (today the last 256 KiB) must resume from the last line stored |
| `stored-operation-output` | an operator opts into blob storage (S3, a GCS bucket, Azure Blob), and a finished Operation's log is flushed there at once, kept by the bucket's own lifecycle rules, and linked from the comment through turnip's UI | `live-operation-output` | written by the Server, never the Runner, which runs pull request code; who may read it is the same question `live-operation-output` raises |
| `lock-overview` | every held Lock on one page, releasable from there | `fix-lock-release-on-close` | archived Slice 28 |
| `runner-pod-placement` | node selectors and tolerations for Runner Pods | | archived Slice 31 |
| `runner-pod-resources` | requests equal to limits for every Runner Pod | | archived Slice 39 |
| `runner-pod-disruption` | nothing evicts a Runner partway through | `runner-pod-resources`, `runner-timeouts` | archived Slice 41 |
| `runner-workspace-volume` | an ephemeral volume for a heavy workspace | `runner-pod-resources` | archived Slice 42 |
| `plugin-kustomize` | Kustomize overlays planned on the pull request and applied by comment | | archived Slice 46 |

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
  only that it is set; `refuse-executable-arguments` needs the same check
- Azure Workload Identity needs Runner Pod labels: the Job has labels,
  its Pod template has none
- Runner Pod affinity: after `runner-pod-placement`
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
