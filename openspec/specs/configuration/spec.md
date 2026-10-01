# configuration Specification

## Purpose
Defines how turnip finds, reads and validates a repository's `turnip.yaml`, and what the Projects, Runner settings and clone settings in it mean. End users rely on it to write a file turnip accepts, and every other capability relies on the Projects it yields.

## Requirements

### Requirement: Configuration file discovery
turnip SHALL read a repository's configuration from the pull request's head commit, trying `.turnip/config.yaml` first and then `turnip.yaml` at the repository root, and SHALL use the first that exists without merging the two. Only these two paths SHALL be read: no `.yml` spelling and no other directory.

#### Scenario: Dedicated directory takes precedence
- **WHEN** a pull request's head commit contains both `.turnip/config.yaml` and `turnip.yaml`
- **THEN** turnip SHALL read `.turnip/config.yaml` only
- **AND** the contents of `turnip.yaml` SHALL have no effect

#### Scenario: Root file used when the directory has none
- **WHEN** the head commit has no `.turnip/config.yaml` but has `turnip.yaml`
- **THEN** turnip SHALL read `turnip.yaml`

#### Scenario: Other spellings are not read
- **WHEN** the head commit has only `turnip.yml` or `.turnip/config.yml`
- **THEN** turnip SHALL treat the repository as having no configuration

#### Scenario: Read at the head commit
- **WHEN** a pull request's head branch changes the configuration
- **THEN** turnip SHALL act on the configuration as written at that head commit, not on the base branch's

### Requirement: Replies for a missing, invalid or unreadable configuration
When the configuration cannot be used, turnip SHALL run no Operation for the event. On the automatic path (a pull request opened, pushed to, reopened or marked ready) a missing configuration SHALL be silent, while an invalid one or a failed fetch SHALL post a pull request comment and mark the commit's configuration invalid for the `turnip` check (see the `merge-check` capability). On the comment path all three SHALL post a reply comment. The replies SHALL read:

| Situation | Comment |
|---|---|
| missing | a `[!WARNING]` alert: "No turnip configuration was found in this repository (checked `.turnip/config.yaml` or `turnip.yaml`)." |
| invalid | a `[!WARNING]` alert: "The turnip configuration is invalid, so no operations ran.", followed outside the alert by a code fence listing every reported problem |
| fetch failed | a `[!CAUTION]` alert: "Fetching the turnip configuration failed, so no operations ran. This is usually an authentication, permission, or rate-limit problem with turnip's GitHub App rather than something wrong with this repository.", followed by a collapsed "Error" section holding the error |

Known issue: an invalid `turnip.yaml` found on the comment path posts the reply but does not mark the `turnip` check invalid; only the automatic path does. Fix proposed in change `fix-invalid-config-check-on-comment`.

#### Scenario: Repository that never onboarded stays quiet
- **WHEN** a pull request is opened in a repository with no configuration at either path
- **THEN** turnip SHALL post no comment and run nothing

#### Scenario: Explicit command in a repository without configuration
- **WHEN** a collaborator comments a trigger command on a pull request whose head commit has no configuration
- **THEN** turnip SHALL reply with the missing-configuration warning naming both accepted paths

#### Scenario: Invalid file on the automatic path
- **WHEN** a push leaves `turnip.yaml` invalid
- **THEN** turnip SHALL post the invalid-configuration warning with every problem in a code fence
- **AND** the `turnip` check for that commit SHALL report an invalid configuration

#### Scenario: Invalid file on the comment path
- **WHEN** a trigger comment is posted on a pull request whose `turnip.yaml` is invalid
- **THEN** turnip SHALL reply with the invalid-configuration warning
- **AND** the `turnip` check SHALL be left as it was

#### Scenario: GitHub refuses the fetch
- **WHEN** reading the configuration fails for a reason other than the file not existing (authentication, permission, rate limit)
- **THEN** turnip SHALL post the caution comment with the error in a collapsed section and run nothing

### Requirement: Schema version
The file SHALL declare `schemaVersion: v1alpha3`, the only version this turnip accepts. A missing or different version SHALL be reported on its own, before and instead of any unrecognized-key or other validation error, since it explains them. turnip SHALL NOT upgrade or reinterpret a file written for another version.

