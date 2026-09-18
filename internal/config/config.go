package config

// SupportedSchemaVersion is the only turnip.yaml schema version this
// turnip accepts. It versions the *shape of the file* — not turnip
// itself, and not the IaC tool release a Project pins in `uses`.
//
// The alpha suffix is deliberate. Before turnip 1.0 the schema is
// expected to break, and advancing within alpha (v1alpha2, v1alpha3, …)
// costs no turnip release and promises nobody anything. It graduates to
// "v1" when turnip reaches 1.0, at which point the schema version and the
// project major coincide. "beta" is not used unless its obligation — a
// deprecation window with migration instructions — is actually accepted.
const SupportedSchemaVersion = "v1alpha2"

// Config is the parsed, validated in-memory representation of a
// turnip.yaml file.
type Config struct {
	SchemaVersion string    `yaml:"schemaVersion"`
	Projects      []Project `yaml:"projects"`
}

// Project is a configuration unit within Config. Its fields answer three
// separate questions: what to run (Uses), how to call it (With), and
// where it runs (Runner).
type Project struct {
	Name      string `yaml:"name"`
	Directory string `yaml:"directory"`

	// Uses names the IaC tool and, optionally, the version to provision,
	// written as "<tool>" or "<tool>@<version>". Nothing downstream reads
	// this field: applyDefaults decomposes it into Tool and ToolVersion,
	// which is what makes the fused form free of consequences past the
	// parser.
	Uses string `yaml:"uses"`

	// With is configuration for this Project's Plugin and for nothing
	// else. Unlike the map it replaces, it carries no setting turnip
	// itself reads — a key here is a Plugin's to interpret, which is why
	// unrecognised keys inside it are accepted.
	With map[string]string `yaml:"with,omitempty"`

	// Runner carries settings that shape the Runner Pod rather than the
	// tool it runs.
	Runner RunnerSpec `yaml:"runner,omitempty"`

	WhenModified []string `yaml:"whenModified"`

	// Tool and ToolVersion are derived from Uses during parsing and are
	// bound to no YAML key: they are neither read from a file nor written
	// back to one. ToolVersion is empty when Uses named no version, which
	// means "the documented default for this tool" rather than "none".
	Tool        string `yaml:"-"`
	ToolVersion string `yaml:"-"`
}

// RunnerSpec is the subset of a Project that configures the Kubernetes
// Pod rather than the IaC tool running inside it.
type RunnerSpec struct {
	// ServiceAccount is the Kubernetes ServiceAccount the Runner Pod runs
	// as — the identity cloud providers map to an IAM role. Whether a
	// Project may set it at all is an operator's decision; see
	// internal/orchestrator's allowed-overrides handling.
	ServiceAccount string `yaml:"serviceAccount,omitempty"`

	// Env is handed to the IaC tool's process and never interpreted by
	// turnip — unlike With, whose keys a Plugin reads. Names are
	// restricted (see validate.go) because the Runner reads its own
	// configuration from TURNIP_* and finds its tool binary through PATH.
	Env map[string]string `yaml:"env,omitempty"`
}

// reservedEnvPrefix is the namespace the Runner reads its own
// configuration from; a Project setting anything here could redirect the
// Runner's server address, operation, or GitHub token. Enforced in
// validate.go.
const reservedEnvPrefix = "TURNIP_"

// Supported IaC tool values for the tool portion of Project.Uses.
const (
	ToolTerraform = "terraform"
	ToolPulumi    = "pulumi"
	ToolHelmfile  = "helmfile"
)
