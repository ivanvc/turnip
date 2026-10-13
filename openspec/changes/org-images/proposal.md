# Proposal

## Why

turnip's images have never been published. The repository owner builds
and pushes them by hand, and the release workflow, which would publish
on a version tag, has never run because no tag has been pushed.

The project now has a GitHub organization, `turnip-iac`. Moving
everything that names the project there in one change, before anything
is published, means no image, label or install ever carries the old
name and nothing needs migrating.

This is roadmap Slice 49, carried over from its Kiro draft. The drafted
requirements travel with this change as `kiro-requirements.md` (Kiro's
format, unchanged).

## What Changes

- **BREAKING** The Go module path becomes
  `github.com/turnip-iac/turnip`, and every link to the repository
  names it
- The images are `ghcr.io/turnip-iac/turnip/server` and
  `ghcr.io/turnip-iac/turnip/runner`, and every reference uses those
  names; the Makefile defaults to no other registry
- Every push to `main` publishes both images under the commit's full
  SHA, after its build, tests and lint pass. A pull request builds both
  images for every architecture and publishes nothing
- A version tag publishes both images under that version, with the
  install manifest and the GitHub release
- No tag that moves is ever published: not `latest`, not `main`, not a
  partial version. Every image reference in `deploy/` and the
  documentation is a commit tag, a version tag or a placeholder
- Every published image is one multi-architecture image, for
  `linux/amd64` and `linux/arm64`
- **BREAKING** turnip's Job labels use the prefix
  `turnip-iac.github.io/`, and the Runner identities' token audiences
  use that domain. The draft predates the separate clone identity, so
  it names one audience; both (`turnip.ivan.vc` and
  `turnip.ivan.vc/clone` today) move. An Operation running while the
  Server is upgraded across this change fails and must be run again

## Decisions carried over (2026-09-30)

- **Immutable tags only.** A deployment pins what it runs, and what it
  runs cannot change underneath it.
- **The full commit SHA as the tag**, never a short one, which can
  collide as the repository grows.
- **A path per component under the project** (`turnip-iac/turnip/server`),
  leaving room for anything else the organization hosts.
- **No support for the old labels and audience during the upgrade**:
  pre-1.0, with one pilot, it would be code kept for one upgrade.

## Not in this change

- Signing images and publishing SBOMs: a change of its own once images
  are published
- Registries other than GHCR
- A documentation site on `turnip-iac.github.io`: the domain is used
  only as the label and audience prefix

## Capabilities

### New Capabilities
- `published-images`: which images are published, when, under which tags and for which architectures

### Modified Capabilities
- `deployment`: the image names and tags the manifests reference
- `runner-job`: the Job label prefix
- `runner-security`: the Runner identities' audiences

## Impact

Every Go file (the module path), `go.mod`, `proto/` and the generated
code, `.github/workflows/` (`ci.yml`, `release.yml`), the Makefile,
`build/`, `deploy/`, `internal/jobs` (labels and audiences),
`internal/runnerauth`, every documentation page that names an image or
the repository, and `SECURITY.md`.

Repository owner steps, outside the tasks: transfer the repository to
the organization (first); allow its Actions to publish packages to the
organization; make both packages public after their first publish; push
a pre-release tag to exercise the release path before a real version.
