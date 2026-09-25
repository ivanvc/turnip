package orchestrator

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/ivanvc/turnip/internal/github"
	"github.com/ivanvc/turnip/internal/lock"
)

// aggregatePublishRounds is a guard against a bug, not a tuning knob. A
// round repeats only when another write for the same commit landed while
// this instance talked to GitHub, so the rounds a publisher needs grow
// with the number of Outcomes arriving at once — a pull request applying
// many Projects together. Capping it low is not safe: a slow publisher
// that gives up while its own stale verdict is the last one GitHub
// received leaves the check wrong, which the convergence property test
// found with a cap of five.
const aggregatePublishRounds = 100

// recordOutcome writes one Project's Outcome and publishes the resulting
// verdict. Any Server instance may call it for any pull request: the
// verdict is computed from the whole shared record, never from what this
// instance happens to have seen (Requirement 8.1).
func (o *Orchestrator) recordOutcome(ctx context.Context, client github.GitHubClient, ref prRef, project string, entry ProjectEntry) error {
	if err := o.records.WriteOutcome(ctx, ref, project, entry); err != nil {
		return err
	}
	return o.publishAggregate(ctx, client, ref)
}

// publishAggregate brings the Aggregate_Check in line with the record.
//
// No lock is involved. Every writer publishes after writing, and after
// publishing re-reads the record: if the version (or the check run it
// names) moved while this instance was talking to GitHub, it publishes
// again. Whichever instance publishes last therefore publishes the latest
// record — including when two updates reach GitHub in the reverse of the
// order their records were read.
//
// A completed check run is never reopened: GitHub does not document
// moving one back to in_progress. When the verdict leaves a completed
// state, a new check run is created, and GitHub evaluates a required check
// by the most recently updated run of its name. Concurrent first
// publishes can create a duplicate for the same reason, and it is
// harmless for the same reason — the run kept current is the one updated
// last.
func (o *Orchestrator) publishAggregate(ctx context.Context, client github.GitHubClient, ref prRef) error {
	for range aggregatePublishRounds {
		st, err := o.records.ReadPRStatus(ctx, ref)
		if err != nil {
			return err
		}
		v := verdictFor(st)
		if !shouldPublish(st, v) {
			return nil
		}

		checkRunID := st.CheckRunID
		if st.CheckDone && !v.completed() {
			// Leaving a completed state: a new run rather than a reopened one.
			checkRunID = 0
		}
		if checkRunID != 0 {
			if err := client.UpdateCheckRun(ctx, ref.Owner, ref.Repo, checkRunID, v.options("")); err != nil {
				// The completed hint may have been stale, or the run may
				// be gone. Either way a fresh run carries the verdict.
				slog.WarnContext(ctx, "updating aggregate check run; creating a new one",
					"owner", ref.Owner, "repo", ref.Repo, "pr_number", ref.PRNumber, "check_run_id", checkRunID, "error", err)
				checkRunID = 0
			} else if err := o.records.SetAggregateCheckDone(ctx, ref, v.completed()); err != nil {
				return err
			}
		}
		if checkRunID == 0 {
			id, err := client.CreateCheckRun(ctx, ref.Owner, ref.Repo, v.options(ref.HeadSHA))
			if err != nil {
				return fmt.Errorf("creating the %s check run: %w", aggregateCheckName, err)
			}
			if err := o.records.SetAggregateCheckRun(ctx, ref, id, v.completed()); err != nil {
				return err
			}
			checkRunID = id
		}

		after, err := o.records.ReadPRStatus(ctx, ref)
		if err != nil {
			return err
		}
		if after.Version == st.Version && after.CheckRunID == checkRunID {
			return nil
		}
	}
	return fmt.Errorf("the %s check did not settle after %d rounds", aggregateCheckName, aggregatePublishRounds)
}

// options renders the verdict for GitHub. headSHA is set only on create;
// an update addresses the run by ID.
func (v aggregateVerdict) options(headSHA string) github.CheckRunOptions {
	return github.CheckRunOptions{
		Name:       aggregateCheckName,
		HeadSHA:    headSHA,
		Status:     v.Status,
		Conclusion: v.Conclusion,
		Title:      v.Title,
		Summary:    v.Summary,
	}
}

// appendAggregateNote is appendCheckRunNote's counterpart for the
// Aggregate_Check: a verdict that could not be recorded or published is
// said on the pull request rather than only logged (Requirement 8.2). The
// Operation itself is unaffected.
func appendAggregateNote(result github.ProjectResult, err error) github.ProjectResult {
	if err == nil {
		return result
	}
	result.Output += fmt.Sprintf(
		"\n\nNote: the `%s` check could not be updated for this result (%v). "+
			"The operation itself is unaffected; the check catches up on the next result for this commit.",
		aggregateCheckName, err,
	)
	return result
}

// recordFinishedOutcome records the Outcome of an Operation that has just
// been finalized — by its Runner's result or by the sweep's timeout — and
// returns pr with a note when the verdict could not be updated.
//
// Keyed by the commit the Operation ran against, not the pull request's
// current head: a result arriving after a push belongs to the commit it
// describes (design Decision 1).
func (o *Orchestrator) recordFinishedOutcome(ctx context.Context, client github.GitHubClient, rec *OperationRecord, ev lock.Event, pr github.ProjectResult) github.ProjectResult {
	outcome, ok := outcomeForEvent(ev)
	if !ok {
		return pr
	}
	ref := prRef{Owner: rec.Owner, Repo: rec.Repo, PRNumber: rec.PRNumber, HeadSHA: rec.HeadSHA}
	entry := ProjectEntry{Outcome: outcome, Operation: rec.Operation}
	if err := o.recordOutcome(ctx, client, ref, rec.Project.Name, entry); err != nil {
		slog.ErrorContext(ctx, "recording outcome for the aggregate check", "operation_id", rec.OperationID, "error", err)
		return appendAggregateNote(pr, err)
	}
	return pr
}
