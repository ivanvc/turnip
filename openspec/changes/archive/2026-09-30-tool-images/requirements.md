# Requirements: Running a Tool Image the Operator Approves (Slice 44)

## Introduction

turnip runs each tool in its vendor's published image. Since Slice 45,
each Plugin declares its image repository (`provisioning.Spec.Image`) and
`uses: helmfile@v1.7.4` runs `ghcr.io/helmfile/helmfile:v1.7.4`, the
version used verbatim as the tag. But the repository is fixed in the
Plugin, and nothing can change which image runs. That blocks two real
needs:

- **A tool beside the tool.** Reaching an EKS cluster turnip is not running
  in needs only `aws` on the tool's PATH: the committed kubeconfig's
  `exec` stanza does the rest (`docs/configuration.md`, "Targeting an EKS
  cluster turnip is not running in"). The vendor's helmfile image has no
  AWS CLI, so an image built `FROM` it with the CLI added is the whole
  remaining gap for migrating a repository whose clusters are outside the
  one turnip runs in.
- **A build the vendor has not released**, such as a fix merged upstream
  but not yet published.

An image is code that runs with the Runner's cloud and cluster
credentials, and `turnip.yaml` is read from the pull request's own head
commit. So an image named in `turnip.yaml` runs only if the operator has
allowed it, and turnip states in public what allowing one means. **turnip
has no stated security model yet** (no `SECURITY.md`); it is written first,
as this slice's first requirement, and covers every opt-in, not only
images.

**The model, settled 2026-09-24 one decision at a time:**

- `uses:` names either one of turnip's **built-in aliases**
  (`helmfile@v1.7.4`) or a **fully qualified image**
  (`ghcr.io/org/helmfile-aws@latest`), with one exact tag or digest after
  the `@`. It never names the tool: the matching entry supplies it.
- The built-in aliases are defined by each tool's Plugin, in the same
  format as the operator's list, and allow only full versions or digests
  of the vendor's image. An operator cannot redefine them, so
  `helmfile@v1.7.4` means the same thing on every turnip Server. This
  builds on Slice 45, which makes each Plugin the one place a tool is
  defined.
- Anything else runs only if the Server's **Access_List** has a matching
  entry, which declares the tool and a glob for the tags it allows.
- Whatever the tag, the apply runs **the exact image digest the plan ran**,
  recorded from the plan Pod's own status. That makes any tag, `latest`
  included, safe between plan and apply, so what a tag may be is left
  entirely to the operator's globs, and turnip does no version
  classification of its own.

The roadmap Backlog entries "Running a tool build turnip does not ship",
"A stated security model", and "Resolving a floating tool version" carry
the analysis this builds on.

## Glossary

- **Alias**: a registered Plugin's name (a Tool_Name, Slice 45; today
  `helmfile`), standing for its vendor's image as the Plugin defines it.
- **Image_Reference**: a fully qualified image, registry host included.
- **Tag_Spec**: what follows the last `@` in `uses:`. A **Digest** when it
  is `sha256:` followed by 64 hexadecimal characters, otherwise a **Tag**.
- **Entry**: one line of the Access_List or of a Plugin's Alias,
  `tool:image@glob`, allowing an image, for one tool, with the Tag_Specs
  its glob matches.
- **Access_List**: the operator's Entries, in the Server's configuration.
- **Image_Digest**: the digest of the image a Pod actually ran, as
  Kubernetes reports it in the container's `imageID`.

## Requirements

### Requirement 1: A stated security model

**User Story:** As an operator, I want turnip to say plainly what its
opt-ins cost, so that I enable them knowingly; and as a security
researcher, I want to tell a designed-in capability from a vulnerability
without re-deriving the reasoning.

#### Acceptance Criteria

1. THE repository SHALL have a `SECURITY.md` at its root
2. IT SHALL state the threat model: `turnip.yaml` and the IaC code are
   read from the pull request's own head commit, and the tool runs in a
   Pod holding cloud and cluster credentials, so anyone who can get code
   into a pull request turnip plans can run it with those credentials
3. IT SHALL list every opt-in that grants capability by design (today
   `runner.serviceAccount`, `clone.submodules`, and this slice's
   Access_List), each default-off, with what enabling it costs
4. IT SHALL state what turnip does consider a vulnerability: a gate that
   fails open, a default that grants more than documented, a leak
   reachable with no opt-in
5. IT SHALL say how to report one: through GitHub's private vulnerability
   reporting on the repository (its Security tab), with no email address
   published. Enabling that setting is the repository owner's step
6. Each opt-in's own documentation SHALL link to it

### Requirement 2: `uses:` names an Alias or an image, with one exact Tag_Spec

#### Acceptance Criteria

1. `uses:` SHALL be `<alias>@<tag-spec>` or `<image-reference>@<tag-spec>`
2. THE Tag_Spec SHALL follow the last `@`, and SHALL be used verbatim: no
   `v` is added or removed
3. A Digest SHALL use `sha256`; any other algorithm SHALL be a validation
   error
