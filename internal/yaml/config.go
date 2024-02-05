package yaml

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// Config holds the turnip.yaml configuration
type Config struct {
	Projects  []Project           `yaml:"projects"`
	Workflows map[string]Workflow `yaml:"workflows"`
}

type ProjectType string

const (
	ProjectTypeHelmfile  ProjectType = "helmfile"
	ProjectTypePulumi    ProjectType = "pulumi"
	ProjectTypeTerraform ProjectType = "terraform"
)

type Workflow struct {
	Type        ProjectType       `yaml:"type"`
	Version     string            `yaml:"version"`
	Env         map[string]string `yaml:"env"`
	PreCommands []Command         `yaml:"preCommands"`
}

type Project struct {
	Dir string `yaml:"dir"`

	Stack       string `yaml:"stack"`
	Workspace   string `yaml:"workspace"`
	Environment string `yaml:"environment"`

	*AutoPlan   `yaml:"autoPlan"`
	AutoPreview *AutoPlan `yaml:"autoPreview"`
	AutoDiff    *AutoPlan `yaml:"autoDiff"`

	Workflow       string   `yaml:"workflow"`
	LoadedWorkflow Workflow `yaml:"_loadedWorkflow"`
}

type Command struct {
	Env        map[string]string `yaml:"env"`
	Run        string            `yaml:"run"`
	Login      string            `yaml:"login"`
	OmitOutput bool              `yaml:"omitOutput"`
}

type Step struct {
	Run string `yaml:"run"`
}

type AutoPlan struct {
	Disabled     bool     `yaml:"disabled"`
	WhenModified []string `yaml:"whenModified"`
}

func Load(data []byte) (Config, error) {
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, err
	}

	for i, p := range cfg.Projects {
		if w, ok := cfg.Workflows[p.Workflow]; ok {
			cfg.Projects[i].LoadedWorkflow = w
		} else {
			return cfg, fmt.Errorf("workflow %s not found", p.Workflow)
		}
	}
	return cfg, nil
}

func LoadProject(data []byte) (Project, error) {
	var p Project
	err := yaml.Unmarshal(data, &p)
	return p, err
}

func (p Project) ToYAML() ([]byte, error) {
	return yaml.Marshal(p)
}
