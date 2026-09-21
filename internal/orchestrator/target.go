package orchestrator

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/ivanvc/turnip/internal/config"
	"github.com/ivanvc/turnip/internal/github"
	"github.com/ivanvc/turnip/internal/lock"
)

// selectorAll is the reserved word meaning every candidate Project.
//
// It is matched by exact equality and resolved before anything is treated
// as a pattern, which is a correctness requirement rather than a
// preference: doublestar's "*" stops at a separator, so evaluating this as
// a glob would silently drop every Project named for its path — exactly
// the repositories that motivated patterns. config rejects a Project whose
// name contains "*", which is what makes "contains a star" a total test.
const selectorAll = "*"

// Target is one (Project, Operation) pair resolved from a TriggerCommand
// (Requirement 4) or from auto-plan matching (Requirement 2) — the unit
// executeOne creates at most one Runner Job and one check run for.
type Target struct {
	Project     config.Project
	Operation   string
	ExtraArgs   []string
	TriggeredBy string

	// Clone is the repository-scoped clone configuration, carried here
	// because executeOne is handed a Target and nothing else: the parsed
	// *config.Config does not survive target construction, so a setting
	// that belongs to the file rather than to a Project would otherwise
	// have no route to the execution path.
	//
	// Every Target matched by one pull request carries the same value —
	// mild redundancy, accepted so that resolving and refusing it keeps
	// the identical shape to runner.serviceAccount's.
	Clone config.CloneSpec
}

// planTargetsFor builds one Target per matched Project at its tool's
// GetPlanOperation(), TriggeredBy "auto" (Requirement 2.3). A Project
// whose tool isn't in the registry is silently skipped — this shouldn't
// normally happen, since config.Parse already rejects an unsupported
// Tool value before a Project can reach here.
func planTargetsFor(matched []config.Project, plugins PluginRegistry, clone config.CloneSpec) []Target {
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
			Clone:       clone,
		})
	}
	return targets
}

// selection carries everything target resolution needs beyond the command
// itself. It is assembled once per issue_comment event and asked about
// each TriggerCommand in turn.
//
// A struct rather than more parameters: the function this replaced took eight,
// and Slice 21 adds a pull request number, a modified-files lookup and a
// lock query to that list. locks is the existing lock.LockManager rather
// than a one-method interface invented for this call site — a second
// abstraction over the same thing is the split this repository already
// declined for Plugins.
type selection struct {
	cfg        *config.Config
	plugins    PluginRegistry
	owner      string
	repo       string
	author     string
	prNumber   int
	authorizer *github.Authorizer
	locks      lock.LockManager

	// modifiedSet returns the pull request's changed files, fetching at
	// most once per event (see memoModifiedFiles). Only a bare plan reads
	// it, so a trigger that names its Projects never pays for the call.
	modifiedSet func(context.Context) ([]string, error)
}

// gitHubMaxChangedFiles is the most files GitHub's pull-request files
// endpoint will return, after which the listing is silently truncated.
//
// turnip cannot see past it, so it says so instead: a Modified_Set built
// from a truncated listing would quietly target fewer Projects than the
// pull request actually touches, and a plan that skipped something looks
// exactly like a plan that had less to do.
const gitHubMaxChangedFiles = 3000

// resolve implements Requirement 4.1-4.7: resolving a TriggerCommand
// into the Targets it should execute, the per-Project rejections it
// produced along the way, and a whole-command error when the command
// itself can't be resolved to any candidate at all (an unmatched named
// Project).
func (s *selection) resolve(ctx context.Context, cmd *github.TriggerCommand) (targets []Target, rejected []github.ProjectResult, notices []string, wholeCommandErr error) {
	candidates := toolCandidates(s.cfg, cmd.Tool)

	if len(cmd.Projects) > 0 {
		names, patterns, all := splitSelectors(cmd.Projects)
		switch {
		case all && (len(names) > 0 || len(patterns) > 0):
			return nil, nil, nil, &MixedSelectorError{Others: append(names, patterns...)}
		case all:
			// Every candidate, which is what candidates already holds.
		default:
			narrowed, unmatched, err := narrowBySelectors(candidates, names, patterns)
			if err != nil {
				return nil, nil, nil, err
			}
			candidates = narrowed
			for _, pattern := range unmatched {
				notices = append(notices, unmatchedPatternNotice(pattern))
			}
		}
	} else {
		narrowed, bareNotices, err := s.bareDefaults(ctx, cmd, candidates)
		notices = append(notices, bareNotices...)
		if err != nil {
			return nil, nil, notices, err
		}
		candidates = narrowed
	}

	for _, project := range candidates {
		p, ok := s.plugins[project.Tool]
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
			hasWrite, err := s.authorizer.HasWritePermission(ctx, s.owner, s.repo, s.author)
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
			TriggeredBy: s.author,
			Clone:       s.cfg.Clone,
		})
	}

	return targets, rejected, notices, nil
}