4. `uses:` SHALL NOT name a tool, and SHALL NOT hold a pattern or a version
   range: one exact Tag or Digest
5. AN Image_Reference SHALL include its registry host; an unqualified name
   that is not an Alias SHALL be a validation error
6. A reference written in registry form with a `:tag`
   (`ghcr.io/org/helmfile-aws:1.7.4`) SHALL be a validation error that
   shows the corrected line (`ghcr.io/org/helmfile-aws@1.7.4`)
7. turnip SHALL hand Kubernetes a fully qualified reference only: an
   Alias expanded to its vendor's image, a Tag as `image:tag`, a Digest as
   `image@sha256:…`
8. turnip SHALL keep no version-shape check of its own: which Tag_Specs a
   `uses:` line may name is decided only by an Alias's or the Access_List's
   Entries. The shape check Slice 45 kept (`isWellFormedVersion` in
   `internal/config`, and its "floating tags are not accepted" error)
   SHALL be removed

*Rationale for 2.2: vendors differ. helmfile's tags carry a `v`
(`v1.7.4`); terraform's and pulumi's do not (`1.16.4`, `3.264.0`). Using
the Tag verbatim is the only rule that is right for every image, including
an operator's own. Slice 45 dropped turnip's `v` handling, so today
`helmfile@1.7.4` passes validation and fails at image pull; this slice's
Alias globs (`v*.*.*`) make it a validation error naming
`helmfile@v1.7.4`.*

*Rationale for 2.8: Slice 45 kept the shape check only because nothing yet
made a floating tag safe between plan and apply. Requirement 6 does, and
the Alias's Entries enforce the same shape for `helmfile@…` (`v*.*.*`), so
a second, separate check would only disagree with the Entries — refusing
`ghcr.io/org/helmfile-aws@latest` before the Access_List that allows it is
consulted.*

*Rationale for 2.4: the `@` form exists so one field carries the image and
its version. A pattern or range belongs where approval is decided, in the
Access_List, not in the line that selects what runs.*

*Rationale for 2.5 and 2.7: an unqualified reference is resolved by the
node's container runtime, which may search Docker Hub or a configured list
of registries. The same line must not mean different images on different
clusters.*

*Rationale for 2.6: accepting `:` as a synonym would leave two spellings of
one thing. A validation error on the pull request's first plan, with the
fix spelled out, turns the copy-paste mistake into a one-line edit. A
Digest pasted from a registry (`…@sha256:…`) is already valid as written.*

### Requirement 3: Built-in aliases, defined by each Plugin

This builds on Slice 45, which made each Plugin the source of its own
provisioning, including its image repository (`provisioning.Spec.Image`),
and the Plugin_Registry the only list of tools. What this slice adds is
**which tags that image may take** under the Plugin's Alias.

#### Acceptance Criteria

1. EACH Plugin SHALL declare the Tag_Spec globs its Alias allows. Its
   Alias's Entries SHALL be formed from its registered name, the image its
   provisioning Spec declares, and those globs, in the Access_List's format
   and matched by the same code, so the image is stated once. For the
   Helmfile Plugin, globs `v*.*.*` and `sha256:*`, forming
   `helmfile:ghcr.io/helmfile/helmfile@v*.*.*` and
   `helmfile:ghcr.io/helmfile/helmfile@sha256:*`
2. THE Alias SHALL be the Plugin's registered name
3. AN Access_List Entry's tool SHALL be a registered Tool_Name
4. AN operator SHALL NOT be able to redefine an Alias
5. `alias@latest`, and any other Tag_Spec an Alias's Entries do not match,
   SHALL be refused; the vendor's image with such a tag is reachable only
   as a full Image_Reference the Access_List allows

*Rationale for 3.1: full versions only, so an Alias cannot quietly move to
a new vendor release with no change to `turnip.yaml`; digests too, because
a Digest pins more tightly than any version. Two plain globs rather than
one with alternation, so every Entry reads on its own. Declared by the
Plugin, so adding a tool stays one place; formed from the Spec's image, so
the Alias and the image the Job runs cannot name different repositories.*

*Rationale for 3.4: a redefinable Alias would make `helmfile@v1.7.4` mean
different images on different Servers, hidden from the file that uses it.
An operator who wants the vendor's `latest` allows the full reference, and
the repository writes it out, where a reviewer sees it.*

*Terraform and Pulumi have no Plugin yet (Slice 7), so no Alias either,
and `uses:` naming them is a validation error (Slice 45). Their Alias
arrives with their Plugins, whose images must be fully qualified
(`docker.io/hashicorp/terraform`, `docker.io/pulumi/pulumi-base`).*

### Requirement 4: The Access_List

#### Acceptance Criteria

1. THE Server SHALL read an Access_List from its configuration; empty,
   the default, SHALL permit only the built-in aliases
2. EACH Entry SHALL be `tool:image@glob`. THE text before the first `:`
   SHALL be read as the tool only when it is one of turnip's tool names;
   otherwise the Entry SHALL be invalid
