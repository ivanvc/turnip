# Design: Cloning Submodules (Slice 16)

## Overview

Submodule initialisation is added to the clone, after the merge, behind a
three-state mode with a Server default and a repository override.

The work divides into eight decisions across five concerns: how submodules
are fetched (1, 2), how the token reaches them (3), how failures are
reported (4), what the setting is called and where it lives (5, 6, 7), and
how a repository-scoped value reaches a per-Project execution path (8).

Decisions 3 and 4 are the pair that interact, and they interact more
closely than they first appear: both are built on a single read of
`.gitmodules`, which decides what is reachable *and* supplies the URL
rewrites. Read them together.

## Decision 1: `git submodule update`, not a hand-rolled fetch

The alternative — and the one that prompted this being examined properly —
is to treat each submodule the way `clone.go` treats the parent: read its
path and URL from `.gitmodules`, read the pinned commit from the tree
(`git ls-tree` reports it as a `160000` gitlink), then `init`,
`remote add`, `fetch <sha>`, `checkout` into the path. turnip already
proves that mechanism works: the parent clone fetches an arbitrary commit
by SHA today.

**Rejected because it reimplements git for no remaining benefit.** The
apparent advantages did not survive examination:

- *"It avoids the token-redaction problem."* It does not. Each submodule
  gets its own token-embedded remote URL, a different string from the
  parent's, which today's redaction would not strip either. The fix is
  needed under every option (Decision 3).
- *"It avoids the shallow-fetch ambiguity."* So does `git submodule
  update`, which fetches submodules at full depth unless asked otherwise.
  The ambiguity only exists if turnip passes `--depth`, and it will not
  (Requirement 3.3).

What remains is real cost: `.gitmodules` may use **relative** URLs
(`url = ../helm-charts`), resolved against the parent's remote — which
turnip would have to resolve itself, including against a remote that has a
token embedded in it. Nested submodules would need hand-rolled recursion.
Both are solved problems inside git.

Where the hand-rolled approach *would* win is targeted error messages, and
Decision 4 gets most of that more cheaply.

## Decision 2: it runs after the merge, in clone mode

Submodules are initialised after the base-branch merge, not after the
checkout. The merged tree is what the tool will read, and it is the merged
tree's gitlinks that record which submodule commits belong to it — merging
can change them, and resolving the pre-merge commits would leave the
workspace subtly wrong rather than obviously broken.

This lives in `runner.RunClone`'s path, so it happens in the clone
initContainer with the rest of the clone. Nothing about the Job shape
changes.

## Decision 3: the token reaches submodules through `GIT_CONFIG_*`

Submodule URLs come from `.gitmodules`, not from the parent's remote, so
the parent's token-embedded URL does not authenticate them. git is told to
rewrite them with `insteadOf`, which is multi-valued and matches on a URL
**prefix**.

**The rewrites are derived from the URLs actually present, not from a list
of schemes turnip guesses at.** Decision 4 reads every submodule URL out
of `.gitmodules` before anything is fetched, in order to test its host.
That same read supplies the rewrite: for each URL whose host is the repository's
own, turnip emits one entry mapping that *exact* URL to its authenticated
HTTPS equivalent. An exact URL is a valid prefix of itself, so this needs
no mechanism `insteadOf` does not already have. This is Requirement 3.5.

**Enumerating prefixes instead would leak**, which is why this is not what
the design does. git accepts `ssh://`, `git://`, `http://`, `https://`,
`ftp[s]://`, `file://` and the scp-like `[<user>@]<host>:<path>`; every
`://` form admits an explicit port, and scp-like makes the user optional.
So `ssh://git@github.com:22/org/repo` and `github.com:org/repo` are both
legal and neither matches a `ssh://git@github.com/` or `git@github.com:`
prefix. Deriving from the real URL sidesteps the enumeration entirely.

`git+ssh://` — the form that prompted this question — **is not a git URL
scheme.** It is a pip/npm/setuptools convention: those tools strip the
`git+` prefix before handing the URL to git. It cannot appear in a
`.gitmodules` git itself can read, so there is nothing to rewrite. The same
goes for any other `vcs+scheme` form.

**Nested submodules are the exception, and keep a prefix net.** Under
`recursive`, a submodule's own `.gitmodules` does not exist until its
parent has been fetched, so turnip cannot have read those URLs when it
builds the configuration. The ordinary forms are covered ahead of time:

