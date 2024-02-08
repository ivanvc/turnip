package yaml

import "fmt"

type Adapter interface {
	GetName() string
	GetPlotName() string
	GetVersion() string
	GetWorkspace(Project) string
	Validate() error
}

type BaseAdapter struct {
	Version     string `yaml:"version"`
	VersionFrom string `yaml:"versionFrom"`
	SkipInstall bool   `yaml:"skipInstall"`
}

type HelmfileAdapter struct{ BaseAdapter }
type TerraformAdapter struct{ BaseAdapter }
type PulumiAdapter struct{ BaseAdapter }

func (a BaseAdapter) Validate() error {
	if a.Version == "" && a.VersionFrom == "" && !a.SkipInstall {
		return fmt.Errorf("version, versionFrom, or skipInstall must be set")
	}
	if a.Version != "" && a.VersionFrom != "" {
		return fmt.Errorf("version and versionFrom cannot be set at the same time")
	}

	return nil
}

func (a BaseAdapter) GetVersion() string {
	return a.Version
}

func (a HelmfileAdapter) GetName() string {
	return "helmfile"
}

func (a HelmfileAdapter) GetPlotName() string {
	return "diff"
}

func (a HelmfileAdapter) GetWorkspace(p Project) string {
	return p.Environment
}

func (a TerraformAdapter) GetName() string {
	return "terraform"
}

func (a TerraformAdapter) GetPlotName() string {
	return "plan"
}

func (a TerraformAdapter) GetWorkspace(p Project) string {
	return p.Workspace
}

func (a PulumiAdapter) GetName() string {
	return "pulumi"
}

func (a PulumiAdapter) GetPlotName() string {
	return "preview"
}

func (a PulumiAdapter) GetWorkspace(p Project) string {
	return p.Stack
}