#### Scenario: Missing version
- **WHEN** a file has no `schemaVersion`
- **THEN** validation SHALL fail with exactly one problem: `config: turnip.yaml: schemaVersion: required; set schemaVersion: v1alpha3`

#### Scenario: Older version
- **WHEN** a file declares `schemaVersion: v1alpha2` and carries fields that version used
- **THEN** validation SHALL fail with one problem naming both values: `unsupported version "v1alpha2"; this turnip supports "v1alpha3"`
- **AND** no unrecognized-field problem SHALL be reported alongside it

### Requirement: Parsing and error reporting
turnip SHALL check a file in stages and report every problem of the first stage that fails, each on its own line as `config: <owner>: <field>: <message>`, where the owner is the Project's name, `projects[<index>]` for a Project with neither name nor directory, or `turnip.yaml` for the file as a whole. The stages SHALL be: YAML syntax, then `schemaVersion`, then unrecognized keys, then every other validation rule. Unrecognized keys SHALL be errors at every level except inside a Project's `with`. YAML anchors and merge keys (`<<`) SHALL be expanded before keys are checked.

#### Scenario: Malformed YAML
- **WHEN** a file is not valid YAML, or a field holds the wrong kind of value
- **THEN** turnip SHALL report a single parse error beginning `config: parse error`, naming the line when the YAML decoder reports one

#### Scenario: Every unrecognized key at once
- **WHEN** a file with a supported `schemaVersion` misspells two keys in different Projects
- **THEN** turnip SHALL report both, each as `config: turnip.yaml: <key>: unrecognized field (line <n>)`
- **AND** no other validation problem SHALL be reported until they are fixed

#### Scenario: Remaining problems accumulate
- **WHEN** a file's keys are all recognized but one Project lacks `directory` and another has an invalid `whenModified` pattern
- **THEN** turnip SHALL report both problems together in one reply

#### Scenario: Anchors are accepted
- **WHEN** two Projects share a `runner:` block through a YAML anchor and alias
- **THEN** the file SHALL validate as if the block were written out in each Project

