package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func cloneConfigYAML(body string) []byte {
	return []byte("schemaVersion: " + SupportedSchemaVersion + "\n" +
		body +
		"projects:\n" +
		"  - directory: infrastructure\n" +
		"    uses: helmfile@v1.7.4\n")
}

// Requirements 2.6 and 2.7: the field is `submodules`, nested under a
// top-level `clone:` block beside `projects:`.
func TestParse_CloneSubmodulesAcceptsEveryMode(t *testing.T) {
	for _, mode := range []string{SubmodulesNone, SubmodulesTopLevel, SubmodulesRecursive} {
		cfg, err := Parse(cloneConfigYAML("clone:\n  submodules: "+mode+"\n"), testTools)

		require.NoErrorf(t, err, "mode %q should be valid", mode)
		assert.Equal(t, mode, cfg.Clone.Submodules)
	}
}

// Requirement 2.2: an unrecognized value is reported rather than silently
// ignored, against the file rather than a project, since `clone:` belongs
// to no project.
func TestParse_CloneSubmodulesRejectsAnUnknownMode(t *testing.T) {
	_, err := Parse(cloneConfigYAML("clone:\n  submodules: shallow\n"), testTools)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "clone.submodules")
	assert.Contains(t, err.Error(), "shallow")
	assert.Contains(t, err.Error(), configFileRef,
		"a file-level problem is reported against the file, not a project")
}

// An absent block is the common case and must stay valid: the Server's
// default then applies.
func TestParse_AbsentCloneBlockLeavesTheZeroValue(t *testing.T) {
	cfg, err := Parse(cloneConfigYAML(""), testTools)

	require.NoError(t, err)
	assert.Empty(t, cfg.Clone.Submodules)
}

// An explicitly empty value means "unset" too, rather than being a fourth
// mode that fails validation.
func TestParse_EmptyCloneSubmodulesIsUnsetNotInvalid(t *testing.T) {
	cfg, err := Parse(cloneConfigYAML("clone:\n  submodules: \"\"\n"), testTools)

	require.NoError(t, err)
	assert.Empty(t, cfg.Clone.Submodules)
}

// Adding the field is what makes `clone:` accepted at all — and the same
// strict decode keeps a typo inside the block from being ignored, which is
// also how an older Server rejects a file using this feature (Decision 7).
func TestParse_UnknownKeyInsideCloneIsRejected(t *testing.T) {
	_, err := Parse(cloneConfigYAML("clone:\n  submodule: recursive\n"), testTools)

	require.Error(t, err, "a typo inside clone: must not be silently ignored")
	assert.Contains(t, err.Error(), "submodule")
}
