# tool-images Specification

## Purpose
Defines what image a Project's tool runs in: the `uses:` line end users write, the built-in aliases, the operator's `TURNIP_ALLOWED_IMAGES` list, and how the image a plan ran is the image its apply runs. End users rely on it to pick a tool version; operators rely on it to decide which images their Runners are allowed to execute.

## Requirements

### Requirement: uses grammar
Each Project's `uses:` SHALL name what to run as `<alias>@<tag-spec>` or `<image>@<tag-spec>`, split at the last `@`. An alias SHALL be a registered tool name (see the `plugins` capability). Any other name SHALL be a fully qualified image, meaning its first path element contains `.` or `:` or is `localhost`. The tag-spec SHALL be exactly one tag (OCI tag syntax: letters, digits, `.`, `_` and `-`, starting with a letter, digit or `_`, at most 128 characters) or one digest, `sha256:` followed by exactly 64 lowercase hex characters, and SHALL be used verbatim. turnip SHALL keep no default version for any tool. Each malformed line SHALL be a validation error of the file (see the `configuration` capability) on the Project's `uses` field, reporting the first rule it breaks:

| Line | Message |
|---|---|
| empty | `required; set uses: <tool>@<tag>, e.g. "helmfile@v1.7.4", where <tool> is one of "helmfile", or a fully qualified <image>@<tag>` |
| no `@` | `"<uses>" names no version; name one as <tool>@<tag>, e.g. "helmfile@v1.7.4"` |
| nothing before `@` | `"<uses>" names no tool or image before "@"` |
| nothing after `@` | `version is empty after "@"; name one, e.g. "helmfile@v1.7.4"` |
| registry form `image:tag` | `"<uses>" names its tag with ":"; write it after an "@": "<image>@<tag>"` |
| unqualified non-alias name | `unsupported tool "<name>", must be one of "helmfile"; any other image must be fully qualified, registry host included (e.g. "ghcr.io/org/<name>@<tag>")` |
| digest other than sha256, or malformed | names the accepted form, `sha256:<64 hex characters>` |
| pattern, range or whitespace instead of a tag | `"<tag-spec>" is not an exact tag; uses: takes one exact tag or sha256 digest, not a pattern or a range ...` |

#### Scenario: Alias with a version
- **WHEN** a Project writes `uses: helmfile@v1.7.4`
- **THEN** the Project SHALL run tool `helmfile` in image `ghcr.io/helmfile/helmfile:v1.7.4`

#### Scenario: No version
- **WHEN** a Project writes `uses: helmfile`
- **THEN** validation SHALL fail asking for a version, e.g. `helmfile@v1.7.4`

#### Scenario: Pasted registry reference
- **WHEN** a Project writes `uses: ghcr.io/org/helmfile-aws:v1.7.4`
- **THEN** validation SHALL fail showing the corrected line `ghcr.io/org/helmfile-aws@v1.7.4`

#### Scenario: Unknown short name
- **WHEN** a Project writes `uses: terraform@1.9.0` and no Terraform Plugin is registered
- **THEN** validation SHALL fail as an unsupported tool, listing the registered tools

#### Scenario: Range instead of a tag
- **WHEN** a Project writes `uses: helmfile@v1.*`
- **THEN** validation SHALL fail saying `uses:` takes one exact tag or sha256 digest

### Requirement: Built-in aliases
Each registered tool SHALL have an alias, named for the tool, standing for that tool's vendor image and allowing only the tag-spec globs its Plugin declares (for Helmfile, see the `plugin-helmfile` capability). An alias SHALL try only its own globs, and an operator SHALL NOT be able to redefine it. A tag-spec the alias does not allow SHALL be a validation error naming the globs it does allow and saying that any other tag of the vendor image runs only written out in full and added by the operator to `TURNIP_ALLOWED_IMAGES`. Globs SHALL be matched against the tag-spec's text, where `*` matches any run of characters, `?` one character and `[...]` a class.

#### Scenario: Floating tag through an alias
- **WHEN** a Project writes `uses: helmfile@latest`
- **THEN** validation SHALL fail with `"latest" is not a tag helmfile allows; it allows only tags matching "v*.*.*" or "sha256:*", e.g. "helmfile@v1.7.4"`
- **AND** the message SHALL go on to say that `ghcr.io/helmfile/helmfile@latest` runs only written in full and added by the operator to `TURNIP_ALLOWED_IMAGES`

#### Scenario: New vendor release
- **WHEN** a vendor publishes a new full version and a Project names it through the alias
- **THEN** turnip SHALL accept it with no change to turnip, since it keeps no list of known versions

#### Scenario: Tag allowed but never published
- **WHEN** a tag matches the alias's globs but does not exist in the registry
- **THEN** validation SHALL pass and the Operation SHALL fail when its image cannot be pulled

### Requirement: Allowed images list
The Server setting `TURNIP_ALLOWED_IMAGES` SHALL be a comma-separated list of `tool:image@glob` entries (whitespace around entries trimmed, blank entries ignored), each allowing one fully qualified image, with tag-specs its glob matches, to run as one tool. Unset or blank SHALL allow nothing beyond the aliases. The tool SHALL be the text before the first `:` and SHALL be a registered tool; the glob SHALL be the non-empty, well-formed text after the last `@`; the image SHALL be non-empty, fully qualified, and carry no tag or digest of its own. A `uses:` naming a full image SHALL be tried against every entry for that image, the aliases' included, and the Project SHALL run as the tool of the first entry that matches. A full image no entry allows SHALL be a validation error: `this Server does not allow "<image>@<tag-spec>"; to run it, the operator must add it to TURNIP_ALLOWED_IMAGES (e.g. <tool>:<image>@<tag-spec>)`.

