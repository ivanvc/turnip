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
	BaseRef     string
	GitHubToken string
	ToolConfig  map[string]string
	ExtraArgs   []string
	PlanData    []byte

	// ToolsDir is where internal/jobs.BuildJob's initContainer copied the
	// tool binary; Run prepends it to the Runner's own PATH at startup
	// (see run.go) rather than the Job spec trying to override PATH
	// directly, which can't compose with the image's own PATH.
	ToolsDir string

	// WorkspaceDir is the directory the repository is cloned into — a
	// volume mount point owned by the Pod, so it is used as-is and never
	// removed. Unlike ToolsDir it is optional: an empty value means
	// "create a temporary directory and remove it afterwards", which is
	// what the Runner's own tests exercise and what any caller outside a
	// turnip-built Job gets.
	WorkspaceDir string

	// CloneSubmodules is the Submodule_Mode this Operation's clone uses:
	// one of config.Submodules{None,TopLevel,Recursive}. Like GitHubToken
	// it is set on the clone initContainer and deliberately absent from
	// the container that runs the tool, so it is optional here and read
	// only in clone mode. An empty value means top-level rather than
	// "off", so a Job built by an older Server still initialises
	// submodules instead of silently producing an empty directory.
	CloneSubmodules string
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
		{"TURNIP_BASE_REF", &cfg.BaseRef},
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

	// The three below are optional because each is set on some containers
	// and deliberately absent from others — requiring them would make the
	// Runner refuse to start in exactly the Jobs turnip builds.

	// Set on the clone initContainer and nowhere else: the process that
	// runs the tool has no use for a GitHub installation token, and under
	// the run-in-image strategy that process is a vendor image executing
	// arbitrary tool plugins. Clone also treats an empty token as "no
	// credential to embed", which is what a public repository needs.
	cfg.GitHubToken = env("TURNIP_GITHUB_TOKEN")

	// Set only under the copy-out strategy. Under run-in-image the tool is
	// already on the vendor image's own PATH, and pathWithToolsDir leaves
	// PATH untouched when this is empty.
	cfg.ToolsDir = env("TURNIP_TOOLS_DIR")

	// An unset value is the documented temporary-directory fallback for
	// the Runner — though not for clone mode, which has nowhere durable to
	// write and rejects it (see runCloneWith).
	cfg.WorkspaceDir = env("TURNIP_WORKSPACE_DIR")

	// Set on the clone initContainer and nowhere else, like the token
	// above. Empty means top-level, not "off" — see initSubmodules.
	cfg.CloneSubmodules = env("TURNIP_CLONE_SUBMODULES")

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