### Requirement: Projects
`projects` SHALL be an optional list; each Project SHALL require `directory` (the tool's working directory, relative to the repository root) and `uses` (see the `tool-images` capability). `name` SHALL default to `directory` when omitted. After defaulting, names SHALL be unique across the file, SHALL NOT contain `*`, and SHALL NOT begin with `-`, so that every Project can be named in a trigger command.

#### Scenario: Name defaults to directory
- **WHEN** a Project sets `directory: infra/web` and no `name`
- **THEN** the Project SHALL be named `infra/web` everywhere turnip names it

#### Scenario: Missing directory
- **WHEN** a Project has no `directory`
- **THEN** validation SHALL report `directory: directory is required` against that Project

#### Scenario: Defaulted names collide
- **WHEN** two Projects share a `directory` and neither sets `name`
- **THEN** validation SHALL report `duplicate project name "<directory>"` against the second

#### Scenario: Unaddressable names
- **WHEN** a Project's effective name contains `*` or begins with `-`
- **THEN** validation SHALL reject it, explaining that a trigger reads such a token as a pattern or as the start of tool arguments

#### Scenario: Empty file of Projects
- **WHEN** a file declares `schemaVersion: v1alpha3` and no Projects
- **THEN** it SHALL be valid and no pull request SHALL affect any Project

### Requirement: whenModified matching
A Project SHALL be affected by a pull request when at least one of its `whenModified` patterns matches at least one path GitHub reports as changed. Patterns SHALL use doublestar glob syntax against repository-root-relative, forward-slash paths: `*` and `?` SHALL NOT cross a `/`, `**` SHALL match any number of path segments, and matching SHALL be case-sensitive and textual (symlinks are not followed). A leading `./` and backslash separators in a reported path SHALL be normalized away before matching. An invalid pattern SHALL be a validation error naming its index. A Project with no patterns SHALL never be affected by changed files and runs only when a command names it or selects every Project (see the `commands` capability).

#### Scenario: Recursive pattern
- **WHEN** a Project has `whenModified: ["infra/web/**"]` and a pull request changes `infra/web/charts/values.yaml`
- **THEN** the Project SHALL be affected

#### Scenario: Single star stays in one segment
- **WHEN** a Project has `whenModified: ["infra/*.yaml"]` and the only change is `infra/web/values.yaml`
- **THEN** the Project SHALL NOT be affected

#### Scenario: Symlinked directory
- **WHEN** a Project's directory reaches shared files through a symlink and only the link target changes
- **THEN** the Project SHALL be affected only if a pattern matches the target's real path

#### Scenario: Invalid pattern
- **WHEN** a Project's second pattern is `infra/[web`
- **THEN** validation SHALL report `whenModified[1]: invalid glob pattern "infra/[web"`

#### Scenario: No patterns
- **WHEN** a Project omits `whenModified`
- **THEN** no push or automatic plan SHALL ever select it

### Requirement: with passthrough
A Project's `with` SHALL be a map of string values handed to the Project's Plugin unchanged; turnip SHALL accept any key in it and interpret none itself. Which keys have an effect is defined per tool (see the `plugin-helmfile` capability).

#### Scenario: Unknown key inside with
- **WHEN** a Project sets `with: {environment: staging, team: web}`
- **THEN** validation SHALL accept both keys
- **AND** both SHALL reach the Plugin, which acts only on the keys it reads

### Requirement: Runner settings at two levels
`runner:` SHALL be accepted both at the top level and on each Project, with the same fields (`serviceAccount`, `env`) and meaning. Each Project SHALL run with its own block merged over the top-level one: `serviceAccount` is replaced (the Project's value if set, else the top level's, else the Server's default; see the `runner-access` capability), and `env` is merged per variable (every top-level variable, then every Project variable over it). A Project SHALL NOT be able to unset a shared variable; setting it to an empty value overrides it with the empty string. Projects inheriting the same top-level block SHALL NOT affect one another's settings.

#### Scenario: Env merged per variable
- **WHEN** the top level sets `env: {AWS_REGION: us-west-2}` and Project `web` sets `env: {AWS_PROFILE: web}`
- **THEN** `web`'s tool SHALL run with both `AWS_REGION=us-west-2` and `AWS_PROFILE=web`

#### Scenario: Project value wins for the same variable
- **WHEN** both levels set `AWS_REGION` and the Project's value is `eu-west-1`
- **THEN** the Project's tool SHALL see `AWS_REGION=eu-west-1`

#### Scenario: Empty value overrides rather than unsets
- **WHEN** a Project sets a shared variable to `""`
- **THEN** the variable SHALL be present in the tool's environment with an empty value

#### Scenario: serviceAccount replaced
- **WHEN** the top level sets `serviceAccount: deployer` and Project `api` sets `serviceAccount: api-deployer`
- **THEN** `api` SHALL use `api-deployer` and every other Project SHALL use `deployer`, subject to the override gate in the `runner-access` capability

### Requirement: Reserved environment names
In either `runner.env` block, a name beginning with `TURNIP_`, and the name `PATH`, SHALL be validation errors. Every offending name SHALL be reported together, in sorted order; a name in the top-level block SHALL be reported once against `turnip.yaml`, not once per Project inheriting it.

#### Scenario: Reserved prefix in a Project
- **WHEN** Project `web` sets `runner.env: {TURNIP_SERVER_ADDR: x}`
- **THEN** validation SHALL report `config: web: runner.env["TURNIP_SERVER_ADDR"]: names beginning with "TURNIP_" are reserved`

#### Scenario: PATH at the top level
- **WHEN** the top-level `runner.env` sets `PATH` and three Projects inherit it
- **THEN** validation SHALL report exactly one problem, `config: turnip.yaml: runner.env["PATH"]: PATH is reserved; the tools directory is prepended to it at startup`

### Requirement: Clone settings
A top-level `clone.submodules` SHALL accept `none`, `top-level` or `recursive`; omitted, the Server's default SHALL apply. Any other value SHALL be a validation error against `turnip.yaml`. What each mode does, and whether the Server honors the repository's value, belong to the `checkout` and `runner-access` capabilities.

#### Scenario: Unrecognized mode
- **WHEN** a file sets `clone.submodules: shallow`
- **THEN** validation SHALL report `config: turnip.yaml: clone.submodules: unrecognized value "shallow"; expected "none", "top-level" or "recursive"`

#### Scenario: Omitted
- **WHEN** a file has no `clone:` block
- **THEN** the clone SHALL use the Server's configured submodule mode
