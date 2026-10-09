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

1. `fix-output-order`: proposal under review
2. Large output, not yet a change: a result over gRPC's 4 MiB message
   limit never reaches the Server (the Runner retries for 15 minutes and
   the check stays in progress), the Runner's result log line repeats the
   whole output, and the results comment splits without bound. It needs
   a change of its own, since `fix-check-output-size` shipped without it
3. `fix-check-output-size`: implemented, awaiting deploy
4. `fix-results-comment-on-replica-loss`: design next

## Open changes

| Change | What it does | Stage |
|---|---|---|
| [`redis-tls-auth`](changes/redis-tls-auth/) | TLS and AUTH to Redis (Slice 27) | implemented; archived once a deployment reports `tls=true` |
| [`fix-results-comment-on-replica-loss`](changes/fix-results-comment-on-replica-loss/) | a trigger's results comment and later commands survive the loss of the replica that dispatched them | specs |
| [`fix-output-order`](changes/fix-output-order/) | output in write order; each plugin decides what its output shows | proposal |
| [`fix-check-output-size`](changes/fix-check-output-size/) | a check no longer carries the output as its details text | implemented; archived once a large plan completes its check |
| [`runner-timeouts`](changes/runner-timeouts/) | an idle timeout, an absolute deadline, and a Runner that ended without a result is finalized (Slice 40) | proposal |
| [`fix-plan-survives-push`](changes/fix-plan-survives-push/) | every push marks the pull request's ready plans stale, and says so | proposal |
| [`fix-concurrent-mutations`](changes/fix-concurrent-mutations/) | a second mutating command for a Project is refused while the first runs | proposal |
| [`fix-lock-release-on-close`](changes/fix-lock-release-on-close/) | closing a pull request releases every Lock it holds, whatever its configuration | proposal |
| [`fix-invalid-config-check-on-comment`](changes/fix-invalid-config-check-on-comment/) | a comment that finds an invalid `turnip.yaml` marks the `turnip` check, as a push does | proposal |
| [`helmfile-hook-policy`](changes/helmfile-hook-policy/) | an operator setting that refuses helmfiles declaring hooks | proposal |
| [`split-server-runner-namespaces`](changes/split-server-runner-namespaces/) | the Server and its Secret in one namespace, Runner Jobs in another | proposal |

## Planned slices without a change yet

Each becomes an OpenSpec change when its work starts. Dependencies list
only open work; every other slice they depend on is complete.

| Slice | Goal | Depends on | Notes |
|---|---|---|---|
| 7: Terraform and Pulumi plugins | the remaining IaC tools | | |
| 22: Refuse tool arguments that name an executable | a comment's trailing arguments cannot choose the binary the Runner runs | | |
| 26: Real-time operation output | watch a long plan or apply while it runs | | the Runner already streams every line, and `fix-output-order` keeps it streaming; the large-output fix may need its durable copy of the full output |
| 28: Seeing and dropping Locks | every held Lock on one page, releasable from there | | |
| 30: Scheduling: concurrency and dependencies | a repository controls how many Operations run at once and in what order | | requirements drafted in `.kiro/specs/operation-scheduling/` |
| 31: Runner Pod placement | node selectors and tolerations for Runner Pods | | |
| 38: Encrypting the Runner-to-Server channel | TLS on the Runner's gRPC channel, and a Runner that verifies the Server | | requirements drafted in `.kiro/specs/runner-server-tls/` |
| 39: Runner Pod resources | requests equal to limits for every Runner Pod | | |
| 41: Runner Pods run without disruption | nothing evicts a Runner partway through | 39, `runner-timeouts` | |
| 42: The workspace on a per-Runner volume | an ephemeral volume for a heavy workspace | 39 | |
| 46: Kustomize plugin | Kustomize overlays planned on the pull request and applied by comment | | |
| 49: Published images under the project's organization | multi-architecture images on every push to `main` | | requirements drafted in `.kiro/specs/org-images/` |

## Backlog

Ideas not yet sliced, each written up under "Backlog (not yet sliced)"
in the archived roadmap. Each is still to be triaged: made a change,
kept as an idea, or dropped. Where later work touched one, it says so.

- Matching Projects by directory rather than name
- Contain `Project.directory` to the workspace
- Running a tool build turnip does not ship
- A stated security model, so opt-in danger is not reported as a
  vulnerability: `SECURITY.md`'s threat model may already cover it
- Azure Workload Identity needs Runner Pod labels
- Runner Pod affinity
- `whenModified` cannot see through a symlink
- Expose the workspace path as a variable rather than a literal
- A Helmfile apply does not verify its plan is still fresh (accepted)
- Resolving a floating tool version
- Per-repository server-side configuration
- A top-level `runner:` block, merged into each Project's: the
  two-level `runner:` settings may already cover it
- Authenticating to a cluster turnip is not running in: partly covered
  by the repository's Secret (`repository-secrets`, archived 2026-10-08)
- A failed Operation's output loses the transcript's highlighting
- Resuming a scheduled Run after a Server restart: related to the
  resume in `fix-results-comment-on-replica-loss`
- A Runner that dies after starting leaves its check in progress:
  `runner-timeouts`
- An intermittent failure in the orchestrator's property tests
