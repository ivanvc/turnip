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

func Plan(binDir, repoDir string, project yaml.Project) (bool, []byte, error) {
	p := plugin.Load(project)
	return p.Plan(binDir, repoDir)
}

func PlanPreCommands(repoDir string, project yaml.Project) ([]byte, error) {
	p := plugin.Load(project)
	w := project.LoadedWorkflow
	log.Info("checking if there are pre-commands to run", "project", project, "workflow", w)
	if len(w.PreCommands) == 0 {
		return []byte{}, nil
	}
	log.Info("Running pre-commands", "preCommands", w.PreCommands)
	return p.RunPreCommands(filepath.Join(repoDir, project.Dir), w.PreCommands)
}
