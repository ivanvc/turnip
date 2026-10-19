package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

func kubeconfigWithNamespace(t *testing.T, ns string) {
	t.Helper()
	nsLine := ""
	if ns != "" {
		nsLine = "    namespace: " + ns + "\n"
	}
	body := "apiVersion: v1\nkind: Config\ncurrent-context: c\n" +
		"clusters:\n- name: k\n  cluster:\n    server: https://127.0.0.1:6443\n" +
		"users:\n- name: u\n  user: {}\n" +
		"contexts:\n- name: c\n  context:\n    cluster: k\n    user: u\n" + nsLine
	path := filepath.Join(t.TempDir(), "kubeconfig")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	t.Setenv("KUBECONFIG", path)
	// Keep the in-cluster branch out of the way.
	t.Setenv("KUBERNETES_SERVICE_HOST", "")
	t.Setenv("KUBERNETES_SERVICE_PORT", "")
}

func TestResolveRunnerNamespace_ContextNamespace(t *testing.T) {
	kubeconfigWithNamespace(t, "dev")
	ns, err := resolveRunnerNamespace("", kubernetesClientConfig())
	require.NoError(t, err)
	assert.Equal(t, "dev", ns)
}

func TestResolveRunnerNamespace_ContextWithoutNamespace(t *testing.T) {
	kubeconfigWithNamespace(t, "")
	ns, err := resolveRunnerNamespace("", kubernetesClientConfig())
	require.NoError(t, err)
	assert.Equal(t, "default", ns)
}

type failingClientConfig struct{}

func (failingClientConfig) RawConfig() (clientcmdapi.Config, error) {
	return clientcmdapi.Config{}, errors.New("boom")
}

func (failingClientConfig) ClientConfig() (*rest.Config, error) { return nil, errors.New("boom") }

func (failingClientConfig) ConfigAccess() clientcmd.ConfigAccess { return nil }

func (failingClientConfig) Namespace() (string, bool, error) {
	return "", false, errors.New("boom")
}

func TestResolveRunnerNamespace_SetValueSkipsLoader(t *testing.T) {
	ns, err := resolveRunnerNamespace("ops", failingClientConfig{})
	require.NoError(t, err)
	assert.Equal(t, "ops", ns)
}

func TestResolveRunnerNamespace_FailureNamesSetting(t *testing.T) {
	_, err := resolveRunnerNamespace("", failingClientConfig{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "TURNIP_RUNNER_NAMESPACE")
}
