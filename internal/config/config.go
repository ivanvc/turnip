package config

// SupportedSchemaVersion is the only turnip.yaml schema version this
// turnip accepts. It versions the *shape of the file* — not turnip
// itself, and not the IaC tool release a Project pins in
// `config.version`.
//
// The alpha suffix is deliberate. Before turnip 1.0 the schema is
// expected to break, and advancing within alpha (v1alpha2, v1alpha3, …)
// costs no turnip release and promises nobody anything. It graduates to
// "v1" when turnip reaches 1.0, at which point the schema version and the
// project major coincide. "beta" is not used unless its obligation — a
// deprecation window with migration instructions — is actually accepted.
const SupportedSchemaVersion = "v1alpha1"

// Config is the parsed, validated in-memory representation of a
// turnip.yaml file.
type Config struct {
	SchemaVersion string    `yaml:"schemaVersion"`
	Projects      []Project `yaml:"projects"`
}

// Project is a configuration unit within Config defining a directory, IaC
// tool, whenModified rules, and tool-specific config.
type Project struct {
	Name         string            `yaml:"name"`
	Directory    string            `yaml:"directory"`
	Tool         string            `yaml:"tool"`
	WhenModified []string          `yaml:"whenModified"`
	Config       map[string]string `yaml:"config"`
	// Env is handed to the IaC tool's process and never interpreted by
	// turnip — unlike Config, whose keys a Plugin reads. Names are
	// restricted (see validate.go) because the Runner reads its own
	// configuration from TURNIP_* and finds its tool binary through PATH.
	Env map[string]string `yaml:"env,omitempty"`
}

// reservedEnvPrefix is the namespace the Runner reads its own
// configuration from; a Project setting anything here could redirect the
// Runner's server address, operation, or GitHub token. Enforced in
// validate.go.
const reservedEnvPrefix = "TURNIP_"

// Supported IaC tool values for Project.Tool.
const (
	ToolTerraform = "terraform"
	ToolPulumi    = "pulumi"
	ToolHelmfile  = "helmfile"
)
