package config

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func asValidationErrors(t *testing.T, err error) ValidationErrors {
	t.Helper()
	var verrs ValidationErrors
	require.ErrorAs(t, err, &verrs)
	return verrs
}

func TestParse_MissingRequiredFields(t *testing.T) {
	tests := []struct {
		name  string
		yaml  string
		field string
	}{
		{
			name:  "missing directory",
			yaml:  "schemaVersion: v1alpha1\nprojects:\n  - name: vpc\n    tool: terraform\n",
			field: "directory",
		},
		{
			name:  "missing tool",
			yaml:  "schemaVersion: v1alpha1\nprojects:\n  - name: vpc\n    directory: infra/vpc\n",
			field: "tool",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse([]byte(tt.yaml))
			require.Error(t, err)
			verrs := asValidationErrors(t, err)

			found := false
			for _, v := range verrs {
				if v.Field == tt.field {
					found = true
				}
			}
			assert.True(t, found, "no ValidationError for field %q in %v", tt.field, verrs)
		})
	}
}

func TestParse_MissingNameAndDirectoryRefersToProjectByIndex(t *testing.T) {
	data := []byte("schemaVersion: v1alpha1\nprojects:\n  - tool: terraform\n")

	_, err := Parse(data)
	require.Error(t, err)
	verrs := asValidationErrors(t, err)

	found := false
	for _, v := range verrs {
		if v.ProjectRef == "projects[0]" && v.Field == "directory" {
			found = true
		}
	}
	assert.True(t, found, "no directory ValidationError referencing projects[0] found in %v", verrs)
}

func TestParse_UnsupportedTool(t *testing.T) {
	data := []byte("schemaVersion: v1alpha1\nprojects:\n  - name: vpc\n    directory: infra/vpc\n    tool: cloudformation\n")

	_, err := Parse(data)
	require.Error(t, err)
	verrs := asValidationErrors(t, err)
	require.Len(t, verrs, 1)

	msg := verrs[0].Message
	for _, tool := range []string{ToolTerraform, ToolPulumi, ToolHelmfile} {
		assert.Contains(t, msg, tool)
	}
}

func TestParse_DuplicateProjectNames(t *testing.T) {
	data := []byte(`
schemaVersion: v1alpha1
projects:
  - name: vpc
    directory: infra/vpc-a
    tool: terraform
  - name: vpc
    directory: infra/vpc-b
    tool: terraform
`)

	_, err := Parse(data)
	require.Error(t, err)
	verrs := asValidationErrors(t, err)

	found := false
	for _, v := range verrs {
		if strings.Contains(v.Message, "duplicate") {
			found = true
		}
	}
	assert.True(t, found, "no duplicate-name ValidationError found in %v", verrs)
}

func TestParse_MultipleSimultaneousViolations(t *testing.T) {
	data := []byte(`
schemaVersion: v1alpha1
projects:
  - directory: infra/a
  - name: b
    directory: infra/b
    tool: terraform
  - name: b
    directory: infra/c
    tool: nope
`)

	_, err := Parse(data)
	require.Error(t, err)
	verrs := asValidationErrors(t, err)
	assert.GreaterOrEqual(t, len(verrs), 3, "want at least 3 violations (missing tool, missing name, duplicate name, bad tool): %v", verrs)
}

// The Runner reads its own configuration out of TURNIP_* and finds its
// tool binary through PATH, so a Project setting either would be
// reconfiguring the Runner rather than the tool it runs.
func TestParse_ReservedEnvNamesRejectedTogether(t *testing.T) {
	data := []byte(`
schemaVersion: v1alpha1
projects:
  - name: web
    directory: infra/web
    tool: helmfile
    env:
      TURNIP_SERVER_ADDR: elsewhere
      PATH: /nowhere
      AWS_PROFILE: untouched
`)

	_, err := Parse(data)
	require.Error(t, err)
	verrs := asValidationErrors(t, err)
	require.Len(t, verrs, 2, "both reserved names reported, and the legal one left alone: %v", verrs)

	assert.ElementsMatch(t,
		[]string{`env["PATH"]`, `env["TURNIP_SERVER_ADDR"]`},
		[]string{verrs[0].Field, verrs[1].Field})
}

// The prefix is what's reserved, not a fixed list of known variables —
// a name turnip doesn't use today is still rejected.
func TestParse_UnknownTurnipPrefixedEnvNameStillRejected(t *testing.T) {
	data := []byte("schemaVersion: v1alpha1\nprojects:\n  - directory: d\n    tool: helmfile\n    env:\n      TURNIP_NOT_A_REAL_VARIABLE: x\n")

	_, err := Parse(data)
	require.Error(t, err)
	verrs := asValidationErrors(t, err)
	require.Len(t, verrs, 1)
	assert.Contains(t, verrs[0].Message, "reserved")
}

func TestParse_OrdinaryEnvNamesAccepted(t *testing.T) {
	data := []byte("schemaVersion: v1alpha1\nprojects:\n  - directory: d\n    tool: helmfile\n    env:\n      AWS_PROFILE: prod\n      PATHOLOGICAL: not-PATH\n")

	_, err := Parse(data)
	require.NoError(t, err, "only PATH exactly is reserved, not names that merely start with it")
}

func TestParse_InvalidGlobPattern(t *testing.T) {
	data := []byte(`
schemaVersion: v1alpha1
projects:
  - name: vpc
    directory: infra/vpc
    tool: terraform
    whenModified:
      - "infra/vpc/["
`)

	_, err := Parse(data)
	require.Error(t, err)
	verrs := asValidationErrors(t, err)

	found := false
	for _, v := range verrs {
		if strings.HasPrefix(v.Field, "whenModified") {
			found = true
		}
	}
	assert.True(t, found, "no whenModified ValidationError found in %v", verrs)
}
