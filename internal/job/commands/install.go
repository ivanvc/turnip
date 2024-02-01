package commands

import (
	"github.com/ivanvc/turnip/internal/job/plugin"
	"github.com/ivanvc/turnip/internal/yaml"
)

func InstallTool(dir, repoDir string, project yaml.Project) error {
	p := plugin.Load(project)
	return p.InstallTool(dir, repoDir)
}
