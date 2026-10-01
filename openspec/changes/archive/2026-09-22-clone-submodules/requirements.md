# Requirements: Cloning Submodules (Slice 16)

## Introduction

turnip's clone does not initialize submodules. `internal/runner/clone.go`
runs `init`, `remote add`, a bounded-depth `fetch`, `checkout` and a merge —
and stops. A repository with a submodule is checked out with that path
present but empty.

Nothing reports this. It surfaces later, wherever the tool first reaches
through the gap. In the pilot it arrived as:

```
helm pull ../helm-charts/example-app --untar -d /tmp/chartify.../example-app
Error: repo .. not found
```

helm could not read `../helm-charts/...` as a local chart path, so it fell
back to parsing it as `<repo>/<chart>` and reported a missing repository
named `..`. The real cause — an empty submodule directory — appears nowhere
in that message. A silently incomplete checkout producing a confusing error
several layers away is the failure mode this slice removes.

**Why this is configurable rather than assumed.** Fetching submodules is
the right default, but there are two cases where it is wrong: a submodule
the GitHub App cannot read, where fetching fails and a previously-working
repository stops working; and a large submodule the IaC code never
references, where fetching is waste. Neither is hypothetical enough to
ignore, and neither is common enough to make "off" the default.

The cost argument is weaker than it first appears: `git submodule update
--init` is a no-op when a repository has no `.gitmodules`, so repositories
without submodules pay nothing whichever way the setting goes.

**Prior art**, consulted and diverged from deliberately:

- `actions/checkout` takes `submodules` as an input with three values —
  `false`, `true`, `recursive` — defaulting to `false`. The three-state
  shape is adopted here. The default is not: that action checks out
  repositories for arbitrary purposes, where turnip clones specifically to
  run IaC that may reference submodule paths, so defaulting off would make
  every repository with a submodule meet the confusing failure above
  before anything worked.
- Atlantis has no native submodule support. Its issue for it has been open
  since October 2018, and users work around it with server-side
  pre-workflow hooks running `git submodule init`. That is a gap nobody
  closed rather than a considered rejection, so it is weak evidence either
  way — but it does show the workaround people reach for, and that
  operator-side configuration is where Atlantis puts this kind of setting.