// bareDefaults narrows candidates for a trigger that named no selector:
// a plan targets what the pull request modified (Requirement 1.1), and
// anything else targets what this pull request has planned
// (Requirement 4.1).
//
// The split is per-candidate rather than per-command because operation
// names are tool-native — "diff" is Helmfile's plan and means nothing to
// Pulumi. A candidate whose Plugin does not recognise the operation is
// kept rather than filtered out, so resolve still rejects it by name
// instead of it vanishing with nothing said.
func (s *selection) bareDefaults(ctx context.Context, cmd *github.TriggerCommand, candidates []config.Project) ([]config.Project, []string, error) {
	var notices []string
	var planKind, mutating []config.Project

	keep := make(map[string]bool, len(candidates))
	for _, p := range candidates {
		plug, ok := s.plugins[p.Tool]
		switch {
		case !ok || !operationRecognized(plug.GetOperations(), cmd.Operation):
			keep[p.Name] = true
		case cmd.Operation == plug.GetPlanOperation():
			planKind = append(planKind, p)
		default:
			mutating = append(mutating, p)
		}
	}

	if len(planKind) > 0 {
		files, err := s.modifiedSet(ctx)
		if err != nil {
			return nil, notices, err
		}
		if len(files) >= gitHubMaxChangedFiles {
			notices = append(notices, truncatedListingNotice(cmd))
		}
		// The automatic plan's own matcher, not a parallel implementation
		// of it (Requirement 1.2).
		for _, p := range config.MatchProjects(planKind, files) {
			keep[p.Name] = true
		}
	}

	for _, p := range mutating {
		held, err := s.holdsPlan(ctx, p)
		if err != nil {
			return nil, notices, err
		}
		if held {
			keep[p.Name] = true
		}
	}

	var selected []config.Project
	for _, p := range candidates {
		if keep[p.Name] {
			selected = append(selected, p)
		}
	}

	if len(selected) == 0 {
		if len(planKind) > 0 {
			return nil, notices, &NoModifiedProjectsError{Tool: cmd.Tool, Operation: cmd.Operation}
		}
		return nil, notices, &NoPlannedProjectsError{Tool: cmd.Tool, Operation: cmd.Operation}
	}
	return selected, notices, nil
}

// holdsPlan reports whether this pull request's Lock on p carries a
// recorded plan — exactly the question Requirement 4.1 selects on, and
// answerable from GetLockStatus without a new lock method.
//
// One round trip per candidate, accepted deliberately at the pilot's
// scale. When that stops being true the fix is shared rather than local:
// Slice 28 needs to enumerate every Lock for its listing page, and a
// SCAN-based ListLocks would replace this single call site.
func (s *selection) holdsPlan(ctx context.Context, p config.Project) (bool, error) {
	status, err := s.locks.GetLockStatus(ctx, projectKey(s.owner, s.repo, p.Name))
	if err != nil || status == nil {
		return false, err
	}
	// The same condition admission uses, so a bare mutating Operation
	// selects exactly the Projects it could actually run on — rather than
	// gathering Projects whose plan is no longer valid and refusing them
	// one by one.
	return status.Locked && status.PRNumber == s.prNumber && status.State == lock.StatePlanReady, nil
}

// truncatedListingNotice warns that the Modified_Set may be incomplete
// (Requirement 1.4). It fires when the listing comes back holding exactly
// the maximum, which also catches the pull request that happens to change
// precisely that many files — a spurious warning on an enormous pull
// request, which is the right direction to be wrong in.
func truncatedListingNotice(cmd *github.TriggerCommand) string {
	return fmt.Sprintf(
		"This pull request changes more files than GitHub will list (%d), so the projects matched below may be incomplete. Run `/%s %s *` to target every project.",
		gitHubMaxChangedFiles, cmd.Tool, cmd.Operation,
	)
}

// splitSelectors classifies a trigger's selector tokens. A token equal to
// "*" is the reserved word; a token containing "*" anywhere else is a
// pattern; anything else is an exact Project name.
func splitSelectors(tokens []string) (names, patterns []string, all bool) {
	for _, t := range tokens {
		switch {
		case t == selectorAll:
			all = true
		case strings.Contains(t, selectorAll):
			patterns = append(patterns, t)
		default:
			names = append(names, t)
		}
	}
	return names, patterns, all
}

// narrowBySelectors resolves names and patterns against candidates,
// returning their union in configuration order plus any pattern that
// matched nothing.
//
// The two kinds fail differently on purpose. A name that matches nothing
// is a typo worth stopping the whole command for, so it errors. A pattern
// that matches nothing is more often a live question — "did anything under
// gcp change?" — so it is reported and the rest of the command proceeds
// (Requirement 2.7).
func narrowBySelectors(candidates []config.Project, names, patterns []string) (selected []config.Project, unmatchedPatterns []string, err error) {
	chosen := make(map[string]bool, len(candidates))

	if len(names) > 0 {
		named, err := narrowByName(candidates, names)
		if err != nil {
			return nil, nil, err
		}
		for _, p := range named {
			chosen[p.Name] = true
		}
	}

	matched, unmatched := config.MatchProjectsByName(candidates, patterns)
	for _, p := range matched {
		chosen[p.Name] = true
	}

	for _, p := range candidates {
		if chosen[p.Name] {
			selected = append(selected, p)
		}
	}
	return selected, unmatched, nil
}

// unmatchedPatternNotice quotes the pattern back (Requirement 2.8), which
// is what makes a transposed "gpc/*" diagnosable at a glance without
// treating a legitimately empty question as a failure.
func unmatchedPatternNotice(pattern string) string {
	return fmt.Sprintf("No project matched `%s`.", pattern)
}

// resolveUnlockCandidates implements Requirement 5.1: the same target
// resolution as resolve's 4.1-4.4 (tool/name filtering), but
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
