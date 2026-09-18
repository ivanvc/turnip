package orchestrator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func envMap(overrides map[string]string) func(string) string {
	defaults := map[string]string{
		"TURNIP_GITHUB_APP_ID":         "123",
		"TURNIP_GITHUB_PRIVATE_KEY":    "fake-pem",
		"TURNIP_GITHUB_WEBHOOK_SECRET": "secret",
		"TURNIP_REDIS_ADDR":            "localhost:6379",
		"TURNIP_K8S_NAMESPACE":         "turnip",
		"TURNIP_HTTP_ADDR":             ":8080",
		"TURNIP_GRPC_ADDR":             ":9090",
		"TURNIP_RUNNER_SERVER_ADDR":    "turnip-server:9090",
		"TURNIP_RUNNER_IMAGE":          "ghcr.io/ivanvc/turnip-runner:test",
	}
	for k, v := range overrides {
		if v == "" {
			delete(defaults, k)
		} else {
			defaults[k] = v
		}
	}
	return func(key string) string { return defaults[key] }
}

func TestConfigFromEnv_FullyPopulated(t *testing.T) {
	cfg, err := ConfigFromEnv(envMap(nil))
	require.NoError(t, err)
	assert.EqualValues(t, 123, cfg.GitHubAppID)
	assert.Equal(t, []byte("fake-pem"), cfg.GitHubPrivateKey)
	assert.Equal(t, "secret", cfg.GitHubWebhookSecret)
	assert.Equal(t, "localhost:6379", cfg.RedisAddr)
	assert.Equal(t, "turnip", cfg.KubernetesNamespace)
	assert.Equal(t, ":8080", cfg.HTTPAddr)
	assert.Equal(t, ":9090", cfg.GRPCAddr)
	assert.Equal(t, "turnip-server:9090", cfg.RunnerServerAddr)
	assert.False(t, cfg.MinimizeOutdatedPlanComments)
	assert.Equal(t, "ghcr.io/ivanvc/turnip-runner:test", cfg.RunnerImage)
}

func TestConfigFromEnv_MinimizeFlagDefaultsFalse(t *testing.T) {
	cfg, err := ConfigFromEnv(envMap(nil))
	require.NoError(t, err)
	assert.False(t, cfg.MinimizeOutdatedPlanComments)
}

func TestConfigFromEnv_MinimizeFlagParsesTrue(t *testing.T) {
	cfg, err := ConfigFromEnv(envMap(map[string]string{"TURNIP_MINIMIZE_OUTDATED_PLAN_COMMENTS": "true"}))
	require.NoError(t, err)
	assert.True(t, cfg.MinimizeOutdatedPlanComments)
}

func TestConfigFromEnv_MinimizeFlagInvalidValue(t *testing.T) {
	_, err := ConfigFromEnv(envMap(map[string]string{"TURNIP_MINIMIZE_OUTDATED_PLAN_COMMENTS": "not-a-bool"}))
	assert.Error(t, err)
}

func TestConfigFromEnv_RunnerImageRoundTrip(t *testing.T) {
	cfg, err := ConfigFromEnv(envMap(map[string]string{"TURNIP_RUNNER_IMAGE": "ghcr.io/ivanvc/turnip-runner:v1.2.3"}))
	require.NoError(t, err)
	assert.Equal(t, "ghcr.io/ivanvc/turnip-runner:v1.2.3", cfg.RunnerImage)
}

func TestConfigFromEnv_RunnerServiceAccountRoundTrip(t *testing.T) {
	cfg, err := ConfigFromEnv(envMap(map[string]string{"TURNIP_RUNNER_SERVICE_ACCOUNT": "turnip-runner"}))
	require.NoError(t, err)
	assert.Equal(t, "turnip-runner", cfg.RunnerServiceAccount)
}

func TestConfigFromEnv_RunnerServiceAccountOptional(t *testing.T) {
	cfg, err := ConfigFromEnv(envMap(nil))
	require.NoError(t, err)
	assert.Empty(t, cfg.RunnerServiceAccount, "unset must not be a missing-variable error")
}

