# Requirements: Per-Tool Provisioning (Slice 14)

## Introduction

turnip provisions an IaC tool by running the vendor's image as an
initContainer and copying one binary out of it onto a shared volume. That
rests on an assumption nobody wrote down: **a tool is a single static
binary.**

The pilot falsified it:

```
error determining helm version: unexpected error:
exec: "helm": executable file not found in $PATH
```

helmfile is not a binary, it is a *runtime*. It shells out to `helm`;
`helmfile diff` needs the helm-diff plugin; helm-secrets needs `sops`; and
helm finds its plugins through `$HELM_DATA_HOME`, which the vendor image
sets and turnip's Runner does not. The vendor image ships seven binaries
in `/usr/local/bin` and four plugins installed into a helm data directory.

Copying more paths would work for a while. It would also make turnip's
provisioning table a mirror of someone else's Dockerfile, re-creating in a
new form the very bottleneck the vendor-image design exists to avoid: a
vendor reorganising their image would break turnip, and adopting a tool
release would again wait on a turnip change.

Meanwhile the original strategy remains exactly right for tools that *are*
a single static binary — Terraform is one — so it stays.

So the mechanism is not global. This slice makes provisioning a per-tool
property with two strategies, and moves repository cloning into an
initContainer so one Runner binary serves both.

**Global requirements**: amends Requirement 14.2a, which specifies the
copy-one-binary mechanism as though it were the only way a Runner Job can
obtain its tool.

**Explicitly out of scope**, each deferred to named future work:

- **Pulumi's provisioning strategy.** Pulumi has its own slice. Its CLI
  orchestrates separate language-host binaries and runtime-fetched provider
  plugins, and running a program needs a language runtime chosen by the
  user's code — so it is very unlikely to be a copy-out tool, but deciding
  that belongs with the work that makes Pulumi actually run.
- **Installing helm plugins turnip's consumers ask for** beyond those the
  vendor image already ships. Running in the vendor image makes its
  bundled plugins available; a repository needing a plugin the vendor does
  not bundle is a separate problem.
- **Cloud CLIs** (`aws`, `gcloud`, `kubelogin`) — unchanged from Slice 12.
- **Remote-cluster authentication** — unchanged from Slice 12.
- **Adding OpenTofu as a supported tool** — belongs with Slice 7, which
  adds the Terraform Plugin. OpenTofu is Terraform-compatible, so whatever
  Plugin runs one runs the other, and a tool nothing can execute yet has
  nowhere useful to live until then. Adding it here would be scope that
  delays the helmfile MVP.
- **Running Terraform at all.** It is recognised by the config parser but
  has no Plugin; that is Slice 7's work, and this slice does not change it.

## Glossary

Terms additional to the global spec's glossary:

- **Provisioning_Strategy**: how a Runner Job obtains an IaC_Tool. One of
  **copy-out** (an initContainer copies the tool's binary from the vendor
  image onto a shared volume, and turnip's Runner image executes it) or
  **run-in-image** (the Runner Job's main container *is* the vendor image,
  and turnip's runner binary is supplied to it).

## Requirements

### Requirement 1: Provisioning is a per-tool property

**User Story:** As a maintainer, I want each tool provisioned the way that
tool actually works, so that one tool's packaging does not dictate every
other tool's.

#### Acceptance Criteria

1. EACH supported IaC_Tool SHALL declare its Provisioning_Strategy
2. THE Server SHALL NOT apply a single strategy to every tool
3. WHERE a tool is a single self-contained binary, its strategy SHALL be
   copy-out
4. WHERE a tool requires helper binaries, plugins, or environment the
   vendor image provides, its strategy SHALL be run-in-image
5. THE strategy SHALL be visible in the generated Job spec — an operator
   reading a Runner Pod SHALL be able to tell which was used, without
   consulting turnip's source

### Requirement 2: Running a tool inside its vendor image

**User Story:** As a developer, I want a tool that needs its own ecosystem
to run in the image its vendor built, so that it behaves the way its own
documentation says it does.

#### Acceptance Criteria

1. WHERE a tool's strategy is run-in-image, THE Runner Job's main
   container SHALL use the vendor's image for the resolved tool version
2. THE turnip runner binary SHALL be supplied to that container by an
   initContainer running turnip's own image, and the container's command
   SHALL be that binary
3. THE tool SHALL run with the vendor image's own `PATH`, environment and
   `HOME`. THE Server SHALL NOT reconstruct them, and SHALL NOT copy the
   tool's helper binaries, plugins or configuration out of the image
4. A Project's `runner.env` SHALL continue to reach the tool's process,
   and SHALL be applied on top of the vendor image's environment rather
   than replacing it
5. THE Server SHALL NOT require the vendor image to contain a shell, git,
   or any utility beyond the tool itself

### Requirement 3: Cloning moves to an initContainer

**User Story:** As a maintainer, I want the Runner to depend on nothing but
its own binary, so that it can run inside an image turnip does not control.

#### Acceptance Criteria

1. THE repository SHALL be cloned by an initContainer running turnip's own
   image, into the workspace volume, before the main container starts
2. THE Runner SHALL require no external command other than the IaC_Tool
   itself
3. IF the clone fails, THEN THE Server SHALL report it on the pull request
   with the underlying error message, and SHALL NOT wait for the
   start-timeout sweep to report it as a generic timeout — today a clone
   failure is reported immediately by the Runner, and moving it must not
   cost that
4. THE cloned workspace SHALL be readable by the main container whatever
   user the vendor image runs as
5. THE Runner's PATH composition for the tools directory (Slice 12's
   `TURNIP_TOOLS_DIR`) SHALL apply only to the copy-out strategy, since a
   run-in-image tool is already on the image's own PATH

### Requirement 4: Documentation

**User Story:** As an operator, I want to know which strategy a tool uses,
so that a Runner Pod's shape is not a surprise.

#### Acceptance Criteria

1. `docs/configuration.md` SHALL state, per tool, how it is provisioned
2. `docs/troubleshooting.md` SHALL cover a missing helper binary or plugin,
   since that is the failure this slice exists to eliminate and the next
   adopter will hit its neighbours
3. THE documentation SHALL state that a run-in-image tool's plugins and
   helpers come from the vendor image, so that adding one means choosing an
   image that has it rather than configuring turnip