**Global requirements**: none directly. Extends Requirement 5's clone
behavior (grpc-runner's slice) without changing its merge semantics.

**Explicitly out of scope**, each deferred to named future work:

- **Per-repository server-side configuration.** The most precise home for
  this setting, and the shape Atlantis uses (`repos.yaml` keyed by
  repository). turnip has no per-repo server config surface at all, and
  building one for a tri-state setting is disproportionate. Recorded in
  `roadmap.md`'s Backlog.
- **A configuration UI.** Further still: no UI, no settings persistence,
  no authentication for one.
- **Submodules hosted somewhere other than the repository's own forge.**
  A GitHub installation token authenticates nothing on GitLab or an
  internal server, whatever the URL looks like, and turnip has no other
  credential to offer. SSH URLs on the repository's *own* host are handled
  — Requirement 3.4 rewrites them — but a foreign host is out of scope,
  and Requirement 4.4 requires it to be reported clearly rather than
  attempted.
- **Submodule-aware change detection.** A modified file inside a submodule
  does not currently match a Project's `whenModified` patterns, since
  GitHub reports the submodule as a single changed path. Out of scope and
  unaddressed.

## Glossary

Terms additional to the global spec's glossary:

- **Submodule_Mode**: one of `none`, `top-level` or `recursive`, deciding
  whether a clone initializes submodules and how deeply. Not `shallow`:
  in git that word means a depth-limited fetch, and submodules are
  deliberately fetched at full depth here (Requirement 3.3), so the name
  would promise the opposite of what happens.

## Requirements

### Requirement 1: Submodules are initialized by default

**User Story:** As a developer whose repository uses a submodule, I want
turnip to check it out, so that my tool finds the files it references.

#### Acceptance Criteria

1. WHEN cloning, THE Server SHALL initialize the repository's submodules
   unless configured otherwise
2. THE initialization SHALL happen after the base-branch merge, so that
   the submodule commits resolved are the ones the merged tree records
3. WHERE a repository has no `.gitmodules`, initialization SHALL be a
   no-op and SHALL NOT fail
4. THE default SHALL be a single level of submodules, not recursive:
   nesting is rarer than the flat case and costs more to fetch

### Requirement 2: The mode is configurable

**User Story:** As a platform operator, I want to control whether
submodules are fetched, so that a repository with an unreachable or
expensive submodule is not forced to fetch it.

#### Acceptance Criteria

1. THE Server SHALL read a Submodule_Mode from its own configuration,
   applying it to every repository it clones
2. THE accepted values SHALL be `none`, `top-level` and `recursive`; an
   unrecognized value SHALL be a startup error rather than a silently
   ignored setting
3. A repository's configuration file SHALL be able to override the
   Server's mode, since whether a repository has submodules — and whether
   they are needed — is a fact about that repository
4. THAT override SHALL be gated through the existing
   `TURNIP_ALLOWED_OVERRIDES` mechanism rather than a new one, under the
   path `clone.submodules` — the same dotted, camelCase shape as the
   existing `runner.serviceAccount`
5. THE override SHALL be repository-scoped rather than per-Project: one
   clone serves every Project matched by a pull request, so a per-Project
   value would have no coherent meaning when two disagreed
6. THE field SHALL be nested under a `clone:` block rather than placed
   loose at the top level, so that repository-wide clone settings have a
   home and the top level does not accumulate unrelated keys
7. THE field SHALL be named `submodules`, following `actions/checkout`'s
   input of that name, rather than `enableSubmodules` or a similar
   `enable`-prefixed name: the value is a tri-state mode, and an `enable`
   prefix would promise a boolean the setting does not provide

### Requirement 3: Private submodules authenticate

**User Story:** As a developer, I want a submodule in another private
repository my App can read to be fetched, so that turnip works for the
repositories I actually have.

#### Acceptance Criteria

1. THE installation token SHALL authenticate submodule fetches, which read
   their URLs from `.gitmodules` rather than from the parent's remote
2. THE token SHALL NOT be written to any file inside the workspace, where
   the tool — and anything it runs — could read it
3. Submodules SHALL be fetched at full depth rather than shallowly: the
   parent's bounded-depth fetch does not apply to them, and a shallow
   submodule fetch can fail to reach the exact commit the parent pins
4. WHERE a submodule URL names the repository's own host in any scheme git
   accepts — `ssh://`, `git://`, `http://`, or the scp-like
   `[user@]host:path`, with or without an explicit port — THE Server SHALL
   rewrite it to authenticated HTTPS. turnip holds no SSH key and never
   will, so HTTPS is the only scheme that can work; a repository pinning
   its submodule over SSH is describing how a human clones it, not
   constraining how a machine with an installation token must
5. THE rewrite SHALL be derived from the submodule URLs actually read from
   `.gitmodules` rather than from a fixed list of URL prefixes, since git's
   accepted forms vary by scheme, port and user and a prefix list would
   silently miss legal ones

### Requirement 4: Failures are legible

**User Story:** As a developer, I want a submodule problem reported as a
submodule problem, so that I am not debugging a message from a tool that
has no idea what went wrong.

#### Acceptance Criteria

1. IF a submodule cannot be fetched, THEN THE clone SHALL fail, reporting
   which submodule failed and why
2. THE clone SHALL NOT leave an empty submodule directory and continue —
   that is the behavior that produced `repo .. not found`
3. THE installation token SHALL NOT appear in any reported message.
   Today's redaction replaces only arguments *exactly equal* to the
   authenticated URL, so a token carried inside a larger argument would
   reach a pull request comment; redaction SHALL cover the token itself
4. WHERE a submodule is hosted somewhere the installation token cannot
   authenticate — a host other than the repository's own — THE failure
   message SHALL name the submodule and that host, rather than reporting a
   generic authentication error. THE test SHALL be the host rather than
   the scheme, since Requirement 3.4 makes every form of the repository's
   own host work

### Requirement 5: Documentation

#### Acceptance Criteria

1. `docs/configuration.md` SHALL document the Server setting
   (`TURNIP_CLONE_SUBMODULES`), the repository override
   (`clone.submodules`, gated through `TURNIP_ALLOWED_OVERRIDES`), and the
   three modes
2. `docs/troubleshooting.md` SHALL carry the symptom this slice was found
   through — a tool reporting a missing path or repository that is
   actually an uninitialized submodule — since that is what the next
   person will search for
3. THE documentation SHALL state that submodule fetches use the same
   installation token, and therefore reach only repositories the App is
   installed on