3. THE glob SHALL be a plain glob over the Tag_Spec's text: `*` matches
   any characters, `?` one, `[…]` a class. So `@*` allows any Tag or
   Digest, `@1.*.*` full 1.x versions, `@latest` exactly `latest`, and
   `@sha256:*` digests only
4. THE Access_List SHALL define no aliases
5. AN invalid Entry, or one image listed for two different tools (by
   the Access_List or an Alias), SHALL stop the Server from starting,
   naming them
6. A `uses:` image and Tag_Spec matching no Entry SHALL be a validation
   error of `turnip.yaml`, naming the image and that the operator must
   allow it, reported like any other (a comment on the pull request and a
   failed `turnip` check)
7. THE tool a Project runs SHALL be the one its matching Entry declares
8. THE Access_List SHALL be operator configuration only, never settable
   from `turnip.yaml`

*Rationale for 4.2: a registry port (`registry.local:5000/…`) also puts a
`:` before the first `/`. Recognizing only turnip's own tool names keeps
the two apart, and the rule applies only to the Server's configuration:
`uses:` never carries a tool.*

*Rationale for 4.3: which tags an image may use is the operator's to say,
and a glob says it without a regular expression. Blocking floating tags
needs no special rule: `@1.*.*` cannot match `1.16` (terraform's floating
minor tag) or `latest`.*

*Rationale for 4.5 and 4.7: the operator built or vetted the image and
knows which tool it runs. If one image could match Entries for two tools,
turnip could not tell which Plugin to drive it with. The check is per
image rather than per image and tag because whether two globs share a
match is not worth deciding, and an image running different tools by tag
is not a case anyone has raised. Entries for the same tool may overlap
freely.*

*Rationale for 4.6, amended during design: first written as a refusal per
Project, before Slice 45 made a tool with no Plugin a validation error. An
unallowed image is the same kind of problem, the file asking this Server
for something it will not run, and resolving `uses:` needs the Entries
anyway, so it is known when the file is parsed.*

### Requirement 5: A custom image runs the tool itself

#### Acceptance Criteria

1. A Project whose `uses:` is an Image_Reference SHALL run its tool inside
   that image, with turnip's runner binary supplied to it
   (`provisioning.RunInImage`, as Helmfile runs in its vendor image),
   whatever Strategy its tool's Plugin declares
2. THE documentation SHALL state what such an image must provide: the
   tool on its PATH, and anything the tool's Plugin shells out to

### Requirement 6: The apply runs what the plan ran

#### Acceptance Criteria

1. WHEN a plan's Pod starts, THE Server SHALL record its Image_Digest with
   the plan on the Lock, for every plan
2. A Mutating_Operation SHALL run the recorded Image_Digest, not resolve
   the Tag again
3. WHERE the recorded digest can no longer be pulled, THE Operation SHALL
   fail with a message saying to re-plan
4. A Pod whose `uses:` names a Tag SHALL pull on every start
   (`imagePullPolicy: Always`); one naming a Digest SHALL use the node's
   cache

*Rationale for 6.1 and 6.2: a Tag can move (`latest` by design, and any
tag when a registry re-pushes it), so what the plan ran is the only thing
the apply can safely repeat. Recording the digest rather than the tool's
version also covers a custom image whose other contents change while the
tool stays the same. Kubernetes reports it in the Pod's status, which
turnip already reads, so no registry access or credentials are involved.*

*Rationale for 6.4: the plan should see what the Tag means now, since the
apply is pinned to whatever that turns out to be. The cost is a manifest
check per Job; a Digest cannot change, so it needs none.*

### Requirement 7: Pulling from private registries

#### Acceptance Criteria

1. THE documentation SHALL explain how a Runner Pod pulls a private image:
   through the node's own credentials (for example ECR on EKS), or an
   `imagePullSecrets` on the Runner's ServiceAccount
2. turnip SHALL NOT handle registry credentials itself

### Requirement 8: Documentation

#### Acceptance Criteria

1. `docs/configuration.md` SHALL document the `uses:` forms, the built-in
   aliases and their allowed Tag_Specs, and the Access_List, linking to
   `SECURITY.md`
2. IT SHALL show the corrected form next to a pasted `:tag` reference, and
   how to allow the vendor's own `latest`
3. THE EKS recipe SHALL show a helmfile image with the AWS CLI, and drop
   its "not usable in turnip yet" warning

## Out of Scope

- **Operator-defined aliases.** Considered, and rejected because an Alias
  that hides its image lets the same line mean different images on
  different Servers; the Access_List allows full references only.
- **Version comparison** (semver ranges such as `~1.2`) in `uses:` or in
  the Access_List. Globs cover every case raised, and `uses:` holds one
  exact Tag_Spec by design.
- **Digest algorithms other than `sha256`.**
- **Deriving a tool's version from the IaC code** (Terraform's
  `required_version`).
- **Server-side per-repository Access_Lists**, the Backlog's
  per-repository server configuration.
