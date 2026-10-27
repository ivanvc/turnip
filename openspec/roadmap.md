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

1. `fix-single-config-surface`: implemented, awaiting the pilot migration (guide in docs/internal)
2. `fix-unlock-without-config`: implemented, awaiting deploy (then unlock the blocked pilot pull request)
3. `fix-rendered-capitalization`: implemented, awaiting deploy
4. `runner-timeouts`: implemented, awaiting deploy (archive after `fix-rendered-capitalization` and `fix-single-config-surface`)
5. `fix-team-collaborators`: implemented, awaiting deploy
6. `fix-comment-part-split`: implemented, awaiting deploy

## Open changes

| Change | What it does | Depends on | Stage |
|---|---|---|---|
| [`runner-timeouts`](changes/runner-timeouts/) | an idle timeout, an absolute deadline, and a Runner that ended without a result is finalized | archive after `fix-rendered-capitalization`, `fix-single-config-surface` | implemented; archived once deployed, after its two dependencies |
| [`fix-plan-survives-push`](changes/fix-plan-survives-push/) | every push marks the pull request's ready plans stale, and says so | | proposal |
| [`fix-concurrent-mutations`](changes/fix-concurrent-mutations/) | a second mutating command for a Project is refused while the first runs | | proposal |
| [`fix-lock-release-on-close`](changes/fix-lock-release-on-close/) | closing a pull request releases every Lock it holds, whatever its configuration | `fix-unlock-without-config` (in part) | proposal |
| [`fix-invalid-config-check-on-comment`](changes/fix-invalid-config-check-on-comment/) | a comment that finds an invalid `turnip.yaml` marks the `turnip` check, as a push does | | proposal |
| [`helmfile-hook-policy`](changes/helmfile-hook-policy/) | an operator setting that refuses helmfiles declaring hooks | | proposal |
| [`operation-scheduling`](changes/operation-scheduling/) | a cap on how many Operations run at once, with a fair queue, and per-Project dependencies that order applies | `fix-concurrent-mutations`, `fix-invalid-config-check-on-comment`, `runner-timeouts` (in part) | proposal |
| [`runner-server-tls`](changes/runner-server-tls/) | TLS on the Runner's gRPC channel, and a Runner that verifies the Server; where the certificate comes from is still open | | proposal |
| [`org-images`](changes/org-images/) | the project under its organization, and multi-architecture images published on every push to `main` | | proposal |
| [`fix-comment-part-split`](changes/fix-comment-part-split/) | an output part ends at a whole line, never inside a line or a character | | implemented; archived once deployed |
| [`fix-single-config-surface`](changes/fix-single-config-surface/) | settings in one ConfigMap, secret material in one Secret, no Deployment patches, under Kustomize and Helmfile alike | | implemented; archived once the pilot runs on one ConfigMap and one Secret |
| [`fix-unlock-without-config`](changes/fix-unlock-without-config/) | `unlock` works whatever the configuration says | | implemented; archived once the blocked pull request unlocks |
| [`fix-rendered-capitalization`](changes/fix-rendered-capitalization/) | rendered text writes project, lock and operation in lowercase mid-sentence | | implemented; archived once deployed |
| [`fix-team-collaborators`](changes/fix-team-collaborators/) | an author with access through a team may trigger commands; a failed permission check says so | | implemented; archived once the refused author's diff runs |

Changes that overlap without depending on each other are best done
one at a time: `fix-plan-survives-push`, `fix-lock-release-on-close` and
`fix-concurrent-mutations` all change the Lock's transitions; and
`org-images`, which changes every Go file, when nothing else is in
flight. Each proposal's Dependencies
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

- A minimum role for mutating commands: an operator setting (for
  example `TURNIP_MUTATION_MIN_ROLE`) that can only raise the role an
  `apply` or `sync` needs above write, to maintain or admin, beside
  `TURNIP_MUTATION_REQUIREMENTS` (raised 2026-10-15). Plans stay as
  they are, and the setting does not depend on the repository's
  visibility: the archived `collaborator-authorization` change declined
  configurable levels on 2026-09-21 because a plan threshold takes the
  read-level reviewer's only tool and an apply threshold has no safe
  value below write; raising it has no unsafe value
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
- A Helm chart whose values file is turnip's configuration surface:
  decided against until turnip has a beta, since releasing a chart
  adds work (2026-10-19, raised by `fix-single-config-surface`). The
  intended way, when it comes (2026-10-20): generate the chart in CI
  from the rendered Kustomize base, with a converter such as helmify,
  and publish it to an OCI registry with each release, so the base
  stays the one source and the chart is a by-product. Still to check:
  how the converter maps `turnip-server-config`'s data and the
  `turnip-server-secrets` Secret into values
- The Runner image following the Server's version, so a deployment
  states turnip's version once rather than in the Server image and
  `TURNIP_RUNNER_IMAGE` (raised 2026-10-19)
- A tool's trigger word narrows `unlock` to that tool's Locks again,
  by recording each Lock's tool when its plan takes it; until then
  `/<tool> unlock` acts as `/turnip unlock` (decided 2026-10-20 in
  `fix-unlock-without-config`). Needed once a second tool's Plugin
  exists
- The image-policy refusal's example names the Project's tool rather
  than the placeholder `<tool>:` (found 2026-10-20)
- A `turnip.yaml` override of the Runner's idle timeout and deadline,
  a `runner:` field at both levels; left out of `runner-timeouts`
  (2026-10-24) since it lets a pull request hold a node longer and
  nothing needs it yet
- A line in the output warning before the idle timeout stops a tool
  ("no output for N minutes"); worth it once the output can be watched
  live (`live-operation-output`), left out of `runner-timeouts`
  (2026-10-24)
- The operator docs warn that separating the Runner namespace reaches
  the cloud identity bindings: an EKS Pod Identity association and any
  `kubernetes-namespace` condition in its role's trust policy, GKE
  Workload Identity, Azure federated credentials, IRSA's `sub` claim
  (found 2026-10-19 at the pilot's namespace move)
