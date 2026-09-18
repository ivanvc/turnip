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
	got, err := resolveServiceAccount(projectWithServiceAccount("turnip-runner-cicd2"), "turnip-runner", allowServiceAccount(true))
	require.NoError(t, err)
	assert.Equal(t, "turnip-runner-cicd2", got)
}
