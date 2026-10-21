# Proposal

## Why

`/turnip unlock` is refused whenever the pull request's `turnip.yaml`
cannot be used. For every comment, the Server fetches and validates the
configuration at the head commit before it looks at which command was
asked for, so an invalid file (here, a Project naming an image the
Server's `TURNIP_ALLOWED_IMAGES` does not allow) answers every command,
`unlock` included, with "The turnip configuration is invalid, so no
operations ran."

`unlock` needs nothing from the configuration. It runs no tool, creates
no Job, and changes no infrastructure; it releases Locks this pull
request holds, and a Lock belongs to the pull request whatever its
configuration now says. It reads the configuration only to turn Project
names into Lock keys, and each key already carries the repository and
the Project name (`lock:<owner>/<repo>/<project>`). So the pull request
that most needs an unlock, one whose configuration change broke it, is
the one that cannot get one, and the only ways out are a commit that
restores a valid file or deleting the Lock in Redis by hand.

Found 2026-10-20 on the pilot, unlocking a plan in a pull request that
changed its Projects' images.

## What Changes

- `unlock` no longer reads `turnip.yaml`. Its candidates are the Locks
  of this repository, found in Redis: a bare `unlock` releases every
  Lock this pull request holds; a named one releases that Project's
  Lock when this pull request holds it
- A comment holding only `unlock` commands does not fetch the
  configuration at all. In a comment that also holds other commands,
  each `unlock` still runs, and the other commands get the
  configuration reply as today
- **BREAKING** (the reply only) A name with no Lock no longer gets
  `Project "<name>" not found in turnip.yaml.`, since turnip no longer
  knows which Projects the file declares; it goes unmentioned like any
  candidate with no Lock, so an unlock that releases and lists nothing
  replies `No projects matched this unlock command.` (lowercased from
  `No Projects ...` under `CLAUDE.md`'s rule for rendered text,
  2026-10-20)
- What does not change: unlock still needs write permission, is still
  refused on a pull request from a fork (checked before anything else,
  so a stranger's comment gets no signal) and on a closed or merged one
  (closing already released its Locks, and nothing can plan there),
  still releases from any state, and still never changes the `turnip`
  check

## Decisions (2026-10-20)

1. **The trigger word no longer narrows an unlock, for now.** A Lock
   does not record its Project's tool, so `/<tool> unlock` releases the
   same Locks as `/turnip unlock`. With Helmfile the only tool, the two
   select the same Projects today. Recording the tool in each Lock, so
   a tool's trigger word skips other tools' Locks again, waits until a
   second tool exists (backlog).

## Capabilities

### New Capabilities

### Modified Capabilities
- `commands`: the Unlock command finds its candidates from this
  repository's Locks rather than `turnip.yaml`, under any trigger word,
  and drops the not-found reply
- `pull-request-lifecycle`: the comment path's guard order fetches the
  configuration for every command other than `unlock`
- `configuration`: a comment holding only `unlock` commands does not
  read the configuration, and in a mixed comment the configuration
  reply answers the other commands only

## Impact

`internal/lock` (listing a repository's Locks), `internal/orchestrator`
(comment handling: `unlock` ahead of the configuration fetch;
`handleUnlock` and its target resolution), `docs/using/commands.md`.

Out of scope:
- Closing or merging a pull request still releases Locks by walking the
  head commit's configuration: `fix-lock-release-on-close`, which can
  reuse the listing this change builds
- The image-policy message showing `<tool>:` in its example rather than
  the Project's tool: backlog

## Dependencies

- **Overlaps `fix-lock-release-on-close`.** Both need to find a pull
  request's Locks without the configuration; this change builds that
  first, since the pilot is blocked on it, and the close change reuses
  it.
- **Overlaps `fix-invalid-config-check-on-comment`.** Both change how
  the comment path handles the configuration fetch; best done one at a
  time.