```
url.https://x-access-token:<token>@github.com/.insteadOf = https://github.com/
url.https://x-access-token:<token>@github.com/.insteadOf = http://github.com/
url.https://x-access-token:<token>@github.com/.insteadOf = git://github.com/
url.https://x-access-token:<token>@github.com/.insteadOf = git@github.com:
url.https://x-access-token:<token>@github.com/.insteadOf = ssh://git@github.com/
```

A *nested* submodule written in an exotic form — an explicit port, a
user-less scp-like URL — is not rewritten and fails to authenticate. It
fails named, by Requirement 4.1, rather than silently; `recursive` combined
with an exotic nested URL is narrow enough to accept rather than pre-solve.
`ftp[s]://` is omitted because git deprecates it for fetching and says not
to use it; `file://` needs no credential.

SSH forms are rewritten rather than rejected because turnip holds no SSH
key and never will — the token is the only credential it has, so HTTPS is
the only scheme that can possibly work. `actions/checkout` makes the same
conversion for the same reason. A repository pinning its submodule over SSH
is expressing how a human clones it, not a constraint on how a machine with
an installation token must.

That rewrite is supplied through git's environment-based configuration —
`GIT_CONFIG_COUNT`, with a `GIT_CONFIG_KEY_n`/`GIT_CONFIG_VALUE_n` pair
per entry — set on the git **subprocess**, not on the container.

| Where the credential could go | Rejected because |
|---|---|
| `git -c url.…insteadOf=…` argument | the token is in `argv`, so it reaches any error message that echoes the command — the leak Requirement 4.3 exists to prevent |
| A gitconfig file (`git config --global`) | writes the token to disk for the lifetime of the container; acceptable, but strictly worse than not writing it |
| A credential helper | needs either an `argv` fragment or a file on disk; both of the above |
| **`GIT_CONFIG_*` on the subprocess** | **chosen**: never in `argv`, never on disk, and not in the Job spec |

The token is already in the clone container's environment as
`TURNIP_GITHUB_TOKEN`, so this adds no exposure that was not already there
— and it stays out of the container that runs the tool, which after Slice
14 never receives the token at all.

`execGit` currently leaves `cmd.Env` nil, inheriting the parent's
environment. The submodule call sets it explicitly; every other git call
is left alone.

## Decision 4: redaction covers the token, and failures name the submodule

Today `redactArgs` replaces arguments *exactly equal* to the authenticated
URL, and `redact` strips that same URL from output. Both miss a token that
appears inside a larger string — which is exactly what a rewritten
submodule URL is.

**Redaction gains the token itself as a second needle.** This is a
security fix in its own right, not a submodule detail: it is what makes
every future git invocation safe by default rather than safe only for the
one URL shape that exists today.

For Requirement 4.4 — a submodule URL the token cannot authenticate — the
URLs are read first, with git rather than by hand:

```
git config -f .gitmodules --get-regexp ^submodule\..*\.url$
```

This read serves twice: it decides which submodules are reachable, and it
supplies Decision 3's exact-URL rewrites. Extracting the host cannot be
`net/url.Parse` alone, because a scp-like URL has no scheme — git's own
rule is that the scp-like form is recognised only when there is no slash
before the first colon, which is what distinguishes `github.com:org/repo`
from the local path `./foo:bar`. turnip applies the same test.

The test is the **host**, not the scheme. Every form of the repository's
own host is authenticated, because Decision 3 rewrites them. What cannot
work is a submodule somewhere else entirely — GitLab, an internal server,
a different forge — since a GitHub installation token authenticates
nothing there whatever the URL looks like. That case is reported before
anything is fetched, naming the submodule and its host, rather than
surfacing as a generic authentication failure several layers down. This is
the targeted-error benefit the hand-rolled approach promised, at the cost
of one git command.

A submodule that cannot be fetched fails the clone. It must not leave an
empty directory and continue: that is precisely the behaviour that reached
the pilot as `Error: repo .. not found`, a message naming neither the
submodule nor the repository it came from.

## Decision 5: the modes are `none`, `top-level` and `recursive`

An earlier draft named these `none`, `shallow` and `recursive`, following
`actions/checkout`'s three-state shape a little too literally. **`shallow`
is the wrong word** and does not ship: in git it means a depth-limited
fetch, and this design deliberately does the opposite — submodules are
fetched at full depth so the pinned commit is certainly present
(Requirement 3.3). A reader seeing `submodules: shallow` would reasonably
expect `--depth`. Requirement 2.2 and the glossary carry `top-level`.

| Mode | Behaviour |
|---|---|
| `none` | submodules are not initialised |
| `top-level` | one level of submodules (the default) |
| `recursive` | submodules of submodules too |

