# Tasks

**The checkpoint that matters is task 4.1.** The unit tests drive
`handleUnlock` and the comment path with a fake lock manager, which
can agree with code that the real one would contradict: how `SCAN`
patterns match a key, what a released or another pull request's lock
looks like in Redis. Only a comment taken through `HandleIssueComment`,
against the Redis lock manager on miniredis and a pull request whose
`turnip.yaml` is invalid, shows the pilot's case fixed end to end. It
uses the orchestrator's harness, so it needs no GitHub.

## 1. Listing a repository's locks (`internal/lock`)

- [x] 1.1 `ListedLock` and `LockManager.ListLocks(ctx, keyPrefix)` as
  design Decision 1 states them; the Redis implementation iterates
  `SCAN` with `lock:<keyPrefix>*` (glob characters in the prefix
  escaped), reads matches in batches with `MGET`, decodes each as
  `GetLockStatus` does, and skips a key gone between the two. The
  orchestrator's test doubles (`fakeLockManager`, `gateLocks`) gain
  `ListLocks`, matching keys by prefix. Verify: `internal/lock` tests on
  miniredis return the locks under `owner/api/` with their status, not
  `owner/api-v2/`'s, and do return a project named `gcp/web`; a key
  deleted after the scan is skipped; a failing read fails the call;
  `go build ./...`
  - _Deltas: commands (Unlock command)_

## 2. Unlock without the configuration (`internal/orchestrator`)

- [x] 2.1 `handleUnlock` takes no configuration: the write check first
  (design Decision 3); a named unlock reads each distinct named lock
  with `GetLockStatus` and releases only once every read succeeded; a
  bare unlock lists `<owner>/<repo>/` with `ListLocks` and keeps the
  locks this pull request holds (Decision 2); each release goes
  through `Apply(EventUnlocked)` as today, `ErrLockedByOtherPR`
  reported as held elsewhere. The trigger word is ignored, so
  `resolveUnlockCandidates` and its tests go (Decision 4). Replies:
  `No projects matched this unlock command.` and the could-not-read
  reply. Verify: a test per scenario of the `commands` delta (bare,
  held elsewhere, nothing to unlock, `*`, other pull requests' locks
  left out, a project the configuration no longer declares, a tool's
  trigger word, a failed `ListLocks`, a failed `GetLockStatus` among
  several names releasing nothing); the existing unlock tests updated
  for the lowercase reply
  - _Deltas: commands (Unlock command)_
- [x] 2.2 `HandleIssueComment` fetches the configuration only when the
  comment holds a command other than `unlock`; when it is not usable,
  the loop still runs, every other command is skipped, the
  configuration reply takes the place of the first skipped command's
  replies, and nothing is dispatched (design "The comment path").
  Verify: an `unlock`-only comment makes no configuration request; an
  invalid configuration with `unlock` and `diff` posts the unlock reply
  and one configuration reply, in command order, and starts no
  trigger; a missing configuration with only `unlock` posts no
  missing-configuration warning; a malformed line and an invalid
  configuration post both replies; `unlock` on a fork's pull request
  posts nothing, and on a closed one the closed reply
  - _Deltas: configuration (Replies for a missing, invalid or
    unreadable configuration), pull-request-lifecycle (Guard order)_

## 3. Documentation

- [x] 3.1 `docs/using/commands.md` ("Releasing a lock manually"): unlock
  works whatever the pull request's configuration says; a bare unlock
  releases this pull request's locks; names are compared as written; a
  tool's trigger word acts as `/turnip`. `docs/using/troubleshooting.md`
  says, where it covers an invalid configuration, that `/turnip unlock`
  still works. Verify: `make docs-check`
  - _Deltas: commands (Unlock command), configuration (Replies for a
    missing, invalid or unreadable configuration)_

## 4. Checkpoint: the pilot's case through the Server

- [x] 4.1 A test taking one comment through `HandleIssueComment` with
  the Redis lock manager on miniredis: this pull request holds `web`,
  another holds `api`, and the head commit's `turnip.yaml` names an
  image the Server does not allow. `/turnip unlock` releases `web` in
  Redis, leaves `api`, replies `Unlocked:` listing `web` only, and
  posts no configuration reply; `/helmfile unlock web` behaves the same
  for `web`

## 5. Final checkpoint

- [x] 5.1 `go build ./...`, `gofmt -l ./internal ./cmd` clean,
  `go vet ./...`, `make test`, `make lint` pass
- [x] 5.2 Every scenario in the deltas maps to a test or a doc change;
  `openspec validate fix-unlock-without-config --strict` passes;
  `openspec/roadmap.md` shows the change as implemented
- [x] 5.3 An intermittent failure is recorded here with its full output,
  not rerun away

## Repository owner

- Review and commit
- Deploy a Server built from this change (the Runner is unchanged)
- On the pilot pull request whose configuration is invalid, comment
  `/turnip unlock`; once its lock is released, archive this change

## Task Dependency Graph

```json
{
  "waves": [
    { "id": 0, "tasks": ["1.1", "3.1"] },
    { "id": 1, "tasks": ["2.1"] },
    { "id": 2, "tasks": ["2.2"] },
    { "id": 3, "tasks": ["4.1"] },
    { "id": 4, "tasks": ["5.1", "5.2", "5.3"] }
  ]
}
```

## Deviations recorded at implementation

- 2026-10-20: Task 1.1. `gateLocks` needed no edit: it embeds
  `fakeLockManager`, so `ListLocks` is promoted. `fakeLockManager`
  gained an optional `listLocksFunc` for tests that script the listing.
  `SCAN` results are deduplicated, since `SCAN` may return a key twice.
- 2026-10-20: Task 2.1. A named lock held by another pull request is
  reported from the status read beforehand, without calling `Apply`;
  `Apply` runs only for locks this pull request holds, and still
  handles `ErrLockedByOtherPR` for the race. `toolCandidates` and
  `narrowByName` stay, used by selection for other commands.
- 2026-10-20: Task 2.2. Building the selection moved into
  `commentSelection`, since it exists only when the configuration is
  usable. Two consequences of posting every reply from one list, both
  following from the design: a failure to post the configuration reply
  is now logged and the handler returns nil, where it used to return
  the post error; and a comment holding only malformed lines no longer
  fetches the configuration, so it gets only the malformed-line reply.
- 2026-10-20: Task 4.1. The test first assumed a released lock reads
  back as nil; `GetLockStatus` returns a status with `Locked` false, and
  the assertion was corrected. No production change.
