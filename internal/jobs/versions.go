package jobs

import (
	"fmt"

	"github.com/ivanvc/turnip/internal/config"
)

// toolImage describes how to provision one IaC tool's CLI binary onto a
// Runner Job via an initContainer.
type toolImage struct {
	// image is a fmt.Sprintf template with one %s for the resolved
	// version.
	image string
	// binaryPath is where the tool's CLI binary lives inside that vendor
	// image, to be copied onto the shared volume.
	binaryPath string
	// versions are known-good example versions for this tool, in
	// preference order; the first entry is the documented default used
	// when a Project's config specifies no version (Requirement 8.5).
	// This is NOT an exhaustive allowlist: Requirement 8's own user story
	// is that turnip must never lag behind a tool's latest release the
	// way a bundled-binary approach would, so resolveVersion accepts any
	// explicit version matching versionPattern, whether or not it
	// appears here — gating on membership in this list would just
	// recreate that same bottleneck one layer up (turnip-side code
	// changes still gating adoption of a real vendor release), which
	// defeats the vendor-image initContainer design entirely. See
	// grpc-runner/design.md's "Version validation" section.
	//
	// Reproduced from the global design's "Tool Binary Provisioning"
	// section; re-verify by pulling and inspecting each vendor image at
	// implementation time, since a vendor could restructure their image
	// layout since that section was written.
	versions []string
}

var toolImages = map[string]toolImage{
	"terraform": {
		image:      "hashicorp/terraform:%s",
		binaryPath: "/bin/terraform",
		versions:   []string{"1.9.5", "1.9.4", "1.8.5"},
	},
	"pulumi": {
		image:      "pulumi/pulumi-base:%s",
		binaryPath: "/pulumi/bin/pulumi",
		versions:   []string{"3.130.0", "3.129.0", "3.128.0"},
	},
	"helmfile": {
		image:      "ghcr.io/helmfile/helmfile:v%s",
		binaryPath: "/usr/local/bin/helmfile",
		versions:   []string{"0.170.1", "0.169.2", "0.168.0"},
	},
}

// UnrecognizedToolError reports a tool with no known vendor image at all.
type UnrecognizedToolError struct {
	Tool string
}

func (e *UnrecognizedToolError) Error() string {
	return fmt.Sprintf("jobs: unrecognized tool %q", e.Tool)
}

// UnrecognizedVersionError reports a version that isn't syntactically a
// plausible version for the given tool (Requirement 8.4). It is not a
// report of "not in our list" — turnip keeps no exhaustive per-tool
// version list; see toolImage.versions.
type UnrecognizedVersionError struct {
	Tool, Version string
	// Examples are known-good example versions for this tool, included
	// in the error message purely as a hint of the expected shape — not
	// the set of values that would have been accepted.
	Examples []string
}

func (e *UnrecognizedVersionError) Error() string {
	return fmt.Sprintf(
		"jobs: %q doesn't look like a valid %s version (expected roughly semver, e.g. %v)",
		e.Version, e.Tool, e.Examples,
	)
}

// resolveVersion returns the version to provision for tool: the tool's
// documented default if requestedVersion is empty, requestedVersion
// itself if it's syntactically a plausible version for that tool, or an
// error identifying the unrecognized tool or malformed version otherwise
// (Requirement 8.3-8.5). It deliberately does not require
// requestedVersion to match any specific enumerated version — turnip has
// no fixed set of "supported" versions to gate on, only vendor images it
// asks for by tag; if a vendor doesn't actually publish that tag, the Job
// fails when its initContainer can't pull the image, surfaced as a normal
// operation failure rather than caught here.
func resolveVersion(tool, requestedVersion string) (string, error) {
	ti, ok := toolImages[tool]
	if !ok {
		return "", &UnrecognizedToolError{Tool: tool}
	}

	if requestedVersion == "" {
		return ti.versions[0], nil
	}

	// Defensive since Slice 13: internal/config rejects a malformed version
	// at parse time, which puts the error on the pull request rather than
	// here at Job-build time. The shape rule lives there because config is
	// a leaf package this one imports, so it cannot be borrowed the other
	// way round without a cycle.
	if !config.IsWellFormedVersion(requestedVersion) {
		return "", &UnrecognizedVersionError{Tool: tool, Version: requestedVersion, Examples: ti.versions}
	}

	return requestedVersion, nil
}
