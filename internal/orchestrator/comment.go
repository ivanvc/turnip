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

	cfg, err := fetchConfig(ctx, client, owner, repoName, pr.HeadSHA)
	if err != nil {
		_, postErr := client.PostComment(ctx, owner, repoName, event.PullRequest.Number, configErrorComment(err))
		return postErr
	}

	repo := event.Repository
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

	if hasMalformed {
		replies = append(replies, malformedTriggerReply(malformed))
	}

	for _, cmd := range commands {
		if cmd.Operation == "unlock" {
			reply := o.handleUnlock(ctx, cfg, cmd, repo, pr.Number, event.Comment.Author, authorizer)
			replies = append(replies, reply)
			continue
		}

		targets, rejected, err := resolveTargets(ctx, cfg, o.plugins, cmd, owner, repoName, event.Comment.Author, authorizer)
		if err != nil {
			var unmatched *UnmatchedProjectError
			if errors.As(err, &unmatched) {
				replies = append(replies, fmt.Sprintf("Project %q not found in turnip.yaml.", unmatched.Name))
				continue
			}
			return err
		}
		if len(targets) > 0 {
			targetGroups = append(targetGroups, targets)
		}
		preResults = append(preResults, rejected...)
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
	go func(groups [][]Target, results []github.ProjectResult) {
		detached := context.WithoutCancel(ctx)
		for _, targets := range groups {
			results = append(results, o.executeTargets(detached, client, repo, prInfo, event.Installation.ID, targets)...)
		}
		o.postResults(detached, client, repo, prInfo.Number, results)
	}(targetGroups, preResults)

	return nil
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
