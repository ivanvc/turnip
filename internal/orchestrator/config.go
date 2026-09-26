package orchestrator

import (
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/ivanvc/turnip/internal/config"
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
	// AllowedOverrides is the set of Project fields a repository's
	// turnip.yaml may set for itself, from TURNIP_ALLOWED_OVERRIDES. See
	// overrides.go for the known paths and the default, which preserves
	// the behavior that shipped before this setting existed.
	AllowedOverrides map[string]bool
	// CloneSubmodules is the Submodule_Mode every clone uses unless a
	// repository overrides it in its own configuration file (and that
	// override is permitted). Unset means config.SubmodulesTopLevel:
	// turnip clones specifically to run IaC that may reference submodule
	// paths, so defaulting off would make every repository with a
	// submodule meet a confusing failure before anything worked.
	CloneSubmodules string
	// Catalog is every image this Server will run: each registered
	// Plugin's Alias Entries and the operator's Access_List, from
	// TURNIP_ALLOWED_IMAGES (images.go). config.Parse resolves each
	// Project's uses: against it.
	Catalog config.Catalog
	// MutationRequirements is the Requirement_Set, from
	// TURNIP_MUTATION_REQUIREMENTS: the conditions a pull request must meet
	// before turnip runs a Mutating_Operation on it. Each entry is one of
	// knownMutationRequirements, in the order first written, without
	// duplicates. Unset or blank leaves it empty, which requires nothing.
	// It is deliberately operator-side only and never an override path: a
	// pull request supplies turnip.yaml, so it must not be able to relax
	// the conditions it is itself held to.
	MutationRequirements []string
}

// The recognized Mutation_Requirement names for TURNIP_MUTATION_REQUIREMENTS.
const (
	// MutationRequirementApproved requires an approval from someone other
	// than the pull request's author.
	MutationRequirementApproved = "approved"
	// MutationRequirementMergeable requires the pull request to be mergeable.
	MutationRequirementMergeable = "mergeable"
)

// knownMutationRequirements is sorted so error messages list them stably.
var knownMutationRequirements = []string{MutationRequirementApproved, MutationRequirementMergeable}

// parseMutationRequirements reads the comma-separated list, trimming
// whitespace, skipping empty fields and collapsing duplicates. An
// unrecognized name is an error rather than a no-op, for the same reason
// parseAllowedOverrides rejects an unknown path: an operator who misspells
// a requirement asked for a control and would otherwise receive none.
func parseMutationRequirements(raw string) ([]string, error) {
	var reqs []string
	for _, field := range strings.Split(raw, ",") {
		name := strings.TrimSpace(field)
		if name == "" {
			continue
		}
		if !slices.Contains(knownMutationRequirements, name) {
			return nil, fmt.Errorf(
				"unknown mutation requirement %q; recognized requirements are %s",
				name, strings.Join(knownMutationRequirements, ", "),
			)
		}
		if !slices.Contains(reqs, name) {
			reqs = append(reqs, name)
		}
	}
	return reqs, nil
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
// rather than calling os.Getenv directly. plugins is the registry the
// Server runs with: its Plugins' Aliases and names are what
// TURNIP_ALLOWED_IMAGES is read against.
func ConfigFromEnv(env func(string) string, plugins PluginRegistry) (Config, error) {
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

	allowedOverrides, err := parseAllowedOverrides(env("TURNIP_ALLOWED_OVERRIDES"))
	if err != nil {
		return Config{}, fmt.Errorf("orchestrator: parsing TURNIP_ALLOWED_OVERRIDES: %w", err)
	}
	cfg.AllowedOverrides = allowedOverrides

	cloneSubmodules, err := parseSubmodules(env("TURNIP_CLONE_SUBMODULES"))
	if err != nil {
		return Config{}, fmt.Errorf("orchestrator: parsing TURNIP_CLONE_SUBMODULES: %w", err)
	}
	cfg.CloneSubmodules = cloneSubmodules

	mutationRequirements, err := parseMutationRequirements(env("TURNIP_MUTATION_REQUIREMENTS"))
	if err != nil {
		return Config{}, fmt.Errorf("orchestrator: parsing TURNIP_MUTATION_REQUIREMENTS: %w", err)
	}
	cfg.MutationRequirements = mutationRequirements

	catalog, err := parseAllowedImages(env("TURNIP_ALLOWED_IMAGES"), plugins)
	if err != nil {
		return Config{}, fmt.Errorf("orchestrator: parsing TURNIP_ALLOWED_IMAGES: %w", err)
	}
	cfg.Catalog = catalog

	return cfg, nil
}
