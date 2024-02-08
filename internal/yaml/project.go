package yaml

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

type Project struct {
	Dir string `yaml:"dir"`

	Stack       string `yaml:"stack"`
	Workspace   string `yaml:"workspace"`
	Environment string `yaml:"environment"`

	AutoPlan    bool `yaml:"autoPlan"`
	AutoPreview bool `yaml:"autoPreview"`
	AutoDiff    bool `yaml:"autoDiff"`

	WhenModified []string `yaml:"whenModified"`

	Workflow       string   `yaml:"workflow"`
	LoadedWorkflow Workflow `yaml:"__loadedWorkflow"`
}

func LoadProject(data []byte) (Project, error) {
	var p Project
	err := yaml.Unmarshal(data, &p)
	return p, err
}

func (p Project) ToYAML() ([]byte, error) {
	return yaml.Marshal(p)
}

func (p Project) GetAutoPlot() bool {
	a, err := p.LoadedWorkflow.GetAdapter()
	if err != nil {
		return false
	}
	switch a.(type) {
	case HelmfileAdapter:
		return p.AutoDiff
	case PulumiAdapter:
		return p.AutoPreview
	case TerraformAdapter:
		return p.AutoPlan
	}
	return false
}

func (p Project) Validate() error {
	if p.Workflow == "" {
		return fmt.Errorf("project %s: workflow not set", p.Dir)
	}

	return nil
}

func (p Project) GetWorkspace() string {
	a, err := p.LoadedWorkflow.GetAdapter()
	if err != nil {
		return ""
	}
	return a.GetWorkspace(p)
}

func (p Project) GetPlotName() string {
	a, err := p.LoadedWorkflow.GetAdapter()
	if err != nil {
		return ""
	}
	return a.GetPlotName()
}

func (p Project) GetAdapterName() string {
	a, err := p.LoadedWorkflow.GetAdapter()
	if err != nil {
		return ""
	}
	return a.GetName()
}