`top-level` is the default because nesting is rarer and costs more to
fetch, and because a repository that needs `recursive` knows it does.

## Decision 6: a Server default a repository may override

**The outcome**: the Server carries the default, a repository may override
it in its own configuration file, and whether that override is honoured is
decided by the existing `TURNIP_ALLOWED_OVERRIDES` list. Three mechanisms,
one of which already exists, and no new configuration surface.

| Setting | Lives in | Default |
|---|---|---|
| `TURNIP_CLONE_SUBMODULES` | Server environment | `top-level` |
| `clone.submodules` | the repository's config file, under `clone:` | unset — the Server's value applies |
| `clone.submodules` in `TURNIP_ALLOWED_OVERRIDES` | Server environment | absent — a repository's override is refused |

**Alternative considered: the Server setting alone.** *Rejected because*
whether a repository has submodules, and whether they are needed, is a
fact about that repository — and one Server serves many. An operator would
be choosing for repositories they may never have opened, and the common
case (fetch them, they are needed) would be indistinguishable from the
rare one.

**Alternative considered: the repository setting alone.** *Rejected
because* the two cases where fetching is wrong are the operator's to
arbitrate, not the repository's: a submodule the App cannot read, and one
too expensive to be worth fetching. A repository failing because of its
own submodule is also the party least likely to turn it off.

**Alternative considered: per-repository server-side configuration**, the
shape Atlantis uses in `repos.yaml`. *Rejected for this slice because*
turnip has no such surface, and introducing one to express a tri-state
value is disproportionate. Recorded in `roadmap.md`'s Backlog; it becomes
worth building when several settings want per-repository policy, not for
the first one that asks.

The default is `top-level` rather than `none`, diverging from
`actions/checkout`, for the reason given in the requirements: that action
checks out repositories for arbitrary purposes, while turnip clones
specifically to run IaC that may reference submodule paths.

The override is gated for cost, not for safety. Fetching a submodule the
App can already read grants no capability the repository does not have —
by the principle that gates belong on what *grants* capability rather than
what uses it, this would not need gating at all. It is gated because an
operator may have a reason to refuse the fetch, and because reusing the
existing list costs nothing.

The value is carried to the Runner as an environment variable like every
other per-Operation setting; `runner.Config` gains a field, and it is read
only in clone mode.

## Decision 7: the field is `clone.submodules`

`actions/checkout` names its input exactly `submodules`, taking
`false`/`true`/`recursive`. The bare noun is the prior art and is kept —
but nested, not loose. This is Requirements 2.6 and 2.7.

**Not a top-level key.** The schema has two today, `schemaVersion` and
`projects`, and both are structural. A third loose key would open a
grab-bag that every future repository-wide setting joins. A `clone:` block
gives this one a home and names the thing it configures — and it has real
neighbours coming: `actions/checkout` groups `submodules` with `lfs`,
`fetch-depth` and `sparse-checkout`, all clone-time concerns, and turnip
already has a bounded-depth fetch that could want the same treatment.

**`clone:` rather than `config:`**, which inside a configuration file says
only "this is configuration" — true of every key in it.

**Not `enableSubmodules`.** The value is a tri-state mode, not a boolean;
`enableSubmodules: recursive` promises an on/off switch and then does not
deliver one.

**Not `cloneSubmodules` or `clone_submodules`.** Under a `clone:` block the
first stutters, and the second would be the only snake_case key in a
schema that is camelCase throughout — `schemaVersion`, `whenModified`,
`serviceAccount`.

In a repository's configuration file:

```yaml
schemaVersion: v1alpha2

clone:
  submodules: recursive

projects:
  - directory: infrastructure
    uses: helmfile@1.1.7
```

The dotted override path then falls out for free: `clone.submodules` has
the same shape as Slice 13's `runner.serviceAccount`, the only path
`TURNIP_ALLOWED_OVERRIDES` knows today. A bare `submodules` would have been
the odd one out.

Per-Project would be incoherent: one clone serves every Project a pull
request matches, so two Projects disagreeing would have no resolution.
`clone:` therefore sits beside `projects:`, never inside it.

**No schema version bump.** This adds an *optional* key to `v1alpha2`:
every configuration file valid today stays valid, so there is nothing for
a new version to signal. A file using `clone:` against an older Server is
rejected by the strict-decoding path Slice 13 already built, naming the
unknown field — a legible failure rather than a silently ignored setting,
which is the outcome a version bump would be buying. Adding optional keys
is what an alpha version is for.

