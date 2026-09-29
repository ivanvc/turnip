package orchestrator

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivanvc/turnip/internal/config"
)

// projectWithServiceAccount builds a Project the way Parse would have left
// one — Tool is derived, and applyDefaults runs only inside Parse.
func projectWithServiceAccount(name string) config.Project {
	p := config.Project{Name: "web", Directory: "infra/web", Uses: "helmfile", Tool: "helmfile"}
	if name != "" {
		p.Runner.ServiceAccount = name
	}
	return p
}

func allowServiceAccount(allowed bool) map[string]bool {
	if allowed {
		return map[string]bool{overrideServiceAccount: true}
	}
	return map[string]bool{}
}

func TestResolveServiceAccount_NoRequestUsesServerDefault(t *testing.T) {
	for _, allow := range []bool{false, true} {
		got, err := resolveServiceAccount(projectWithServiceAccount(""), "turnip-runner", allowServiceAccount(allow))
		require.NoError(t, err)
		assert.Equal(t, "turnip-runner", got, "allowed=%v", allow)
	}
}

func TestResolveServiceAccount_NoRequestNoDefaultIsEmpty(t *testing.T) {
	got, err := resolveServiceAccount(projectWithServiceAccount(""), "", allowServiceAccount(false))
	require.NoError(t, err)
	assert.Empty(t, got, "an empty result leaves the Pod on the namespace's default ServiceAccount")
}

func TestResolveServiceAccount_RequestRefusedWhenNotAllowed(t *testing.T) {
	_, err := resolveServiceAccount(projectWithServiceAccount("atlantis"), "turnip-runner", allowServiceAccount(false))
	require.Error(t, err)

	var notPermitted *ServiceAccountNotPermittedError
	require.ErrorAs(t, err, &notPermitted)
	assert.Equal(t, "web", notPermitted.Project)
	assert.Equal(t, "atlantis", notPermitted.ServiceAccount)
	assert.Contains(t, err.Error(), "TURNIP_ALLOWED_OVERRIDES", "the error names the variable that would permit it")
	assert.Contains(t, err.Error(), overrideServiceAccount, "and the path to add to it")
}

func TestResolveServiceAccount_RequestHonoredWhenAllowed(t *testing.T) {
	got, err := resolveServiceAccount(projectWithServiceAccount("turnip-runner-project"), "turnip-runner", allowServiceAccount(true))
	require.NoError(t, err)
	assert.Equal(t, "turnip-runner-project", got)
}

// The refusal names the block the value came from, so the author edits the
// block that set it rather than searching the Project for a setting that
// is somewhere else. The fix is the same either way.
func TestServiceAccountNotPermitted_MessageNamesSource(t *testing.T) {
	const fix = ", which this turnip deployment does not permit. " +
		"Add \"runner.serviceAccount\" to TURNIP_ALLOWED_OVERRIDES on the Server to let turnip.yaml choose its own ServiceAccount."

	cases := map[string]struct {
		source config.ServiceAccountSource
		want   string
	}{
		"project": {
			source: config.ServiceAccountSourceProject,
			want:   `Project "web" requested runner.serviceAccount "deployer"` + fix,
		},
		"top level": {
			source: config.ServiceAccountSourceTopLevel,
			want:   `Project "web" inherits runner.serviceAccount "deployer" from the top-level runner: block` + fix,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			p := projectWithServiceAccount("deployer")
			p.ServiceAccountSource = tc.source

			_, err := resolveServiceAccount(p, "turnip-runner", allowServiceAccount(false))
			require.Error(t, err)

			var notPermitted *ServiceAccountNotPermittedError
			require.ErrorAs(t, err, &notPermitted)
			assert.Equal(t, tc.source, notPermitted.Source, "the error carries the source the Project recorded")
			assert.Equal(t, tc.want, err.Error())
		})
	}
}

// Wherever the value came from, the refusal is a Configuration_Refusal for
// runner.serviceAccount, so the Project_Check Title does not change with
// the source; only the message (the summary and the comment) does.
func TestServiceAccountNotPermitted_ConfigurationRefusalForEitherSource(t *testing.T) {
	for _, source := range []config.ServiceAccountSource{
		config.ServiceAccountSourceProject,
		config.ServiceAccountSourceTopLevel,
	} {
		t.Run(string(source), func(t *testing.T) {
			p := projectWithServiceAccount("deployer")
			p.ServiceAccountSource = source

			_, err := resolveServiceAccount(p, "turnip-runner", allowServiceAccount(false))
			require.Error(t, err)

			r := overrideRefusal(err)
			assert.Equal(t, refusalConfiguration, r.kind)
			assert.Equal(t, overrideServiceAccount, r.setting)
			assert.Equal(t, "runner.serviceAccount is not permitted", notPermittedTitle(r.setting))
			assert.Equal(t, err.Error(), r.reason)
		})
	}
}

// A value inherited from the top level is honored exactly like the
// Project's own once the override is permitted.
func TestResolveServiceAccount_InheritedHonoredWhenAllowed(t *testing.T) {
	p := projectWithServiceAccount("deployer")
	p.ServiceAccountSource = config.ServiceAccountSourceTopLevel

	got, err := resolveServiceAccount(p, "turnip-runner", allowServiceAccount(true))
	require.NoError(t, err)
	assert.Equal(t, "deployer", got)
}
