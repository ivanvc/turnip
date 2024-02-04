package yaml

import "gopkg.in/yaml.v3"

// Config holds the turnip.yaml configuration
type Config struct {
	HelmfileVersion string    `yaml:"helmfileVersion"`
	PulumiVersion   string    `yaml:"pulumiVersion"`
	Projects        []Project `yaml:"projects"`
}

type ProjectType string

const (
	ProjectTypeHelmfile  ProjectType = "helmfile"
	ProjectTypePulumi    ProjectType = "pulumi"
	ProjectTypeTerraform ProjectType = "terraform"
)

type Project struct {
	Dir     string      `yaml:"dir"`
	Type    ProjectType `yaml:"type"`
	Version string      `yaml:"version"`

	Stack       string `yaml:"stack"`
	Workspace   string `yaml:"workspace"`
	Environment string `yaml:"environment"`

	*AutoPlan   `yaml:"autoPlan"`
	AutoPreview *AutoPlan `yaml:"autoPreview"`
	AutoDiff    *AutoPlan `yaml:"autoDiff"`

	*Plan   `yaml:"plan"`
	Preview *Plan `yaml:"preview"`
	Diff    *Plan `yaml:"diff"`
}

type Plan struct {
	PreCommands []Command `yaml:"preCommands"`
}

type Command struct {
	Env map[string]string `yaml:"env"`
	Run string            `yaml:"run"`
}

type Step struct {
	Run string `yaml:"run"`
}

type AutoPlan struct {
	Disabled     bool     `yaml:"disabled"`
	WhenModified []string `yaml:"whenModified"`
}

func (p Project) ToYAML() ([]byte, error) {
	return yaml.Marshal(p)
}
