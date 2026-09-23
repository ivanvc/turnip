package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/ivanvc/turnip/internal/config"
	"github.com/ivanvc/turnip/internal/github"
	"github.com/ivanvc/turnip/internal/lock"
)

// HandlePullRequest reacts to pull_request webhooks: "opened"/"synchronize"
// trigger the auto-plan flow (Requirement 2), "closed" releases Locks
// (Requirement 11). Every other action is a no-op.
func (o *Orchestrator) HandlePullRequest(ctx context.Context, event *github.WebhookEvent) error {
	client := o.installationClient(event.Installation.ID)

	switch event.Action {
	case "opened", "synchronize", "ready_for_review", "reopened":
		// "reopened" joins this arm rather than getting one of its own,
		// so that everything wanted comes from the arm it joins: the
		// matched-Project set from handlePlanTrigger, the draft guard
		// below, and the fork refusal after it. A separate arm would have
		// to repeat the last two, and two copies of a security check
		// drift apart.
		//
		// A draft is work its author has marked unfinished, so turnip
		// does not act on it of its own accord: no Lock, no Runner Job,
		// no comment. A comment trigger still works — being a draft
		// changes when turnip acts on its own, never what it can be
		// asked to do.
		//
		// The test lives here rather than before the switch on purpose.
		// Guarding the whole handler would skip "closed" too, and a
		// draft's Locks would stop being released — silently, and for
		// exactly the pull requests most likely to be abandoned rather
		// than closed cleanly. Inside the arm, "closed" is unreachable
		// from it by construction.
		//
		// It also returns before handlePlanTrigger, whose first act is
		// fetching turnip.yaml, so a skipped draft costs one webhook and
		// no API call.
		//
		// "ready_for_review" needs no special case: GitHub sends it with
		// the payload's draft field already false, so it falls through
		// this test and plans as though the pull request had just been
		// opened.
		if event.PullRequest.Draft {
			return nil
		}
		// A pull request whose code comes from another repository is
		// refused outright: its head commit and the turnip.yaml read
		// from that commit are both chosen by whoever opened it, and an
		// automatic plan runs with no authorization check by design.
		//
		// Placed in this arm for the same reason the draft test is, and
		// it matters more here: guarding the whole handler would stop
		// "closed" releasing Locks, and a foreign pull request holding
		// one from before this check would strand it with no lifecycle
		// event left to discharge it.
		//
		// Before handlePlanTrigger, whose first act is fetching
		// turnip.yaml — on a foreign pull request that file is
		// attacker-controlled, so reading it is already a step too far.
		if event.PullRequest.IsForeign(event.Repository) {
			slog.WarnContext(ctx, "refusing operation on a pull request from another repository",
				"owner", event.Repository.Owner,
				"repo", event.Repository.Name,
				"pr_number", event.PullRequest.Number,
				"head_owner", event.PullRequest.HeadRepo.Owner,
				"head_repo", event.PullRequest.HeadRepo.Name,
				"actor", event.PullRequest.Author,
			)
			return github.ErrRefused
		}
		return o.handlePlanTrigger(ctx, client, event)
	case "closed":
		return o.handlePRClosed(ctx, client, event)
	default:
		return nil
	}
}

