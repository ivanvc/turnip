package orchestrator

import (
	"github.com/ivanvc/turnip/internal/lock"
	"github.com/ivanvc/turnip/internal/plugin"
)

// lockEventFor classifies a finished Operation into the Event its Lock
// sees, together with the plan to record when the edge keeps one.
//
// unreported is true when the tool could not report the plan's counts
// (a quiet Helmfile diff). Such a plan may have something to apply, so it
// is kept as one rather than released as a plan with no changes.
//
// An unregistered tool is treated as mutating rather than as a plan. That
// is the conservative reading: a mutating Operation never releases on
// failure, so a tool turnip cannot identify can never talk turnip into
// freeing a Project it knows nothing about.
func (o *Orchestrator) lockEventFor(rec *OperationRecord, success bool, changes plugin.ChangeSummary, unreported bool, planData []byte) (lock.Event, *lock.PlanRecord) {
	p, registered := o.plugins[rec.Project.Tool]
	if !registered || rec.Operation != p.GetPlanOperation() {
		if success {
			return lock.EventMutatingSucceeded, nil
		}
		return lock.EventMutatingFailed, nil
	}

	if !success {
		return lock.EventPlanFailed, nil
	}

	// "Something to apply" is the plan's own verdict *or* the tool's:
	// Helmfile answers true on account of sync, which upgrades every
	// release regardless of the diff. A plan whose counts were not
	// reported has no zero to trust, so it counts as something to apply.
	if changes.Add != 0 || changes.Change != 0 || changes.Destroy != 0 || unreported || p.ActsWithoutChanges() {
		// The arguments stored are the Operation's own, taken from the
		// record the Job was built from rather than re-derived from the
		// trigger line, so whatever turnip normalized is what a later
		// mutating Operation replays.
		//
		// Whether the counts were reported is kept with them, so the
		// mutating Operation replaying the plan titles its check
		// "succeeded" rather than "no changes".
		return lock.EventPlanApplicable, &lock.PlanRecord{
			Data:       planData,
			Args:       rec.ExtraArgs,
			Summary:    changes,
			Unreported: unreported,
		}
	}

	return lock.EventPlanNothingToApply, nil
}

// lockNoteFor renders why the Lock moved, for the pull request to say.
//
// The message belongs to the *edge*, not to the state arrived at: several
// edges end in StatePlanStale for different causes, and "this plan is
// stale" tells an author nothing they can act on.
func lockNoteFor(ev lock.Event, tr lock.Transition) string {
	if tr.Released {
		switch ev {
		case lock.EventPlanFailed:
			return "Lock released: the plan failed, so nothing was recorded. This project is free for another pull request to plan against."
		case lock.EventPlanNothingToApply:
			return "Lock released: the plan found nothing to apply. This project is free for another pull request to plan against."
		case lock.EventMutatingSucceeded:
			return "Lock released: the operation completed. This project is free for another pull request to plan against."
		default:
			return "Lock released. This project is free for another pull request to plan against."
		}
	}

	// Entering staleness is news; already being stale is not, except when
	// a re-plan just failed to lift it.
	if tr.To == lock.StatePlanStale && tr.From != lock.StatePlanStale {
		switch ev {
		case lock.EventMutatingFailed:
			return "The stored plan is no longer valid: this operation failed part-way, so infrastructure may have changed since it was planned. Re-plan before retrying. The lock is still held, so no other pull request can act on this project meanwhile."
		case lock.EventMutatingTimedOut:
			return "The stored plan is no longer valid: no result arrived, and the operation may still be running. Re-plan before retrying. The lock is still held."
		case lock.EventPlanDispatched:
			return "The stored plan was superseded by a new commit. Re-plan before applying."
		}
	}

	if tr.From == lock.StatePlanStale && ev == lock.EventPlanFailed {
		return "The plan failed, so the previously stored plan is still unusable. The lock is still held; a successful plan is required before applying."
	}

	return ""
}
