package orchestrator

import (
	"fmt"

	"github.com/ivanvc/turnip/internal/github"
	"github.com/ivanvc/turnip/internal/plugin"
	"github.com/ivanvc/turnip/internal/rpc"
)

// Every check run title turnip sets is worded here, so this file is the
// one place to read what the checks list can say (check-run-titles
// Requirement 1).
//
// The rule every function follows: a title never repeats the status icon
// beside it. When turnip knows the cause of an outcome, the title names
// it; when it does not — a tool exiting non-zero — the title states the
// plain fact and does not guess, because a confident wrong summary in a
// list is what people act on without clicking.
//
// Titles are plain text: not HTML, not markdown, and parts are joined with
// commas. Counts and scope come from the same renderers the pull request
// comment uses, so an outcome reads the same in both.

// completedTitle is a successful Operation's: what it changed, and the
// scope it ran with.
func completedTitle(changes github.ChangeCounts, scope []string) string {
	return withScope(github.ChangeText(changes), scope)
}

// runningTitle is a Plan_Operation's while it runs. The Operation and
// Project are already in the check's name, so only the scope adds
// anything.
func runningTitle(scope []string) string {
	return withScope("running", scope)
}

// runningRecordedPlanTitle is a Mutating_Operation's while it runs: what
// it is about to change, from the plan the Lock recorded — the line
// someone watching an apply wants.
func runningRecordedPlanTitle(changes github.ChangeCounts, scope []string) string {
	return withScope("running the recorded plan, "+github.ChangeText(changes), scope)
}

// failedTitle names the step that failed. Only "the tool exited" carries
// an exit code, since only there is it the tool's; turnip's own -1 marker
// never reaches a title.
func failedTitle(tool string, category rpc.FailureCategory, exitCode int32) string {
	switch category {
	case rpc.FailureToolExited:
		return fmt.Sprintf("%s exited %d", tool, exitCode)
	case rpc.FailureCloneFailed:
		return "clone failed"
	case rpc.FailureWorkspaceFailed:
		return "workspace could not be prepared"
	case rpc.FailureToolNotStarted:
		return tool + " could not be started"
	default:
		return "failed"
	}
}

// jobNotCreatedTitle is for an Operation whose Runner Job Kubernetes
// refused. The cause is turnip's own and known, so it is named, rather
// than reading like the tool rejecting the author's change.
func jobNotCreatedTitle() string {
	return "Runner Job could not be created"
}

// timeoutTitle is the diagnostic the sweep already builds from the Job's
// state — a known cause, named.
func timeoutTitle(diagnostic string) string {
	return diagnostic
}

// aggregateTitle counts Projects up to date — applied, or planned with
// nothing to apply — which is what the turnip check guarantees before
// merge: the infrastructure matches this pull request.
func aggregateTitle(upToDate, total, failed int) string {
	title := fmt.Sprintf("%d/%d projects up to date", upToDate, total)
	if failed > 0 {
		title += fmt.Sprintf(", %d failed", failed)
	}
	return title
}

// lockWaitTitle is a Lock_Wait's: the plan is queued behind another pull
// request, not failed, so the title says who holds the Lock and what to
// do once it is released. The holder is named when the Lock records it;
// 0 means it could not be read, and a PR number is never guessed.
func lockWaitTitle(blockedBy int) string {
	if blockedBy > 0 {
		return fmt.Sprintf("locked by PR #%d, re-plan once it's released", blockedBy)
	}
	return "locked by another pull request, re-plan once it's released"
}

// notPermittedTitle is a Configuration_Refusal's Project_Check: the
// setting named exactly as TURNIP_ALLOWED_OVERRIDES spells it, so the fix
// can be found from the title alone.
func notPermittedTitle(setting string) string {
	return setting + " is not permitted"
}

// refusedTitle is the turnip check's when a Project was refused: it names
// the first and counts the rest, because the title is one line and the
// summary beneath lists them all.
func refusedTitle(project, setting string, more int) string {
	title := fmt.Sprintf("not permitted: %s sets %s", project, setting)
	if more > 0 {
		title += fmt.Sprintf(", and %d more", more)
	}
	return title
}

// lockNotAcquiredTitle, recordNotSavedTitle and jobNotBuiltTitle are
// Infrastructure_Errors: turnip's own failures before the tool ran. Like
// jobNotCreatedTitle, each names the step, so none reads like the tool
// rejecting the author's change.
func lockNotAcquiredTitle() string {
	return "lock could not be acquired"
}

func recordNotSavedTitle() string {
	return "operation could not be recorded"
}

func jobNotBuiltTitle() string {
	return "Runner Job could not be built"
}

// notStartedTitle completes a check that already exists for a refusal
// that names no step of its own. Every site today names one; this is the
// fallback that keeps a later site from leaving the check in progress.
func notStartedTitle() string {
	return "operation could not be started"
}

func noProjectsAffectedTitle() string {
	return "no projects affected"
}

func invalidConfigTitle() string {
	return "invalid turnip.yaml"
}

// changeCounts converts a Plugin's counts, as the Lock records them, into
// the comment's type the renderers take.
func changeCounts(c plugin.ChangeSummary) github.ChangeCounts {
	return github.ChangeCounts{Add: c.Add, Change: c.Change, Destroy: c.Destroy}
}

func withScope(title string, scope []string) string {
	if text := github.ScopeText(scope); text != "" {
		return title + ", " + text
	}
	return title
}
