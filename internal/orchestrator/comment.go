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

// malformedTriggerReply renders the malformed lines ParseTriggers
// couldn't parse (Requirement 3.3) — well-formed commands in the same
// comment are still processed; this is purely informational.
func malformedTriggerReply(malformed github.MalformedTriggerErrors) string {
	var b strings.Builder
	b.WriteString("The following line(s) looked like a trigger command but couldn't be parsed:\n\n")
	for _, m := range malformed {
		fmt.Fprintf(&b, "- line %d: `%s`\n", m.Line, m.Content)
	}
	return b.String()
}

var _ github.EventHandler = (*Orchestrator)(nil)

// HandleIssueComment reacts to issue_comment webhooks: parses Trigger
// Commands (Requirement 3), authorizes the author, resolves and executes
// every command's Targets in order (Requirement 4/5), and posts the
// consolidated result.
func (o *Orchestrator) HandleIssueComment(ctx context.Context, event *github.WebhookEvent) error {
	if event.Action != "created" {
		return nil
	}

	client := o.installationClient(event.Installation.ID)
	owner, repoName := event.Repository.Owner, event.Repository.Name

	commands, err := github.ParseTriggers(event.Comment.Body)
	if errors.Is(err, github.ErrNoTrigger) {
		return nil
	}
	var malformed github.MalformedTriggerErrors
	hasMalformed := errors.As(err, &malformed)

	authorizer := github.NewAuthorizer(client)
	isCollaborator, err := authorizer.IsCollaborator(ctx, owner, repoName, event.Comment.Author)
	if err != nil || !isCollaborator {
		_, postErr := client.PostComment(ctx, owner, repoName, event.PullRequest.Number,
			fmt.Sprintf("@%s does not have permission to trigger operations on this repository.", event.Comment.Author))
		return postErr
	}

	pr, err := client.GetPullRequest(ctx, owner, repoName, event.PullRequest.Number)
	if err != nil {
		return fmt.Errorf("orchestrator: getting pull request %s/%s#%d: %w", owner, repoName, event.PullRequest.Number, err)
	}

	// The first point at which the head repository is known: an
	// issue_comment payload carries a pull request number and nothing
	// else, so GetPullRequest above is the only source.
	//
	// The collaborator check this follows authorizes the *trigger*, not
	// the *code*. A trusted colleague commenting on a fork's pull request
	// would otherwise run a stranger's code, which is why permission
	// level does not enter into this decision.
	//
	// Before fetchConfig, for the same reason as the automatic path: on a
	// foreign pull request that file is chosen by whoever opened it.
	if pr.IsForeign(event.Repository) {
		slog.WarnContext(ctx, "refusing operation on a pull request from another repository",
			"owner", owner,
			"repo", repoName,
			"pr_number", pr.Number,
			"head_owner", pr.HeadRepo.Owner,
			"head_repo", pr.HeadRepo.Name,
			"actor", event.Comment.Author,
		)
		return github.ErrRefused
	}

	cfg, err := fetchConfig(ctx, client, owner, repoName, pr.HeadSHA)
	if err != nil {
		_, postErr := client.PostComment(ctx, owner, repoName, event.PullRequest.Number, configErrorComment(err))
		return postErr
	}

	repo := event.Repository
	// Assembled once, then asked about each command in turn — see
	// selection's own comment for why this is a value rather than a
	// parameter list.
	sel := &selection{
		cfg:         cfg,
		plugins:     o.plugins,
		owner:       owner,
		repo:        repoName,
		author:      event.Comment.Author,
		prNumber:    pr.Number,
		authorizer:  authorizer,
		locks:       o.locks,
		modifiedSet: memoModifiedFiles(client, owner, repoName, pr.Number),
	}
	// targetGroups preserves per-TriggerCommand grouping: each command's
	// Targets must finish executing (Requirement 6's Lock semantics)
	// before the next command's begin, so a "diff" followed by "apply" on
	// the same Project in one comment behaves predictably rather than
	// racing (see design.md's Edge Cases). Concurrency still applies
	// *within* one command's own Targets (Requirement 17.1) — only the
	// commands themselves are sequenced.
	var targetGroups [][]Target
	var preResults []github.ProjectResult
	var replies []string
	// selectionNotices are command-level caveats — a pattern that matched
	// nothing — which ride at the top of the consolidated comment rather
	// than generating a notification of their own.
	var selectionNotices []string

	if hasMalformed {
		replies = append(replies, malformedTriggerReply(malformed))
	}

	for _, cmd := range commands {
		if cmd.Operation == "unlock" {
			reply := o.handleUnlock(ctx, cfg, cmd, repo, pr.Number, event.Comment.Author, authorizer)
			replies = append(replies, reply)
			continue
		}

		targets, rejected, notices, err := sel.resolve(ctx, cmd)
		// Notices are collected before the error check: an empty selection
		// still has something worth saying, and a truncated file listing is
		// most relevant precisely when nothing matched.
		selectionNotices = append(selectionNotices, notices...)
		if err != nil {
			var unmatched *UnmatchedProjectError
			if errors.As(err, &unmatched) {
				replies = append(replies, fmt.Sprintf("Project %q not found in turnip.yaml.", unmatched.Name))
				continue
			}
			var mixed *MixedSelectorError
			if errors.As(err, &mixed) {
				replies = append(replies, fmt.Sprintf(
					"`*` cannot be combined with other selectors (got %s) — a trigger either names projects or asks for all of them.",
					strings.Join(mixed.Others, ", ")))
				continue
			}
			var noModified *NoModifiedProjectsError
			if errors.As(err, &noModified) {
				replies = append(replies, fmt.Sprintf(
					"No project matched the files this pull request changes. Run `/%s %s *` to target every project.",
					noModified.Tool, noModified.Operation))
				continue
			}
			var noPlanned *NoPlannedProjectsError
			if errors.As(err, &noPlanned) {
				replies = append(replies, fmt.Sprintf(
					"No project has a plan from this pull request to apply. Plan first, or name a project explicitly with `/%s %s <project>`.",
					noPlanned.Tool, noPlanned.Operation))
				continue
			}
			return err
		}
		if len(targets) > 0 {
			targetGroups = append(targetGroups, targets)
		}
		preResults = append(preResults, rejected...)
	}

	// A notice has nowhere to ride when nothing ran, so it becomes a reply
	// of its own — the same reasoning that makes UnmatchedProjectError
	// standalone: there is no result row to attach it to.
	if len(targetGroups) == 0 && len(preResults) == 0 && len(selectionNotices) > 0 {
		replies = append(replies, strings.Join(selectionNotices, "\n"))
		selectionNotices = nil
	}

	for _, reply := range replies {
		if _, err := client.PostComment(ctx, owner, repoName, event.PullRequest.Number, reply); err != nil {
			slog.ErrorContext(ctx, "posting reply comment", "owner", owner, "repo", repoName, "pr_number", event.PullRequest.Number, "error", err)
		}
	}

	if len(targetGroups) == 0 && len(preResults) == 0 {
		return nil
	}

	prInfo := github.PullRequest{Number: pr.Number, HeadSHA: pr.HeadSHA, BaseRef: pr.BaseRef, HeadRef: pr.HeadRef}
	go func(groups [][]Target, results []github.ProjectResult, notices []string) {
		detached := context.WithoutCancel(ctx)
		for _, targets := range groups {
			results = append(results, o.executeTargets(detached, client, repo, prInfo, event.Installation.ID, targets)...)
		}
		o.postResults(detached, client, repo, prInfo.Number, results, notices...)
	}(targetGroups, preResults, selectionNotices)

	return nil
}

