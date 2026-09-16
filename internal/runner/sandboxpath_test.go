package runner

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// The real shape this exists for: helmfile quotes absolute paths inside a
// long wrapped error, twice over, and every one of them names a directory
// that only existed for this run.
func TestStripSandboxPath_RepositoryRelativePathsSurvive(t *testing.T) {
	const dir = "/tmp/turnip-runner-2858245619"
	in := `failed to load environment values file "` + dir + `/environments/secrets.yaml.gotmpl": ` +
		`failed to render [` + dir + `/environments/secrets.yaml.gotmpl]`

	got := stripSandboxPath(dir, in)

	assert.NotContains(t, got, dir, "every occurrence is rewritten, not just the first")
	assert.Contains(t, got, `"environments/secrets.yaml.gotmpl"`)
	assert.Contains(t, got, `[environments/secrets.yaml.gotmpl]`)
}

func TestStripSandboxPath_BareDirectoryBecomesDot(t *testing.T) {
	const dir = "/tmp/turnip-runner-1"
	assert.Equal(t, "chdir .: permission denied", stripSandboxPath(dir, "chdir "+dir+": permission denied"))
}

func TestStripSandboxPath_EmptyDirIsANoop(t *testing.T) {
	assert.Equal(t, "unchanged /tmp/whatever", stripSandboxPath("", "unchanged /tmp/whatever"))
}

func TestStripSandboxPath_UnrelatedTextUntouched(t *testing.T) {
	const dir = "/tmp/turnip-runner-1"
	in := "Error: cannot re-use a name that is still in use (release: my-app)"
	assert.Equal(t, in, stripSandboxPath(dir, in))
}

// A different Runner's directory must not be rewritten: only this run's
// own sandbox is turnip's to hide.
func TestStripSandboxPath_OnlyThisRunsDirectory(t *testing.T) {
	got := stripSandboxPath("/tmp/turnip-runner-1", "see /tmp/turnip-runner-2/environments")
	assert.Equal(t, "see /tmp/turnip-runner-2/environments", got)
}
