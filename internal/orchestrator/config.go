package orchestrator

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Config carries the Server's own startup configuration, read from
// environment variables.
type Config struct {
	GitHubAppID         int64
	GitHubPrivateKey    []byte // PEM-encoded
	GitHubWebhookSecret string
	RedisAddr           string
	KubernetesNamespace string
	HTTPAddr            string
	GRPCAddr            string
	// RunnerServerAddr is the address a Runner Pod dials to reach the
	// Server's gRPC endpoint — not necessarily the same as GRPCAddr (the
	// bind address), since a real deployment typically points Runners at
	// a Kubernetes Service DNS name rather than whatever address the
	// Server process itself binds to.
	RunnerServerAddr             string
	MinimizeOutdatedPlanComments bool
	// RunnerImage is the Runner Job's container image, threaded into
	// internal/jobs.OperationParams.RunnerImage (Decision 3).
	RunnerImage string
	// RunnerServiceAccount is the ServiceAccount every Runner Job's Pod
	// runs as unless a Project overrides it (and that override is
	// permitted). Optional: empty leaves Pods on the namespace's default
	// ServiceAccount, which is what turnip did before this setting existed.
	RunnerServiceAccount string
	// AllowServiceAccountFromConfig gates whether a Project's
	// `config.serviceAccount` in turnip.yaml may override
	// RunnerServiceAccount. Default false — see
	// serviceaccount.go's resolveServiceAccount for why.
	AllowServiceAccountFromConfig bool
}

// MissingEnvVarsError names every required environment variable that was
// unset, accumulated in one error rather than reported one at a time
// (mirroring internal/config's accumulate-everything validation
// convention, already followed by internal/runner.ConfigFromEnv).
type MissingEnvVarsError struct {
	Names []string
}

func (e *MissingEnvVarsError) Error() string {
	return fmt.Sprintf("orchestrator: missing required environment variable(s): %s", strings.Join(e.Names, ", "))
}

// ConfigFromEnv builds a Config from env, an injectable lookup function
// (mirroring internal/runner.ConfigFromEnv's testability-seam convention)
// rather than calling os.Getenv directly.
func ConfigFromEnv(env func(string) string) (Config, error) {
	cfg := Config{
		GitHubWebhookSecret: env("TURNIP_GITHUB_WEBHOOK_SECRET"),
		RedisAddr:           env("TURNIP_REDIS_ADDR"),
		KubernetesNamespace: env("TURNIP_K8S_NAMESPACE"),
		HTTPAddr:            env("TURNIP_HTTP_ADDR"),
		GRPCAddr:            env("TURNIP_GRPC_ADDR"),
		RunnerServerAddr:    env("TURNIP_RUNNER_SERVER_ADDR"),
		RunnerImage:         env("TURNIP_RUNNER_IMAGE"),

		RunnerServiceAccount: env("TURNIP_RUNNER_SERVICE_ACCOUNT"),
	}

	var missing []string
	for _, req := range []struct {
		name  string
		value string
	}{
		{"TURNIP_GITHUB_WEBHOOK_SECRET", cfg.GitHubWebhookSecret},
		{"TURNIP_REDIS_ADDR", cfg.RedisAddr},
		{"TURNIP_K8S_NAMESPACE", cfg.KubernetesNamespace},
		{"TURNIP_HTTP_ADDR", cfg.HTTPAddr},
		{"TURNIP_GRPC_ADDR", cfg.GRPCAddr},
		{"TURNIP_RUNNER_SERVER_ADDR", cfg.RunnerServerAddr},
		{"TURNIP_RUNNER_IMAGE", cfg.RunnerImage},
	} {
		if req.value == "" {
			missing = append(missing, req.name)
		}
	}

	rawAppID := env("TURNIP_GITHUB_APP_ID")
	if rawAppID == "" {
		missing = append(missing, "TURNIP_GITHUB_APP_ID")
	} else {
		appID, err := strconv.ParseInt(rawAppID, 10, 64)
		if err != nil {
			return Config{}, fmt.Errorf("orchestrator: parsing TURNIP_GITHUB_APP_ID: %w", err)
		}
		cfg.GitHubAppID = appID
	}

	switch {
	case env("TURNIP_GITHUB_PRIVATE_KEY_PATH") != "":
		key, err := os.ReadFile(env("TURNIP_GITHUB_PRIVATE_KEY_PATH"))
		if err != nil {
			return Config{}, fmt.Errorf("orchestrator: reading TURNIP_GITHUB_PRIVATE_KEY_PATH: %w", err)
		}
		cfg.GitHubPrivateKey = key
	case env("TURNIP_GITHUB_PRIVATE_KEY") != "":
		cfg.GitHubPrivateKey = []byte(env("TURNIP_GITHUB_PRIVATE_KEY"))
	default:
		missing = append(missing, "TURNIP_GITHUB_PRIVATE_KEY or TURNIP_GITHUB_PRIVATE_KEY_PATH")
	}

	if len(missing) > 0 {
		return Config{}, &MissingEnvVarsError{Names: missing}
	}

	if raw := env("TURNIP_MINIMIZE_OUTDATED_PLAN_COMMENTS"); raw != "" {
		enabled, err := strconv.ParseBool(raw)
		if err != nil {
			return Config{}, fmt.Errorf("orchestrator: parsing TURNIP_MINIMIZE_OUTDATED_PLAN_COMMENTS: %w", err)
		}
		cfg.MinimizeOutdatedPlanComments = enabled
	}

	if raw := env("TURNIP_RUNNER_SERVICE_ACCOUNT_ALLOW_FROM_CONFIG"); raw != "" {
		allowed, err := strconv.ParseBool(raw)
		if err != nil {
			return Config{}, fmt.Errorf("orchestrator: parsing TURNIP_RUNNER_SERVICE_ACCOUNT_ALLOW_FROM_CONFIG: %w", err)
		}
		cfg.AllowServiceAccountFromConfig = allowed
	}

	return cfg, nil
}
