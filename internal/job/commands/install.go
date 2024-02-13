package commands

import (
	"path/filepath"

	"github.com/charmbracelet/log"
	"github.com/ivanvc/turnip/internal/job/plugin"
	"github.com/ivanvc/turnip/internal/yaml"
)

func Install(dir, repoDir string, project yaml.Project) ([]byte, error) {
	p := plugin.Load(project)
	return p.InstallDependencies(dir, repoDir)
}

func Plot(repoDir string, project yaml.Project) (bool, []byte, error) {
	p := plugin.Load(project)
	return p.Plot(repoDir)
}

func Lift(repoDir string, project yaml.Project) (bool, []byte, error) {
	p := plugin.Load(project)
	return p.Lift(repoDir)
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