#### Scenario: Custom image allowed
- **WHEN** `TURNIP_ALLOWED_IMAGES=helmfile:ghcr.io/org/helmfile-aws@v*.*.*` and a Project writes `uses: ghcr.io/org/helmfile-aws@v1.7.4`
- **THEN** the Project SHALL run as a Helmfile Project in `ghcr.io/org/helmfile-aws:v1.7.4`

#### Scenario: Vendor image written in full
- **WHEN** no entry is configured and a Project writes `uses: ghcr.io/helmfile/helmfile@v1.7.4`
- **THEN** it SHALL be allowed exactly as the `helmfile` alias allows it

#### Scenario: Image not allowed
- **WHEN** a Project names a fully qualified image no entry matches
- **THEN** validation SHALL fail naming the image, tag-spec and `TURNIP_ALLOWED_IMAGES`

### Requirement: Allowed images startup refusals
The Server SHALL refuse to start, naming every offending entry at once, when an entry of `TURNIP_ALLOWED_IMAGES` is malformed (not `tool:image@glob`, an unregistered tool, an empty image or glob, a malformed glob, an image carrying its own tag or digest, or an image that is not fully qualified), or when one image is listed for two different tools across the aliases and the list. Entries for the same tool SHALL be allowed to repeat or overlap an image.

#### Scenario: Unqualified image
- **WHEN** an entry reads `helmfile:org/helmfile-aws@*`
- **THEN** the Server SHALL exit at startup with a message containing `image "org/helmfile-aws" is not fully qualified; write its registry host (e.g. docker.io/org/helmfile-aws)`

#### Scenario: Several bad entries
- **WHEN** two entries are malformed
- **THEN** the startup error SHALL name both, separated by `; `

#### Scenario: Image claimed by two tools
- **WHEN** an entry lists `ghcr.io/helmfile/helmfile` for a tool other than `helmfile`
- **THEN** the Server SHALL refuse to start, saying the image is listed for more than one tool and naming each entry, the alias's marked `(built-in alias)`

### Requirement: Custom images run the tool in the image
A Project whose image is not its Plugin's vendor image SHALL run with that image as the Job's main container and turnip's runner binary supplied to it, whatever provisioning strategy the tool normally uses (see the `runner-job` capability). The image SHALL provide the tool on its `PATH` and everything the tool shells out to.

#### Scenario: Custom image for a copy-out tool
- **WHEN** a tool normally copies its binary out of the vendor image and a Project names a custom image for it
- **THEN** the Job SHALL run the tool inside the custom image instead

### Requirement: Pull policy
The container running the tool's image SHALL pull with `imagePullPolicy: Always` when the Job names a tag, and `IfNotPresent` when it names a digest. Containers running turnip's own Runner image SHALL keep the cluster's default policy.

#### Scenario: Plan by tag
- **WHEN** a plan runs for `uses: helmfile@v1.7.4`
- **THEN** the tool container SHALL use `ghcr.io/helmfile/helmfile:v1.7.4` with `imagePullPolicy: Always`

#### Scenario: Plan by digest
- **WHEN** a plan runs for a `uses:` naming a `sha256:` digest
- **THEN** the tool container SHALL use `<image>@sha256:<hex>` with `imagePullPolicy: IfNotPresent`

### Requirement: The apply runs what the plan ran
When a plan's result arrives, the Server SHALL read the repository digest of the image the plan's tool container ran, as Kubernetes reports it, and record it with the plan (the record itself belongs to the `locking` capability). Only a `<repo>@sha256:<64 hex>` image ID SHALL qualify; a runtime-local ID or an unreadable Pod SHALL record nothing, and the plan SHALL still succeed. Every mutating Operation SHALL run `<image>@<recorded digest>` with `IfNotPresent`, never the tag resolved again. A mutating command whose plan has no recorded digest SHALL be refused before any check run or Job exists, with the reason `the image this plan ran could not be recorded; re-plan`.

#### Scenario: Tag moves between plan and apply
- **WHEN** a plan ran `helmfile@v1.7.4` and the registry re-pushes that tag before the apply
- **THEN** the apply SHALL run the digest the plan's Pod ran, not the re-pushed image

#### Scenario: Digest not recorded
- **WHEN** a plan succeeded but its Pod reported no pullable repository digest
- **THEN** a later `apply` or `sync` for that Project SHALL be refused asking to re-plan

### Requirement: Diagnosing a recorded digest that cannot be pulled
When a mutating Operation never starts within the start timeout (see the `operation-lifecycle` capability) and its tool container is waiting with `ErrImagePull` or `ImagePullBackOff`, the diagnostic SHALL read `Job <name>: the image could not be pulled (<reason>). The image digest the plan recorded may no longer exist in the registry; re-plan`. A pull failure on a plan, or on a container running turnip's own image, SHALL get the generic stuck-container diagnostic instead.

#### Scenario: Digest deleted from the registry
- **WHEN** the digest a plan recorded is deleted from the registry and the apply's Pod cannot pull it
- **THEN** the apply SHALL fail when the start timeout expires, with the diagnostic saying to re-plan
