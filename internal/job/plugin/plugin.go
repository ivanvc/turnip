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

	Plot(string, string) (bool, []byte, error)

	RunInitCommands(string) ([]byte, error)
}

func Load(project yaml.Project) Plugin {
	a, err := project.LoadedWorkflow.GetAdapter()
	if err != nil {
		return nil
	}
	switch a.GetName() {
	case "pulumi":
		return Pulumi{project: project}
	default:
		return nil
	}
}
