package plugin

import "github.com/ivanvc/turnip/internal/yaml"

type Plugin interface {
	// PlanCommand returns the command to run to plan the project.
	//PlanCommand() string
	// ApplyCommand returns the command to run to apply the project.
	//ApplyCommand() string
	// Workspace returns the workspace/sack/environment to use for the project.
	//Workspace() string
	// Version returns the version of the plugin.
	//Version() string
	Install(string, string) (string, error)

	Plan(string, string) (bool, []byte, error)

	RunPreCommands(string, []yaml.Command) ([]byte, error)
}

func Load(project yaml.Project) Plugin {
	switch project.LoadedWorkflow.Type {
	case yaml.ProjectTypePulumi:
		return &Pulumi{project: project}
	default:
		return nil
	}
}
