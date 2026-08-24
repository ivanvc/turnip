package jobs

import (
	"fmt"
	"slices"
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
	// versions is the known-good list for this tool, in preference
	// order; the first entry is the documented default used when a
	// Project's config specifies no version (Requirement 8.5).
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

// UnrecognizedVersionError reports a version not in the given tool's
// known-good list (Requirement 8.4).
type UnrecognizedVersionError struct {
	Tool, Version string
	Known         []string
}

func (e *UnrecognizedVersionError) Error() string {
	return fmt.Sprintf("jobs: unrecognized %s version %q (known versions: %v)", e.Tool, e.Version, e.Known)
}

// resolveVersion returns the version to provision for tool: requestedVersion
// itself if it's in that tool's known-good list, the tool's documented
// default if requestedVersion is empty, or an error identifying the
// unrecognized tool or version otherwise (Requirement 8.3-8.5).
func resolveVersion(tool, requestedVersion string) (string, error) {
	ti, ok := toolImages[tool]
	if !ok {
		return "", &UnrecognizedToolError{Tool: tool}
	}

	if requestedVersion == "" {
		return ti.versions[0], nil
	}

	if slices.Contains(ti.versions, requestedVersion) {
		return requestedVersion, nil
	}

	return "", &UnrecognizedVersionError{Tool: tool, Version: requestedVersion, Known: ti.versions}
}
