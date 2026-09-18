package runner

import (
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func fullEnv() map[string]string {
	return map[string]string{
		"TURNIP_SERVER_ADDR":  "server:9443",
		"TURNIP_OPERATION_ID": "op-1",
		"TURNIP_PROJECT_NAME": "web",
		"TURNIP_PROJECT_DIR":  "infra/web",
		"TURNIP_TOOL":         "helmfile",
		"TURNIP_OPERATION":    "diff",
		"TURNIP_REPO_URL":     "https://github.com/acme/repo.git",
		"TURNIP_COMMIT_SHA":   "abc123",
		"TURNIP_BASE_REF":     "main",
		"TURNIP_GITHUB_TOKEN": "ghs_token",
		"TURNIP_TOOLS_DIR":    "/tools",
		"TURNIP_TOOL_CONFIG":  `{"environment":"staging"}`,
		"TURNIP_EXTRA_ARGS":   `["--quiet"]`,
		"TURNIP_PLAN_DATA":    base64.StdEncoding.EncodeToString([]byte("plan-bytes")),
	}
}

func lookup(env map[string]string) func(string) string {
	return func(name string) string { return env[name] }
}

func TestConfigFromEnv_FullyPopulated(t *testing.T) {
	cfg, err := ConfigFromEnv(lookup(fullEnv()))
	require.NoError(t, err)

	assert.Equal(t, "server:9443", cfg.ServerAddr)
	assert.Equal(t, "op-1", cfg.OperationID)
	assert.Equal(t, "web", cfg.ProjectName)
	assert.Equal(t, "infra/web", cfg.ProjectDir)
	assert.Equal(t, "helmfile", cfg.Tool)
	assert.Equal(t, "diff", cfg.Operation)
	assert.Equal(t, "https://github.com/acme/repo.git", cfg.RepoURL)
	assert.Equal(t, "abc123", cfg.CommitSHA)
	assert.Equal(t, "main", cfg.BaseRef)
	assert.Equal(t, "ghs_token", cfg.GitHubToken)
	assert.Equal(t, "/tools", cfg.ToolsDir)
	assert.Equal(t, map[string]string{"environment": "staging"}, cfg.ToolConfig)
	assert.Equal(t, []string{"--quiet"}, cfg.ExtraArgs)
	assert.Equal(t, []byte("plan-bytes"), cfg.PlanData)
}

func TestConfigFromEnv_OptionalFieldsDefaultEmpty(t *testing.T) {
	env := fullEnv()
	delete(env, "TURNIP_TOOL_CONFIG")
	delete(env, "TURNIP_EXTRA_ARGS")
	delete(env, "TURNIP_PLAN_DATA")

	cfg, err := ConfigFromEnv(lookup(env))
	require.NoError(t, err)
	assert.Nil(t, cfg.ToolConfig)
	assert.Nil(t, cfg.ExtraArgs)
	assert.Nil(t, cfg.PlanData)
}

func TestConfigFromEnv_MissingVariablesNamedTogether(t *testing.T) {
	env := fullEnv()
	delete(env, "TURNIP_SERVER_ADDR")
	delete(env, "TURNIP_OPERATION_ID")

	_, err := ConfigFromEnv(lookup(env))
	require.Error(t, err)
	var missing *MissingEnvVarsError
	require.ErrorAs(t, err, &missing)
	assert.ElementsMatch(t, []string{"TURNIP_SERVER_ADDR", "TURNIP_OPERATION_ID"}, missing.Names)
}

// The environment BuildJob actually sets on the container that runs the
// tool: no GitHub token (it goes to the clone initContainer alone) and, under
// the run-in-image strategy, no tools directory either. Requiring either
// would make the Runner refuse to start in every Job turnip builds — a
// failure no other test here can see, because they all start from an
// environment carrying every variable at once.
func TestConfigFromEnv_ToolContainerEnvironmentIsAccepted(t *testing.T) {
	env := fullEnv()
	delete(env, "TURNIP_GITHUB_TOKEN")
	delete(env, "TURNIP_TOOLS_DIR")

	cfg, err := ConfigFromEnv(lookup(env))
	require.NoError(t, err)
	assert.Empty(t, cfg.GitHubToken)
	assert.Empty(t, cfg.ToolsDir, "run-in-image leaves the tool on the vendor image's own PATH")
}

func TestConfigFromEnv_InvalidToolConfigJSON(t *testing.T) {
	env := fullEnv()
	env["TURNIP_TOOL_CONFIG"] = "{not json"

	_, err := ConfigFromEnv(lookup(env))
	require.Error(t, err)
}

func TestConfigFromEnv_InvalidPlanDataBase64(t *testing.T) {
	env := fullEnv()
	env["TURNIP_PLAN_DATA"] = "not-base64!!"

	_, err := ConfigFromEnv(lookup(env))
	require.Error(t, err)
}
