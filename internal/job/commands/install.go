package commands

import (
	"github.com/ivanvc/turnip/internal/job/plugin"
	"github.com/ivanvc/turnip/internal/yaml"
)

func Install(dir string, project yaml.Project) (string, error) {
	p := plugin.Load(project)
	return p.Install(dir)
}

func Plan(binDir, repoDir string, project yaml.Project) ([]byte, error) {
	p := plugin.Load(project)
	return p.Plan(binDir, repoDir)
}