// memoModifiedFiles returns a lookup that fetches the pull request's
// changed files at most once per event, caching the error alongside the
// result so a failing fetch is not retried once per command.
//
// Deliberately mutex-free: HandleIssueComment resolves commands one after
// another, which is what makes a plain closure safe here. Running commands
// concurrently would turn this into a race — noted at the seam rather than
// left to be discovered.
func memoModifiedFiles(client github.GitHubClient, owner, repo string, prNumber int) func(context.Context) ([]string, error) {
	var (
		files  []string
		err    error
		called bool
	)
	return func(ctx context.Context) ([]string, error) {
		if !called {
			called = true
			files, err = client.GetModifiedFiles(ctx, owner, repo, prNumber)
		}
		return files, err
	}
}

// handleUnlock implements Requirement 5: resolve candidates (5.1),
// require write permission (5.2), release each candidate's Lock (5.3),
// and build a single reply naming what was unlocked or who actually
// holds it (5.4). No Runner Job, check run, or Operation Record is ever
// created (5.5).
func (o *Orchestrator) handleUnlock(ctx context.Context, cfg *config.Config, cmd *github.TriggerCommand, repo github.Repository, prNumber int, author string, authorizer *github.Authorizer) string {
	candidates, err := resolveUnlockCandidates(cfg, cmd)
	if err != nil {
		var unmatched *UnmatchedProjectError
		if errors.As(err, &unmatched) {
			return fmt.Sprintf("Project %q not found in turnip.yaml.", unmatched.Name)
		}
		return fmt.Sprintf("Resolving unlock targets failed: %v", err)
	}

	hasWrite, err := authorizer.HasWritePermission(ctx, repo.Owner, repo.Name, author)
	if err != nil || !hasWrite {
		return fmt.Sprintf("@%s does not have write permission required to unlock.", author)
	}

	var unlocked, heldByOther []string
	for _, project := range candidates {
		key := projectKey(repo.Owner, repo.Name, project.Name)
		err := o.locks.ReleaseLock(ctx, key, prNumber)
		switch {
		case err == nil:
			unlocked = append(unlocked, project.Name)
		case errors.Is(err, lock.ErrLockedByOtherPR):
			status, statusErr := o.locks.GetLockStatus(ctx, key)
			if statusErr == nil && status.Locked {
				heldByOther = append(heldByOther, fmt.Sprintf("%s (held by #%d)", project.Name, status.PRNumber))
			} else {
				heldByOther = append(heldByOther, project.Name)
			}
		default:
			slog.ErrorContext(ctx, "releasing lock via unlock command", "lock_key", key, "error", err)
		}
	}

	var body string
	if len(unlocked) > 0 {
		body += "Unlocked:\n"
		for _, name := range unlocked {
			body += fmt.Sprintf("- %s\n", name)
		}
	}
	if len(heldByOther) > 0 {
		body += "\nNot unlocked (held by a different PR):\n"
		for _, name := range heldByOther {
			body += fmt.Sprintf("- %s\n", name)
		}
	}
	if body == "" {
		body = "No Projects matched this unlock command."
	}
	return body
}