func (o *Orchestrator) handlePlanTrigger(ctx context.Context, client github.GitHubClient, event *github.WebhookEvent) error {
	cfg, err := fetchConfig(ctx, client, event.Repository.Owner, event.Repository.Name, event.PullRequest.HeadSHA)
	if err != nil {
		// A repository with no turnip.yaml anywhere hasn't opted into
		// turnip, and this handler runs on every PR open and every push
		// to one — commenting here would put "turnip.yaml was not found"
		// on every pull request in the repository. Stay silent; the
		// Operation is skipped either way.
		//
		// This is only true of the *automatic* trigger. An explicit
		// Trigger Comment still reports the missing file
		// (HandleIssueComment), because there a human asked turnip to do
		// something and silence would be the confusing answer. A
		// turnip.yaml that exists but is invalid also still comments
		// here: that repository *has* opted in, so its breakage should
		// be visible rather than silently skipped.
		if errors.Is(err, ErrConfigMissing) {
			return nil
		}
		// An invalid configuration is the author's to fix, so a required
		// `turnip` check fails for it, rather than waiting forever with the
		// reason only in the comment (aggregate-check-run Requirement 7.1).
		o.markAutomaticVerdict(ctx, client, prRefFor(event), o.records.MarkConfigInvalid)
		_, postErr := client.PostComment(ctx, event.Repository.Owner, event.Repository.Name, event.PullRequest.Number, configErrorComment(err))
		return postErr
	}

	modifiedFiles, err := client.GetModifiedFiles(ctx, event.Repository.Owner, event.Repository.Name, event.PullRequest.Number)
	if err != nil {
		return fmt.Errorf("orchestrator: getting modified files for %s/%s#%d: %w", event.Repository.Owner, event.Repository.Name, event.PullRequest.Number, err)
	}

	matched := config.MatchProjects(cfg.Projects, modifiedFiles)
	if len(matched) == 0 {
		// Nothing to plan is reported, as `skipped`: without it, requiring
		// `turnip` would block every pull request that touches no Project
		// (aggregate-check-run Requirement 6.3).
		o.markAutomaticVerdict(ctx, client, prRefFor(event), o.records.MarkEmpty)
		return nil
	}

	targets, unsupported := planTargetsFor(matched, o.plugins, cfg.Clone)

	// Detach from the request context: the webhook HTTP handler responds
	// as soon as this method returns, but executing Targets can take far
	// longer than GitHub's webhook delivery timeout allows for. Run it in
	// the background on a context stripped of the request's cancellation
	// (but not its values), and return immediately.
	go o.runTargetsAndPost(context.WithoutCancel(ctx), client, event.Repository, *event.PullRequest, event.Installation.ID, targets, unsupported)

	return nil
}

func (o *Orchestrator) runTargetsAndPost(ctx context.Context, client github.GitHubClient, repo github.Repository, pr github.PullRequest, installationID int64, targets []Target, unsupported []config.Project) {
	notices := o.recordUnsupported(ctx, client, repo, pr, unsupported)
	if len(targets) == 0 {
		// With nothing planned there is no result for a notice to ride on,
		// so it is posted on its own — the rule HandleIssueComment applies
		// to an orphaned notice.
		if len(notices) > 0 {
			if _, err := client.PostComment(ctx, repo.Owner, repo.Name, pr.Number, strings.Join(notices, "\n")); err != nil {
				slog.ErrorContext(ctx, "posting unsupported-tool notice", "owner", repo.Owner, "repo", repo.Name, "pr_number", pr.Number, "error", err)
			}
		}
		return
	}
	results := o.executeTargets(ctx, client, repo, pr, installationID, targets)
	o.postResults(ctx, client, repo, pr.Number, results, notices...)
}

// recordUnsupported records each affected Project whose tool this Server
// has no Plugin for, and returns a notice for each to lead the comment.
// It is a configuration problem — turnip.yaml asks this Server for
// something it cannot do — so the aggregate check fails for it rather
// than waiting on a plan that will never run (aggregate-check-run
// Requirement 7.3).
func (o *Orchestrator) recordUnsupported(ctx context.Context, client github.GitHubClient, repo github.Repository, pr github.PullRequest, unsupported []config.Project) []string {
	ref := prRef{Owner: repo.Owner, Repo: repo.Name, PRNumber: pr.Number, HeadSHA: pr.HeadSHA}
	var notices []string
	for _, project := range unsupported {
		entry := ProjectEntry{Outcome: OutcomeUnsupported, Tool: project.Tool}
		if err := o.recordOutcome(ctx, client, ref, project.Name, entry); err != nil {
			slog.ErrorContext(ctx, "recording unsupported project for the aggregate check", "owner", repo.Owner, "repo", repo.Name, "pr_number", pr.Number, "project", project.Name, "error", err)
		}
		notices = append(notices, fmt.Sprintf(
			"Project `%s` uses `%s`, which this turnip server cannot run, so it was not planned. The `%s` check fails until turnip.yaml changes.",
			project.Name, project.Tool, aggregateCheckName))
	}
	return notices
}