// Unset must keep behaving exactly as turnip did before the setting
// existed: a repository could never choose its own ServiceAccount.
func TestConfigFromEnv_AllowedOverridesDefaultsToNothing(t *testing.T) {
	cfg, err := ConfigFromEnv(envMap(nil))
	require.NoError(t, err)
	assert.Empty(t, cfg.AllowedOverrides)
}

func TestConfigFromEnv_AllowedOverridesParsesList(t *testing.T) {
	cfg, err := ConfigFromEnv(envMap(map[string]string{
		"TURNIP_ALLOWED_OVERRIDES": " runner.serviceAccount ",
	}))
	require.NoError(t, err)
	assert.True(t, cfg.AllowedOverrides[overrideServiceAccount], "surrounding whitespace is trimmed")
}

// An unrecognized path would gate nothing while looking like it gated
// something — the operator-side version of the silently-ignored key this
// schema version removes from turnip.yaml.
func TestConfigFromEnv_AllowedOverridesUnknownPathIsAnError(t *testing.T) {
	_, err := ConfigFromEnv(envMap(map[string]string{
		"TURNIP_ALLOWED_OVERRIDES": "runner.serviceaccount",
	}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "runner.serviceaccount", "the error names what was written")
	assert.Contains(t, err.Error(), overrideServiceAccount, "and what was probably meant")
}

func TestConfigFromEnv_MissingRunnerImage(t *testing.T) {
	_, err := ConfigFromEnv(envMap(map[string]string{"TURNIP_RUNNER_IMAGE": ""}))
	require.Error(t, err)
	var missing *MissingEnvVarsError
	require.ErrorAs(t, err, &missing)
	assert.Contains(t, missing.Names, "TURNIP_RUNNER_IMAGE")
}

func TestConfigFromEnv_MissingVariablesNamedTogether(t *testing.T) {
	_, err := ConfigFromEnv(envMap(map[string]string{
		"TURNIP_REDIS_ADDR": "",
		"TURNIP_HTTP_ADDR":  "",
	}))
	require.Error(t, err)
	var missing *MissingEnvVarsError
	require.ErrorAs(t, err, &missing)
	assert.Contains(t, missing.Names, "TURNIP_REDIS_ADDR")
	assert.Contains(t, missing.Names, "TURNIP_HTTP_ADDR")
}

func TestConfigFromEnv_MissingAppID(t *testing.T) {
	_, err := ConfigFromEnv(envMap(map[string]string{"TURNIP_GITHUB_APP_ID": ""}))
	require.Error(t, err)
	var missing *MissingEnvVarsError
	require.ErrorAs(t, err, &missing)
	assert.Contains(t, missing.Names, "TURNIP_GITHUB_APP_ID")
}

func TestConfigFromEnv_InvalidAppID(t *testing.T) {
	_, err := ConfigFromEnv(envMap(map[string]string{"TURNIP_GITHUB_APP_ID": "not-a-number"}))
	assert.Error(t, err)
}

func TestConfigFromEnv_PrivateKeyFromPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "key.pem")
	require.NoError(t, os.WriteFile(path, []byte("pem-from-file"), 0o600))

	cfg, err := ConfigFromEnv(envMap(map[string]string{
		"TURNIP_GITHUB_PRIVATE_KEY":      "",
		"TURNIP_GITHUB_PRIVATE_KEY_PATH": path,
	}))
	require.NoError(t, err)
	assert.Equal(t, []byte("pem-from-file"), cfg.GitHubPrivateKey)
}

func TestConfigFromEnv_MissingPrivateKey(t *testing.T) {
	_, err := ConfigFromEnv(envMap(map[string]string{"TURNIP_GITHUB_PRIVATE_KEY": ""}))
	require.Error(t, err)
	var missing *MissingEnvVarsError
	require.ErrorAs(t, err, &missing)
}
