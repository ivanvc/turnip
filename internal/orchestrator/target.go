package orchestrator

import (
	"context"
	"slices"

	"github.com/ivanvc/turnip/internal/config"
	"github.com/ivanvc/turnip/internal/github"
)

// Target is one (Project, Operation) pair resolved from a TriggerCommand
// (Requirement 4) or from auto-plan matching (Requirement 2) — the unit
// executeOne creates at most one Runner Job and one check run for.
type Target struct {
	Project     config.Project
	Operation   string
	ExtraArgs   []string
	TriggeredBy string
}

// planTargetsFor builds one Target per matched Project at its tool's
// GetPlanOperation(), TriggeredBy "auto" (Requirement 2.3). A Project
// whose tool isn't in the registry is silently skipped — this shouldn't
// normally happen, since config.Parse already rejects an unsupported
// Tool value before a Project can reach here.
func planTargetsFor(matched []config.Project, plugins PluginRegistry) []Target {
	targets := make([]Target, 0, len(matched))
	for _, project := range matched {
		p, ok := plugins[project.Tool]
		if !ok {
			continue
		}
		targets = append(targets, Target{
			Project:     project,
			Operation:   p.GetPlanOperation(),
			TriggeredBy: "auto",
		})
	}
	return targets
}

// resolveTargets implements Requirement 4.1-4.7: resolving a TriggerCommand
// into the Targets it should execute, the per-Project rejections it
// produced along the way, and a whole-command error when the command
// itself can't be resolved to any candidate at all (an unmatched named
// Project).
func resolveTargets(
	ctx context.Context,
	cfg *config.Config,
	plugins PluginRegistry,
	cmd *github.TriggerCommand,
	owner, repo, author string,
	authorizer *github.Authorizer,
) (targets []Target, rejected []github.ProjectResult, wholeCommandErr error) {
	candidates := toolCandidates(cfg, cmd.Tool)

	if len(cmd.Projects) > 0 {
		narrowed, err := narrowByName(candidates, cmd.Projects)
		if err != nil {
			return nil, nil, err
		}
		candidates = narrowed
	}

	for _, project := range candidates {
		p, ok := plugins[project.Tool]
		if !ok || !operationRecognized(p.GetOperations(), cmd.Operation) {
			rejected = append(rejected, github.ProjectResult{
				ProjectName: project.Name,
				Tool:        project.Tool,
				Operation:   cmd.Operation,
				Success:     false,
				Output:      "operation \"" + cmd.Operation + "\" is not recognized for tool \"" + project.Tool + "\"",
			})
			continue
		}

		if cmd.Operation != p.GetPlanOperation() {
			hasWrite, err := authorizer.HasWritePermission(ctx, owner, repo, author)
			if err != nil || !hasWrite {
				rejected = append(rejected, github.ProjectResult{
					ProjectName: project.Name,
					Tool:        project.Tool,
					Operation:   cmd.Operation,
					Success:     false,
					Output:      "write permission is required to run \"" + cmd.Operation + "\"",
				})
				continue
			}
		}

		targets = append(targets, Target{
			Project:     project,
			Operation:   cmd.Operation,
			ExtraArgs:   cmd.ExtraArgs,
			TriggeredBy: author,
		})
	}

	return targets, rejected, nil
}

// resolveUnlockCandidates implements Requirement 5.1: the same target
// resolution as resolveTargets' 4.1-4.4 (tool/name filtering), but
// skipping 4.5's Plugin-operation validation entirely — "unlock" is never
// a Plugin operation.
func resolveUnlockCandidates(cfg *config.Config, cmd *github.TriggerCommand) ([]config.Project, error) {
	candidates := toolCandidates(cfg, cmd.Tool)
	if len(cmd.Projects) == 0 {
		return candidates, nil
	}
	return narrowByName(candidates, cmd.Projects)
}

// toolCandidates implements Requirement 4.1/4.2: "turnip" considers every
// configured Project regardless of tool; a specific tool name narrows to
// Projects using exactly that tool.
func toolCandidates(cfg *config.Config, tool string) []config.Project {
	if tool == "turnip" {
		return cfg.Projects
	}
	var candidates []config.Project
	for _, p := range cfg.Projects {
		if p.Tool == tool {
			candidates = append(candidates, p)
		}
	}
	return candidates
}

// narrowByName implements Requirement 4.3: narrow to the named Projects,
// erroring if any listed name matches no candidate.
func narrowByName(candidates []config.Project, names []string) ([]config.Project, error) {
	byName := make(map[string]config.Project, len(candidates))
	for _, p := range candidates {
		byName[p.Name] = p
	}

	var narrowed []config.Project
	for _, name := range names {
		p, ok := byName[name]
		if !ok {
			return nil, &UnmatchedProjectError{Name: name}
		}
		narrowed = append(narrowed, p)
	}
	return narrowed, nil
}

func operationRecognized(operations []string, operation string) bool {
	return slices.Contains(operations, operation)
}
