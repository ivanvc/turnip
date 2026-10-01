# checkout Specification

## Purpose
Defines how a Runner checks out the pull request it operates on: which commits it fetches, how the base branch is merged in, how submodules are initialized and authenticated, and how a failed checkout is reported. Repository authors and operators rely on it to know exactly what tree the tool runs against.

## Requirements

### Requirement: Clone in an init container before the tool
The repository SHALL be checked out by the `clone` init container, running turnip's Runner image, into `/turnip/src` (see the `runner-job` capability), before the container that runs the tool starts. The container running the tool SHALL need no git of its own. When the checkout succeeds, the `clone` container SHALL exit 0 without reporting anything to the Server.

#### Scenario: A tool image without git
- **WHEN** a Project's tool image contains no `git` binary
- **THEN** the Operation still runs against the checked-out repository in `/turnip/src`

#### Scenario: A successful checkout
- **WHEN** the clone completes
- **THEN** the `clone` container exits 0 and the `runner` container starts with the repository in `/turnip/src`

### Requirement: Head commit with the base branch merged in
The clone SHALL fetch, from the pull request's repository URL, the pull request's head commit (as of dispatch) and the tip of its base branch, each to a depth of 50 commits, check out the head commit, and merge the base branch into it with a non-fast-forward merge commit (message `turnip-merge`, author `turnip <turnip@localhost>`). The tool SHALL run against the result of that merge, so it sees what merging the pull request would produce at that moment. When no base branch is given, the clone SHALL check out the head commit alone.

If the merge fails because the two shallow histories share no commit within the fetched depth (git's `refusing to merge unrelated histories`), the clone SHALL fetch the full history once (`git fetch --unshallow`) and retry the merge once.

#### Scenario: A pull request behind its base branch
- **WHEN** a pull request's base branch has gained a commit since the branch was created
- **THEN** the tool runs against the head commit with that base commit merged in

#### Scenario: A branch older than the fetch depth
- **WHEN** the head commit and the base branch tip have no common ancestor within 50 commits of each
- **THEN** the clone fetches the full history and retries the merge, and the Operation proceeds if that merge succeeds

### Requirement: Merge conflicts fail the checkout
When merging the base branch produces a conflict, the clone SHALL abort the merge and fail the checkout with a message beginning `clone failed: runner: clone: merge conflict: ` followed by git's merge output. The tool SHALL NOT run.

#### Scenario: A conflicting pull request
- **WHEN** the pull request's changes conflict with its base branch
- **THEN** the Operation fails with failure category clone failed and the message names the merge conflict and git's output
- **AND** no tool runs for that Operation

### Requirement: Checkout failures are reported, not retried
Any failed checkout step (fetch, checkout, merge, submodule initialization) SHALL be reported by the `clone` container itself over the Operation's stream as a failed result with failure category clone failed and exit code -1 (see the `operation-lifecycle` capability), with a message beginning `clone failed: ` that carries the failing git command and its output, with workspace paths stripped. The `clone` container SHALL then exit 1, so the `runner` container never starts.

#### Scenario: The head commit cannot be fetched
- **WHEN** the head commit no longer exists on the remote
- **THEN** the Operation fails with failure category clone failed and a message naming the `git fetch` that failed and git's output

### Requirement: Submodule modes
The clone SHALL initialize submodules according to a Submodule_Mode, one of:

| Mode | Behavior |
|---|---|
| `none` | submodules are not initialized; their directories stay empty |
| `top-level` | `git submodule update --init`: one level |
| `recursive` | `git submodule update --init --recursive`: submodules of submodules too |

The mode SHALL be the Server's `TURNIP_CLONE_SUBMODULES` (default `top-level`; surrounding whitespace ignored), unless the repository's `clone.submodules` names one and the Server permits that override (see the `runner-access` capability for the `TURNIP_ALLOWED_OVERRIDES` gate and its refusal, and the `configuration` capability for the field). Any other value of `TURNIP_CLONE_SUBMODULES` SHALL stop the Server from starting with an error listing the known modes (see the `server` capability). Submodules SHALL be fetched at full depth, so the commit a parent pins is always present. A repository with no `.gitmodules` SHALL be checked out without any submodule step.

#### Scenario: The default
- **WHEN** `TURNIP_CLONE_SUBMODULES` is unset and `turnip.yaml` has no `clone.submodules`
- **THEN** one level of submodules is initialized

#### Scenario: Submodules turned off
- **WHEN** the effective mode is `none` and the repository has a submodule at `charts`
- **THEN** `/turnip/src/charts` exists and is empty

#### Scenario: A permitted repository override
- **WHEN** `TURNIP_ALLOWED_OVERRIDES` includes `clone.submodules` and `turnip.yaml` sets `clone.submodules: recursive`
- **THEN** nested submodules are initialized

### Requirement: Same-host submodule URLs are rewritten to HTTPS
For submodules on the same host as the pull request's repository, the clone SHALL rewrite each submodule URL to its HTTPS equivalent on that host, whatever form `.gitmodules` uses (SSH `git@host:`, `ssh://git@host/`, `git://host/`, `http://host/`, or an explicit port), so every fetch authenticates with the installation credential (see the `runner-security` capability). A relative submodule URL SHALL resolve against the repository's own URL. The rewrite SHALL apply only for the duration of the clone and SHALL NOT be written to the repository's configuration.

#### Scenario: An SSH submodule URL
- **WHEN** `.gitmodules` lists `git@github.com:example/charts.git` and the repository is on `github.com`
- **THEN** the submodule is fetched from `https://github.com/example/charts.git` with the installation credential

#### Scenario: A relative submodule URL
- **WHEN** `.gitmodules` lists `../charts.git`
- **THEN** the submodule is fetched from the repository's own host

### Requirement: Off-host submodules are refused before fetching
When any submodule URL in `.gitmodules` names a host other than the pull request repository's host, and the mode is not `none`, the clone SHALL fail before any submodule is fetched, with a message naming the submodule and its host: `submodule "<name>" is hosted on <host>, which this installation token cannot authenticate (turnip authenticates only <repository host>)`.

#### Scenario: A submodule on another forge
- **WHEN** `.gitmodules` lists submodule `shared` at `https://git.example.com/shared.git` and the repository is on `github.com`
- **THEN** the Operation fails with failure category clone failed and a message stating that submodule `shared` is hosted on `git.example.com`
- **AND** no submodule fetch is attempted

#### Scenario: Submodules off
- **WHEN** the mode is `none` and a submodule is hosted off-host
- **THEN** the checkout succeeds without examining submodule hosts

### Requirement: Access hint for unreadable submodules
When a submodule fetch fails with git's `Repository not found`, the failure message SHALL end with a hint stating that turnip authenticated the fetch with its installation token, so the likely cause is that the GitHub App is not installed on that repository, and that the App's repository access should be checked.

#### Scenario: The App is not installed on a submodule's repository
- **WHEN** a same-host submodule's repository is outside the GitHub App installation's repository access
- **THEN** the clone fails with git's `Repository not found` output followed by a `hint:` line pointing at the GitHub App's repository access
