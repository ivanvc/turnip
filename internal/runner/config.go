package runner

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

// Config carries everything the Runner needs to execute one Operation,
// read from the environment variables internal/jobs.BuildJob sets on the
// Runner container.
type Config struct {
	ServerAddr  string
	OperationID string
	ProjectName string
	ProjectDir  string
	Tool        string
	Operation   string
	RepoURL     string
	CommitSHA   string
	GitHubToken string
	ToolConfig  map[string]string
	ExtraArgs   []string
	PlanData    []byte

	// ToolsDir is where internal/jobs.BuildJob's initContainer copied the
	// tool binary; Run prepends it to the Runner's own PATH at startup
	// (see run.go) rather than the Job spec trying to override PATH
	// directly, which can't compose with the image's own PATH.
	ToolsDir string
}

// MissingEnvVarsError names every required environment variable that was
// unset, accumulated in one error rather than reported one at a time
// (mirroring internal/config's accumulate-everything validation
// convention).
type MissingEnvVarsError struct {
	Names []string
}

func (e *MissingEnvVarsError) Error() string {
	return fmt.Sprintf("runner: missing required environment variable(s): %s", strings.Join(e.Names, ", "))
}

// ConfigFromEnv builds a Config from env, an injectable lookup function
// (mirroring internal/plugin's commandRunner testability-seam convention)
// rather than calling os.Getenv directly, so it's testable without
// mutating process-wide environment variables.
func ConfigFromEnv(env func(string) string) (Config, error) {
	cfg := Config{}
	required := []struct {
		name string
		dst  *string
	}{
		{"TURNIP_SERVER_ADDR", &cfg.ServerAddr},
		{"TURNIP_OPERATION_ID", &cfg.OperationID},
		{"TURNIP_PROJECT_NAME", &cfg.ProjectName},
		{"TURNIP_PROJECT_DIR", &cfg.ProjectDir},
		{"TURNIP_TOOL", &cfg.Tool},
		{"TURNIP_OPERATION", &cfg.Operation},
		{"TURNIP_REPO_URL", &cfg.RepoURL},
		{"TURNIP_COMMIT_SHA", &cfg.CommitSHA},
		{"TURNIP_GITHUB_TOKEN", &cfg.GitHubToken},
		{"TURNIP_TOOLS_DIR", &cfg.ToolsDir},
	}

	var missing []string
	for _, req := range required {
		value := env(req.name)
		if value == "" {
			missing = append(missing, req.name)
			continue
		}
		*req.dst = value
	}
	if len(missing) > 0 {
		return Config{}, &MissingEnvVarsError{Names: missing}
	}

	if raw := env("TURNIP_TOOL_CONFIG"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &cfg.ToolConfig); err != nil {
			return Config{}, fmt.Errorf("runner: parse TURNIP_TOOL_CONFIG: %w", err)
		}
	}

	if raw := env("TURNIP_EXTRA_ARGS"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &cfg.ExtraArgs); err != nil {
			return Config{}, fmt.Errorf("runner: parse TURNIP_EXTRA_ARGS: %w", err)
		}
	}

	if raw := env("TURNIP_PLAN_DATA"); raw != "" {
		planData, err := base64.StdEncoding.DecodeString(raw)
		if err != nil {
			return Config{}, fmt.Errorf("runner: parse TURNIP_PLAN_DATA: %w", err)
		}
		cfg.PlanData = planData
	}

	return cfg, nil
}
