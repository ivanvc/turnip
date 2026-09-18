package jobs

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivanvc/turnip/internal/config"
)

// Only the clone reads the submodule mode, so the container that runs the
// tool must not carry it — the same rule the installation token follows,
// and it matters most under run-in-image, where that container is a vendor
// image turnip does not control.
func TestBuildJob_SubmoduleModeReachesOnlyTheCloneContainer(t *testing.T) {
	for _, tool := range []string{"helmfile", "terraform"} {
		t.Run(tool, func(t *testing.T) {
			params := testParams()
			params.Submodules = config.SubmodulesRecursive

			job, err := BuildJob(testProject(tool), params)
			require.NoError(t, err)

			clone := envMap(initContainerNamed(t, "clone", job))
			assert.Equal(t, config.SubmodulesRecursive, clone["TURNIP_CLONE_SUBMODULES"])

			main := envMap(mainContainer(t, job))
			assert.NotContains(t, main, "TURNIP_CLONE_SUBMODULES",
				"the tool's process has no use for the submodule mode")
		})
	}
}

// An empty value is meaningful rather than absent: the Runner reads it as
// top-level, so a Job whose Server never set one still initialises
// submodules instead of silently leaving an empty directory.
func TestBuildJob_EmptySubmoduleModeIsStillPresentOnTheCloneContainer(t *testing.T) {
	job, err := BuildJob(testProject("helmfile"), testParams())
	require.NoError(t, err)

	clone := envMap(initContainerNamed(t, "clone", job))
	assert.Contains(t, clone, "TURNIP_CLONE_SUBMODULES")
	assert.Empty(t, clone["TURNIP_CLONE_SUBMODULES"])
}
