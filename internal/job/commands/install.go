package commands

import (
	"path/filepath"

	"github.com/charmbracelet/log"
	"github.com/ivanvc/turnip/internal/job/plugin"
	"github.com/ivanvc/turnip/internal/yaml"
)

func Install(dir, repoDir string, project yaml.Project) (string, error) {
	p := plugin.Load(project)
	return p.Install(dir, repoDir)
}

func Plot(binDir, repoDir string, project yaml.Project) (bool, []byte, error) {
	p := plugin.Load(project)
	return p.Plot(binDir, repoDir)
}

func RunInitCommands(repoDir string, project yaml.Project) ([]byte, error) {
	p := plugin.Load(project)
	w := project.LoadedWorkflow
	log.Info("checking if there are pre-commands to run", "project", project, "workflow", w)
	if len(w.InitCommands) == 0 {
		return []byte{}, nil
	}
	log.Info("Running pre-commands", "preCommands", w.InitCommands)
	return p.RunInitCommands(filepath.Join(repoDir, project.Dir))
}