// markAutomaticVerdict records a whole-commit fact the automatic plan found
// — an invalid configuration, or nothing affected — and publishes it.
// There is no comment for a failure here to join, so it is logged; the
// next trigger or push on the pull request publishes again.
func (o *Orchestrator) markAutomaticVerdict(ctx context.Context, client github.GitHubClient, ref prRef, mark func(context.Context, prRef) error) {
	err := mark(ctx, ref)
	if err == nil {
		err = o.publishAggregate(ctx, client, ref)
	}
	if err != nil {
		slog.ErrorContext(ctx, "publishing the aggregate check", "owner", ref.Owner, "repo", ref.Repo, "pr_number", ref.PRNumber, "error", err)
	}
}

func prRefFor(event *github.WebhookEvent) prRef {
	return prRef{
		Owner:    event.Repository.Owner,
		Repo:     event.Repository.Name,
		PRNumber: event.PullRequest.Number,
		HeadSHA:  event.PullRequest.HeadSHA,
	}
}

func (o *Orchestrator) handlePRClosed(ctx context.Context, client github.GitHubClient, event *github.WebhookEvent) error {
	owner, repoName, prNumber := event.Repository.Owner, event.Repository.Name, event.PullRequest.Number

	cfg, err := fetchConfig(ctx, client, owner, repoName, event.PullRequest.HeadSHA)
	if err != nil {
		// No other source of Project identity exists in this stateless
		// design — log and skip Lock release, but the PlanCommentRecord
		// deletion below is independent of knowing any Project.
		slog.ErrorContext(ctx, "fetching config at PR close", "owner", owner, "repo", repoName, "pr_number", prNumber, "error", err)
		cfg = nil
	}

	var unlocked []string
	if cfg != nil {
		for _, project := range cfg.Projects {
			key := projectKey(owner, repoName, project.Name)
			// Through the transition table, for the same reason as the
			// manual unlock: a closing pull request releases from any
			// state, and the day that stops being true there has to be a
			// row to change rather than a caller to remember.
			tr, err := o.locks.Apply(ctx, key, prNumber, lock.EventPullRequestClosed, nil)
			if err != nil {
				// A Lock held by a different pull request is the ordinary
				// case, not a problem worth logging on every close.
				if !errors.Is(err, lock.ErrLockedByOtherPR) {
					slog.ErrorContext(ctx, "releasing lock on PR close", "lock_key", key, "error", err)
				}
				continue
			}
			if tr.Released {
				slog.InfoContext(ctx, "lock released on PR close", "lock_key", key, "pr_number", prNumber)
				unlocked = append(unlocked, project.Name)
			}
		}
	}

	if len(unlocked) > 0 {
		body := "Unlocked the following Projects since this PR closed:\n"
		for _, name := range unlocked {
			body += fmt.Sprintf("- %s\n", name)
		}
		if _, err := client.PostComment(ctx, owner, repoName, prNumber, body); err != nil {
			slog.ErrorContext(ctx, "posting unlock comment", "owner", owner, "repo", repoName, "pr_number", prNumber, "error", err)
		}
	}

	if err := o.records.DeletePlanCommentRecord(ctx, owner, repoName, prNumber); err != nil {
		slog.ErrorContext(ctx, "deleting plan comment record", "owner", owner, "repo", repoName, "pr_number", prNumber, "error", err)
	}

	return nil
}
