package runner

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// The real shape this exists for: helmfile quotes absolute paths inside a
// long wrapped error, twice over, and every one of them is turnip's
// workspace rather than anything the reviewer would recognize.
func TestStripWorkspacePath_RepositoryRelativePathsSurvive(t *testing.T) {
	const dir = "/turnip/src"
	in := `failed to load environment values file "` + dir + `/environments/secrets.yaml.gotmpl": ` +
		`failed to render [` + dir + `/environments/secrets.yaml.gotmpl]`

	got := stripWorkspacePath(dir, in)

	assert.NotContains(t, got, dir, "every occurrence is rewritten, not just the first")
	assert.Contains(t, got, `"environments/secrets.yaml.gotmpl"`)
	assert.Contains(t, got, `[environments/secrets.yaml.gotmpl]`)
}

func TestStripWorkspacePath_BareDirectoryBecomesDot(t *testing.T) {
	const dir = "/turnip/src"
	assert.Equal(t, "chdir .: permission denied", stripWorkspacePath(dir, "chdir "+dir+": permission denied"))
}

func TestStripWorkspacePath_EmptyDirIsANoop(t *testing.T) {
	assert.Equal(t, "unchanged /tmp/whatever", stripWorkspacePath("", "unchanged /tmp/whatever"))
}

func TestStripWorkspacePath_UnrelatedTextUntouched(t *testing.T) {
	const dir = "/turnip/src"
	in := "Error: cannot re-use a name that is still in use (release: my-app)"
	assert.Equal(t, in, stripWorkspacePath(dir, in))
}

// The temporary-directory fallback is stripped just the same — the
// function takes the directory as a parameter, so a fixed mount path and
// a per-run temporary directory behave identically.
func TestStripWorkspacePath_TemporaryFallbackDirectory(t *testing.T) {
	const dir = "/tmp/turnip-runner-2858245619"
	assert.Equal(t, "see environments/prod", stripWorkspacePath(dir, "see "+dir+"/environments/prod"))
}

// A different directory must not be rewritten: only this run's own
// workspace is turnip's to hide.
func TestStripWorkspacePath_OnlyThisRunsDirectory(t *testing.T) {
	got := stripWorkspacePath("/tmp/turnip-runner-1", "see /tmp/turnip-runner-2/environments")
	assert.Equal(t, "see /tmp/turnip-runner-2/environments", got)
}
