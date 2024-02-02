package commands

import (
	"path/filepath"

	"github.com/ivanvc/turnip/internal/job/plugin"
	"github.com/ivanvc/turnip/internal/yaml"
)

func InstallTool(dir, repoDir string, project yaml.Project) error {
	p := plugin.Load(project)
	return p.InstallTool(dir, repoDir)
}

func RunToolPlan(dir, repoDir string, project yaml.Project) ([]byte, error) {
	b := filepath.Join(dir, "pulumi")
	d := filepath.Join(repoDir, project.Dir)
	p := plugin.Load(project)
	return p.RunToolPlan(b, d, project.Stack)
}
