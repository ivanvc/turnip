package orchestrator

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivanvc/turnip/internal/config"
)

func projectWithServiceAccount(name string) config.Project {
	p := config.Project{Name: "web", Directory: "infra/web", Tool: "helmfile"}
	if name != "" {
		p.Config = map[string]string{"serviceAccount": name}
	}
	return p
}

func TestResolveServiceAccount_NoRequestUsesServerDefault(t *testing.T) {
	for _, allow := range []bool{false, true} {
		got, err := resolveServiceAccount(projectWithServiceAccount(""), "turnip-runner", allow)
		require.NoError(t, err)
		assert.Equal(t, "turnip-runner", got, "allowFromConfig=%v", allow)
	}
}

func TestResolveServiceAccount_NoRequestNoDefaultIsEmpty(t *testing.T) {
	got, err := resolveServiceAccount(projectWithServiceAccount(""), "", false)
	require.NoError(t, err)
	assert.Empty(t, got, "an empty result leaves the Pod on the namespace's default ServiceAccount")
}

func TestResolveServiceAccount_RequestRefusedWhenNotAllowed(t *testing.T) {
	_, err := resolveServiceAccount(projectWithServiceAccount("atlantis"), "turnip-runner", false)
	require.Error(t, err)

	var notPermitted *ServiceAccountNotPermittedError
	require.ErrorAs(t, err, &notPermitted)
	assert.Equal(t, "web", notPermitted.Project)
	assert.Equal(t, "atlantis", notPermitted.ServiceAccount)
	assert.Contains(t, err.Error(), "TURNIP_RUNNER_SERVICE_ACCOUNT_ALLOW_FROM_CONFIG")
}

func TestResolveServiceAccount_RequestHonoredWhenAllowed(t *testing.T) {
	got, err := resolveServiceAccount(projectWithServiceAccount("turnip-runner-cicd2"), "turnip-runner", true)
	require.NoError(t, err)
	assert.Equal(t, "turnip-runner-cicd2", got)
}
