# Requirements: Published Images, Under the Project's Organization (Slice 49)

## Introduction

turnip's images have never been published. The repository owner builds
and pushes them by hand. `release.yml` would publish on a `v*` tag, but
no tag has been pushed, so that path has never run. Every change the
owner wants in the homelab cluster costs a manual build and push.

The project now has a GitHub organization, `turnip-iac`, and this slice
moves everything that names the project to it at once: the repository,
the Go module path, the images, and the domain in turnip's Kubernetes
labels and Runner token audience. Doing it together, and before
anything is published, means no image, label or install ever carries
the old name, and nothing needs migrating.

**Immutable tags only** (decided 2026-09-30). An image tag names one
build forever: a push to `main` publishes its commit's SHA, a release
publishes its version, and nothing publishes `latest` or any other tag
that moves. A deployment pins what it runs, and what it runs cannot
change underneath it.

## Glossary

- **Image_Tag**: the tag an image is published under.
- **Commit_Tag**: an Image_Tag that is the full 40-character SHA of the
  commit the image was built from (decided 2026-09-30: never ambiguous,
  where a short SHA can collide as the repository grows; it is copied,
  not typed).
- **Version_Tag**: an Image_Tag that is a release's version, e.g.
  `v0.1.0`, exactly as its git tag.
- **Floating_Tag**: any Image_Tag that can later name a different
  build, such as `latest` or `main`.

## Requirements

### Requirement 1: The project lives in its organization

#### Acceptance Criteria

1. THE repository SHALL be transferred to `turnip-iac` by the
   repository owner, before the rest of this slice lands
2. THE Go module path SHALL be `github.com/turnip-iac/turnip`, and every
   import SHALL use it
3. EVERY link to the repository in the documentation, `SECURITY.md`,
   manifests and configuration SHALL name `github.com/turnip-iac/turnip`

### Requirement 2: One name for each image

#### Acceptance Criteria

1. THE images SHALL be `ghcr.io/turnip-iac/turnip/server` and
   `ghcr.io/turnip-iac/turnip/runner`
2. EVERY reference to an image, in `deploy/`, the release configuration,
   the Makefile and the documentation, SHALL use those names
3. THE Makefile SHALL NOT default to any other registry

*Rationale for 2.1 (decided 2026-09-30): each project in the
organization takes a path of its own: organization, project, component.
That avoids the stutter of `turnip-iac/turnip-server`, and leaves room
for anything else the organization hosts later without a bare
`turnip-iac/server` becoming ambiguous; renaming published images later
would cost far more than one path segment now. An OpenTofu image, the
first candidate, is not needed after all (Slice 7 copies the binary out
of the official image), but the room is kept. Multi-segment image names
are an established GHCR pattern.*

### Requirement 3: Every push to `main` publishes its commit

**User Story:** As the repository owner, I want every merge to `main`
to publish images I can deploy, so that I stop building and pushing them
by hand.

#### Acceptance Criteria

1. WHEN a commit is pushed to `main`, CI SHALL build and publish both
   images, each under its Commit_Tag
2. THE publish SHALL run only after the commit's build, tests and lint
   pass
3. A pull request SHALL build both images, for every architecture, and
   SHALL NOT publish them

*Rationale for 3.3: an image that fails to build for one architecture
should fail the pull request, not the push to `main` after it merges.*

### Requirement 4: A release publishes its version

#### Acceptance Criteria

1. WHEN a `v*` tag is pushed, THE release SHALL publish both images under
   the Version_Tag, and the install manifest and GitHub release as today
2. THE install manifest, and the release overlay a tagged checkout
   carries, SHALL reference the Version_Tag of both images

### Requirement 5: No floating tags

#### Acceptance Criteria

1. NOTHING SHALL publish a Floating_Tag: not `latest`, not `main`, not a
   partial version such as `v0.1`
2. EVERY image reference in `deploy/` and the documentation SHALL be a
   Commit_Tag, a Version_Tag, or a placeholder the reader must replace;
   none SHALL be a Floating_Tag
3. THE documentation SHALL say how to pin a deployment to a Commit_Tag
   and to a Version_Tag, and how to find the tag for a given commit or
   release

### Requirement 6: Both architectures

#### Acceptance Criteria

1. EVERY published image SHALL be a multi-architecture image for
   `linux/amd64` and `linux/arm64`, under the one Image_Tag

### Requirement 7: turnip's Kubernetes names use the organization's domain

#### Acceptance Criteria

1. turnip's Job labels SHALL use the prefix `turnip-iac.github.io/`
   (`turnip-iac.github.io/operation-id`, `turnip-iac.github.io/project`)
2. THE Runner's ServiceAccount token audience SHALL be
   `turnip-iac.github.io`
3. THE operator documentation SHALL state that an Operation running
   while the Server is upgraded across this change fails and must be
   run again, since its Job carries the old labels and its token the old
   audience

*Rationale for 7.1: a label prefix should be a domain the project
controls. The organization controls `turnip-iac.github.io` through its
GitHub Pages site; `turnip.ivan.vc` belongs to a person.*

*Rationale for 7.3: pre-1.0, with one pilot, supporting both names
would be code kept for one upgrade. Upgrading while nothing runs avoids
the failure entirely.*

### Requirement 8: What the repository owner does

#### Acceptance Criteria

1. THE slice SHALL list, as owner steps: transferring the repository;
   allowing the repository's Actions to publish packages to the
   organization; making both packages public after their first publish;
   and pushing a pre-release tag (e.g. `v0.1.0-rc.1`) to exercise the
   release path end to end before a real version

## Out of Scope

- **Signing images and publishing SBOMs** (cosign, attestations): worth
  doing once images are published, as its own slice.
- **Registries other than GHCR.**
- **A documentation site** on `turnip-iac.github.io`: the domain is used
  here only as the label and audience prefix.
- **Keeping the old label names or audience working** during an
  upgrade; Requirement 7.3 explains why.
