package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

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
	case "opened", "synchronize", "ready_for_review":
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
		_, postErr := client.PostComment(ctx, event.Repository.Owner, event.Repository.Name, event.PullRequest.Number, configErrorComment(err))
		return postErr
	}

	modifiedFiles, err := client.GetModifiedFiles(ctx, event.Repository.Owner, event.Repository.Name, event.PullRequest.Number)
	if err != nil {
		return fmt.Errorf("orchestrator: getting modified files for %s/%s#%d: %w", event.Repository.Owner, event.Repository.Name, event.PullRequest.Number, err)
	}

	matched := config.MatchProjects(cfg.Projects, modifiedFiles)
	if len(matched) == 0 {
		return nil
	}

	targets := planTargetsFor(matched, o.plugins, cfg.Clone)

	// Detach from the request context: the webhook HTTP handler responds
	// as soon as this method returns, but executing Targets can take far
	// longer than GitHub's webhook delivery timeout allows for. Run it in
	// the background on a context stripped of the request's cancellation
	// (but not its values), and return immediately.
	go o.runTargetsAndPost(context.WithoutCancel(ctx), client, event.Repository, *event.PullRequest, event.Installation.ID, targets)

	return nil
}

func (o *Orchestrator) runTargetsAndPost(ctx context.Context, client github.GitHubClient, repo github.Repository, pr github.PullRequest, installationID int64, targets []Target) {
	results := o.executeTargets(ctx, client, repo, pr, installationID, targets)
	o.postResults(ctx, client, repo, pr.Number, results)
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
