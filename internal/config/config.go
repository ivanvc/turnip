package config

// Config is the parsed, validated in-memory representation of a
// turnip.yaml file.
type Config struct {
	Version  int       `yaml:"version"`
	Projects []Project `yaml:"projects"`
}

// Project is a configuration unit within Config defining a directory, IaC
// tool, whenModified rules, and tool-specific config.
type Project struct {
	Name         string            `yaml:"name"`
	Directory    string            `yaml:"directory"`
	Tool         string            `yaml:"tool"`
	WhenModified []string          `yaml:"whenModified"`
	Config       map[string]string `yaml:"config"`
}

// Supported IaC tool values for Project.Tool.
const (
	ToolTerraform = "terraform"
	ToolPulumi    = "pulumi"
	ToolHelmfile  = "helmfile"
)
