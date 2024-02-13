package yaml

import "fmt"

type Adapter interface {
	GetName() string
	GetPlotName() string
	GetLiftName() string
	GetVersion() string
	GetWorkspace(Project) string
	Validate() error
}

type HelmfileAdapter struct {
	Version     string `yaml:"version"`
	VersionFrom string `yaml:"versionFrom"`
	SkipInstall bool   `yaml:"skipInstall"`
}

func (HelmfileAdapter) GetName() string                 { return "helmfile" }
func (HelmfileAdapter) GetPlotName() string             { return "diff" }
func (HelmfileAdapter) GetLiftName() string             { return "apply" }
func (a HelmfileAdapter) GetVersion() string            { return a.Version }
func (a HelmfileAdapter) GetWorkspace(p Project) string { return p.Environment }
func (a HelmfileAdapter) Validate() error {
	return validateAdapter(a.Version, a.VersionFrom, a.SkipInstall)
}

type TerraformAdapter struct {
	Version     string `yaml:"version"`
	VersionFrom string `yaml:"versionFrom"`
	SkipInstall bool   `yaml:"skipInstall"`
}

func (TerraformAdapter) GetName() string                 { return "terraform" }
func (TerraformAdapter) GetPlotName() string             { return "plan" }
func (TerraformAdapter) GetLiftName() string             { return "apply" }
func (a TerraformAdapter) GetVersion() string            { return a.Version }
func (a TerraformAdapter) GetWorkspace(p Project) string { return p.Workspace }
func (a TerraformAdapter) Validate() error {
	return validateAdapter(a.Version, a.VersionFrom, a.SkipInstall)
}

type PulumiAdapter struct {
	Version     string `yaml:"version"`
	VersionFrom string `yaml:"versionFrom"`
	SkipInstall bool   `yaml:"skipInstall"`
}

func (PulumiAdapter) GetName() string                 { return "pulumi" }
func (PulumiAdapter) GetPlotName() string             { return "preview" }
func (PulumiAdapter) GetLiftName() string             { return "up" }
func (a PulumiAdapter) GetVersion() string            { return a.Version }
func (a PulumiAdapter) GetWorkspace(p Project) string { return p.Stack }
func (a PulumiAdapter) Validate() error {
	return validateAdapter(a.Version, a.VersionFrom, a.SkipInstall)
}

func validateAdapter(version, versionFrom string, skipInstall bool) error {
	if version == "" && versionFrom == "" && !skipInstall {
		return fmt.Errorf("version, versionFrom, or skipInstall must be set")
	}
	if version != "" && versionFrom != "" {
		return fmt.Errorf("version and versionFrom cannot be set at the same time")
	}

	return nil
}
