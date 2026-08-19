package plugin

import (
	"context"
	"slices"
	"strings"
)

// HelmfilePlugin implements Plugin by shelling out to the helmfile CLI.
type HelmfilePlugin struct {
	run commandRunner
}

// NewHelmfilePlugin constructs a HelmfilePlugin backed by a real helmfile
// subprocess.
func NewHelmfilePlugin() *HelmfilePlugin {
	return &HelmfilePlugin{run: execCommand}
}

func (p *HelmfilePlugin) Name() string { return "helmfile" }

func (p *HelmfilePlugin) GetOperations() []string {
	return []string{"diff", "apply", "sync", "destroy"}
}

func (p *HelmfilePlugin) GetPlanOperation() string  { return "diff" }
func (p *HelmfilePlugin) GetApplyOperation() string { return "apply" }

func (p *HelmfilePlugin) Execute(ctx context.Context, operation string, opts ExecuteOptions) (*ExecuteResult, error) {
	if !slices.Contains(p.GetOperations(), operation) {
		return nil, &UnsupportedOperationError{
			Plugin:    p.Name(),
			Operation: operation,
			Supported: p.GetOperations(),
		}
	}

	var args []string
	if env := opts.Config["environment"]; env != "" {
		args = append(args, "--environment", env)
	}
	args = append(args, operation)
	args = append(args, opts.ExtraArgs...)

	stdout, stderr, exitCode, err := p.run(ctx, opts.WorkingDir, "helmfile", args)
	if err != nil {
		return nil, err
	}

	output := string(stdout)
	if len(stderr) > 0 {
		if output != "" {
			output += "\n"
		}
		output += string(stderr)
	}

	var summary ChangeSummary
	if operation == "diff" {
		summary.Change = parseChangedReleases(strings.TrimRight(output, "\n"))
	}

	return &ExecuteResult{
		Output:        output,
		ChangeSummary: summary,
		PlanData:      nil,
		ExitCode:      exitCode,
		Error:         nil,
	}, nil
}
