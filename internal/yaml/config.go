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
	Dir         string `yaml:"dir"`
	*AutoPlan   `yaml:"autoPlan"`
	AutoPreview *AutoPlan   `yaml:"autoPlan"`
	AutoDiff    *AutoPlan   `yaml:"autoDiff"`
	Stack       string      `yaml:"stack"`
	Workspace   string      `yaml:"workspace"`
	Environment string      `yaml:"environment"`
	Version     string      `yaml:"version"`
	Type        ProjectType `yaml:"type"`
}

type AutoPlan struct {
	Disabled     bool     `yaml:"disabled"`
	WhenModified []string `yaml:"whenModified"`
}

func (p Project) ToYAML() ([]byte, error) {
	return yaml.Marshal(p)
}