## Decision 8: the repository-scoped value rides on `Target`

Slice 13's `runner.serviceAccount` is per-Project, so `resolveServiceAccount`
finds everything it needs on `t.Project`. `clone.submodules` is not:
Decision 7 puts it beside `projects:`, and `Target` carries only a
`config.Project` — the `*config.Config` does not survive target
construction. **As the code stands the value has no route to `executeOne`
at all**, which is why this is a decision and not an implementation
detail.

**`Target` gains a `Clone config.CloneSpec` field**, copied in at the two
sites that build targets from a fetched config (the pull-request path and
the comment path). Resolution then happens in `executeOne`, immediately
beside `resolveServiceAccount` and before any lock is acquired, refusing
through the same rejected-result path.

*Alternative considered: a new parameter threaded through
`executeTargets` and `executeOne`.* **Rejected because** it widens several
signatures to carry a value that `Target` already exists to carry, and
`Target` is precisely what `executeOne` is handed.

*Alternative considered: resolving once at fetch time, before targets
exist.* **Rejected because** the refusal would then have no Target to
attach to, forcing a second reporting path alongside the per-Project
results that already consolidate into one comment.

Copying a repository-scoped value onto every Target is mild redundancy,
accepted deliberately: keeping the resolve-then-reject shape identical to
the one Slice 13 established is worth more than avoiding a duplicated
string. The cost of the duplication is that N matched Projects with a
forbidden override produce N rejections — which consolidate into a single
comment, so the operator sees one message either way.

## Edge cases

| Case | Behaviour |
|---|---|
| No `.gitmodules` | no-op; `git submodule update` succeeds trivially |
| Mode `none`, repository has submodules | not fetched; the tool fails on its own terms, which is what was asked for |
| Submodule the App cannot read | clone fails, naming the submodule |
| SSH submodule URL on the repository's host | rewritten to HTTPS and authenticated with the token |
| Submodule URL with an explicit port, or scp-like with no user | rewritten, because the rewrite is derived from the URL itself rather than matched against a prefix list |
| `git+ssh://` or another `vcs+scheme` URL | not a git URL scheme; git rejects it before turnip is involved |
| Exotic nested submodule URL under `recursive` | not rewritten — its `.gitmodules` was unreadable when the configuration was built — so it fails named rather than silently |
| Submodule on another host entirely | reported before fetching, naming the submodule and its host |
| Relative submodule URL | handled by git, resolved against the parent's remote |
| Submodule commit not reachable from any branch | fetch fails, named — unlike a silent empty directory |
| Nested submodules under `top-level` | not initialised; the tool fails on its own terms |

## Testing approach

`clone.go`'s existing tests use a real local git repository fixture rather
than a fake, which extends naturally: a fixture repository gains a
submodule pointing at a second local repository, so the merge-then-
initialise ordering and the pinned-commit resolution are exercised for
real without a network.

**Local submodules need `protocol.file.allow`, and only in tests.** git
refuses the `file` transport for submodules — `fatal: transport 'file' not
allowed` — as CVE-2022-39253 hardening, confirmed here against git 2.55.
It blocks `submodule update --init`, not merely `submodule add`, so both
the fixture construction and the code under test must run with
`-c protocol.file.allow=always`. This flag is **test-only and must never
be set on turnip's own clone path**: production submodule URLs resolve to
`https://`, relative ones included (they resolve against the parent's
authenticated HTTPS remote), so nothing in production needs it, and
setting it would reopen exactly what the hardening closed. A future reader
meeting a `transport 'file' not allowed` failure should add the flag to
the *test*, never to `clone.go`.

These cases are worth naming because they pin decisions rather than
behaviour:

- A token embedded in a rewritten submodule URL does not appear in a
  reported error — the assertion that pins Decision 4, and the one whose
  absence would leak a credential into a pull request.
- Mode `none` leaves the submodule directory empty and does **not** fail,
  distinguishing "configured off" from "failed to fetch".
- An SSH submodule URL on the repository's own host is rewritten and
  fetched, rather than refused — the assertion that pins Decision 3.
- A submodule URL carrying an explicit port is rewritten too. This is the
  assertion that pins the *derivation*: a prefix list passes the plain SSH
  case above and fails this one, so without it the design's central claim
  in Decision 3 goes untested.
- Host extraction handles the scp-like form, and does not mistake a local
  path containing a colon for one.
- A submodule on a foreign host is reported before any fetch is attempted,
  naming the submodule and its host.
