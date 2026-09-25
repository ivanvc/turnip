package plugin

import (
	"context"
	"slices"
	"strings"

	"github.com/ivanvc/turnip/internal/provisioning"
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
	return []string{"diff", "apply", "sync"}
}

// ActsWithoutChanges is true because GetOperations exposes `sync`
// alongside `apply`. `helmfile sync` runs `helm upgrade --install` for
// every release regardless of the diff — it acts precisely when a diff
// found nothing, which is the whole of its difference from `apply`.
//
// `helmfile apply` alone would answer false: it diffs first and syncs
// only the releases that changed, so it is inert relative to the diff
// that preceded it. Answering from the apply operation alone would remove
// `sync` from every Project whose diff came back clean, permanently — a
// re-plan would find no changes and release the Lock again.
func (p *HelmfilePlugin) ActsWithoutChanges() bool { return true }

// Provisioning runs in helmfile's own image rather than copying a binary
// out, because helmfile is a runtime, not a binary: it shells out to
// `helm`, `helmfile diff` needs the helm-diff plugin, helm-secrets needs
// `sops`, and helm locates its plugins through an environment this image
// sets. Copying one binary out fails with `exec: "helm": executable file
// not found in $PATH`.
func (p *HelmfilePlugin) Provisioning() provisioning.Spec {
	return provisioning.Spec{
		Strategy: provisioning.RunInImage,
		Image:    "ghcr.io/helmfile/helmfile",
	}
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

	lines, exitCode, err := p.run(ctx, opts.WorkingDir, "helmfile", args, opts.ToolVersion, opts.OnOutput)
	if err != nil {
		return nil, err
	}

	var summary ChangeSummary
	if operation == "diff" {
		// stdout only. Helmfile writes "Comparing release=" and each
		// release's diff there, and its progress and errors ("Building
		// dependency", "Adding repo", a failed helm diff) to stderr — which,
		// interleaved into the record, would otherwise read as the body of
		// whichever release preceded it and count an unchanged one as
		// changed.
		summary.Change = parseChangedReleases(strings.TrimRight(streamText(lines, "stdout"), "\n"))
	}

	return &ExecuteResult{
		Output:        outputText(lines),
		ChangeSummary: summary,
		PlanData:      nil,
		ExitCode:      exitCode,
		Error:         nil,
	}, nil
}
