package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
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
// Commands (Requirement 3), authorizes the author, resolves every
// command's Targets (Requirement 4/5), and starts the trigger that runs
// them in order and posts the consolidated result (carry.go).
func (o *Orchestrator) HandleIssueComment(ctx context.Context, event *github.WebhookEvent) error {
	if event.Action != "created" {
		return nil
	}

	client := o.installationClient(event.Installation.ID)
	owner, repoName := event.Repository.Owner, event.Repository.Name

	commands, err := github.ParseTriggers(event.Comment.Body, o.plugins.Names())
	if errors.Is(err, github.ErrNoTrigger) {
		return nil
	}
	var malformed github.MalformedTriggerErrors
	hasMalformed := errors.As(err, &malformed)

	authorizer := github.NewAuthorizer(client)
	isCollaborator, err := authorizer.IsCollaborator(ctx, owner, repoName, event.Comment.Author, event.Repository.Private)
	if err != nil {
		// Answered below as "could not check", which is what the author
		// sees; the actual failure is only ever visible here.
		slog.ErrorContext(ctx, "checking collaborator permission",
			"owner", owner, "repo", repoName, "pr_number", event.PullRequest.Number,
			"actor", event.Comment.Author, "error", err)
	} else if !isCollaborator {
		slog.InfoContext(ctx, "refusing comment trigger from a non-collaborator",
			"owner", owner, "repo", repoName, "pr_number", event.PullRequest.Number,
			"actor", event.Comment.Author)
	}
	if err != nil || !isCollaborator {
		reply := fmt.Sprintf("@%s does not have permission to trigger operations on this repository.", event.Comment.Author)
		if err != nil {
			reply = fmt.Sprintf("@%s: turnip could not check your permission on this repository, so nothing was run. Comment again to retry.", event.Comment.Author)
		}
		_, postErr := client.PostComment(ctx, owner, repoName, event.PullRequest.Number, reply)
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

	// A pull request that is no longer open is finished or abandoned, and
	// the cleanup that follows closing has already run. Acting now takes a
	// Lock with no remaining lifecycle event to release it — close has
	// already fired and there is no second one — and a diff followed by an
	// apply on one closed *without* merging deploys precisely the changes
	// someone declined to merge.
	//
	// After the fork refusal above, deliberately. A closed fork pull
	// request stays silent: replying here would hand back the signal that
	// refusal exists to withhold.
	//
	// Before fetchConfig, which is what puts it ahead of every Lock, Job
	// and check run at once — all of them are downstream of it.
	if !pr.Open {
		// INFO rather than WARN. Slice 15's WARN channel is for someone
		// trying to have turnip run their code; this is a colleague
		// commenting on the wrong tab, and mixing the two is how a WARN
		// channel stops being read.
		slog.InfoContext(ctx, "refusing operation on a closed pull request",
			"owner", owner,
			"repo", repoName,
			"pr_number", pr.Number,
			"actor", event.Comment.Author,
		)
		if _, postErr := client.PostComment(ctx, owner, repoName, event.PullRequest.Number, closedPullRequestComment()); postErr != nil {
			// Logged, not returned. Returning it answers 500 and has
			// GitHub redeliver a comment turnip has already decided about
			// — which would post this same reply again.
			slog.ErrorContext(ctx, "posting closed pull request refusal",
				"owner", owner, "repo", repoName, "pr_number", pr.Number, "error", postErr)
		}
		return github.ErrRefused
	}

	// unlock never reads the configuration, so it is fetched only when the
	// comment holds some other command, and then once. When it cannot be
	// used, the loop below still runs: each unlock is handled, every other
	// command is skipped, and configReply stands in for the first skipped
	// command's replies, so replies keep the comment's command order.
	needsConfig := false
	for _, cmd := range commands {
		if cmd.Operation != "unlock" {
			needsConfig = true
			break
		}
	}
	var configReply string
	var sel *selection
	if needsConfig {
		cfg, err := fetchConfig(ctx, client, owner, repoName, pr.HeadSHA, o.catalog)
		if err != nil {
			configReply = configErrorComment(err)
		} else {
			sel = o.commentSelection(client, authorizer, event, pr, cfg)
		}
	}

	repo := event.Repository
	// targetGroups preserves per-TriggerCommand grouping: each command's
	// Targets must finish executing (Requirement 6's Lock semantics)
	// before the next command's begin, so a "diff" followed by "apply" on
	// the same Project in one comment behaves predictably rather than
	// racing (see design.md's Edge Cases). Concurrency still applies
	// *within* one command's own Targets (Requirement 17.1); only the
	// commands themselves are sequenced. Each group is one command of the
	// trigger, begun by whichever replica finishes the one before it.
	var targetGroups [][]Target
	var preResults []github.ProjectResult
	var replies []string
	// selectionNotices are command-level caveats (a pattern that matched
	// nothing) which ride at the top of the consolidated comment rather
	// than generating a notification of their own.
	var selectionNotices []string

	if hasMalformed {
		replies = append(replies, malformedTriggerReply(malformed))
	}

	for _, cmd := range commands {
		if cmd.Operation == "unlock" {
			reply := o.handleUnlock(ctx, cmd, repo, pr.Number, event.Comment.Author, authorizer)
			replies = append(replies, reply)
			continue
		}

		if sel == nil {
			// The configuration is not usable: this command is skipped,
			// and the first one skipped carries the configuration reply.
			if configReply != "" {
				replies = append(replies, configReply)
				configReply = ""
			}
			continue
		}

		targets, rejected, notices, refusal, err := sel.resolve(ctx, cmd)
		// Notices are collected before the error check: an empty selection
		// still has something worth saying, and a truncated file listing is
		// most relevant precisely when nothing matched.
		selectionNotices = append(selectionNotices, notices...)
		if refusal != "" {
			replies = append(replies, refusal)
		}
		if err != nil {
			var unmatched *UnmatchedProjectError
			if errors.As(err, &unmatched) {
				replies = append(replies, fmt.Sprintf("Project %q not found in turnip.yaml.", unmatched.Name))
				continue
			}
			var mixed *MixedSelectorError
			if errors.As(err, &mixed) {
				replies = append(replies, fmt.Sprintf(
					"`*` cannot be combined with other selectors (got %s): a trigger either names projects or asks for all of them.",
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
	// of its own, the same reasoning that makes UnmatchedProjectError
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

	return o.startTrigger(ctx, client, triggerMeta{
		Repo:           repo,
		InstallationID: event.Installation.ID,
		PRNumber:       pr.Number,
		HeadSHA:        pr.HeadSHA,
		BaseRef:        pr.BaseRef,
		HeadRef:        pr.HeadRef,
		Notices:        selectionNotices,
		Commands:       targetGroups,
	}, preResults)
}

// commentSelection assembles the selection a comment's commands are
// resolved against, once per comment. See selection's own comment for why
// this is a value rather than a parameter list.
func (o *Orchestrator) commentSelection(client github.GitHubClient, authorizer *github.Authorizer, event *github.WebhookEvent, pr *github.PullRequest, cfg *config.Config) *selection {
	owner, repoName := event.Repository.Owner, event.Repository.Name
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
	if len(o.mutationRequirements) > 0 {
		sel.unmetRequirements = memoMutationRequirements(&mutationRequirementCheck{
			client:     client,
			authorizer: authorizer,
			owner:      owner,
			repo:       repoName,
			pr:         pr,
			sleep:      o.requirementSleep,
		}, o.mutationRequirements)
	}
	return sel
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

// memoMutationRequirements returns a lookup that evaluates the
// Requirement_Set at most once per event. The answer cannot differ
// between the Projects of a command, or between the commands of one
// comment, so a second apply command reuses the first's (Requirement
// 6.4, met more strictly than asked).
//
// Mutex-free for the same reason as memoModifiedFiles: commands are
// resolved one after another.
func memoMutationRequirements(check *mutationRequirementCheck, requirements []string) func(context.Context) []unmetRequirement {
	var (
		unmet  []unmetRequirement
		called bool
	)
	return func(ctx context.Context) []unmetRequirement {
		if !called {
			called = true
			unmet = check.evaluate(ctx, requirements)
		}
		return unmet
	}
}

// handleUnlock releases this pull request's locks without reading
// turnip.yaml: the write check comes first, so a refused author causes no
// lock reads; then the locks are found among the repository's own (a
// named unlock reads each named lock, a bare one lists the repository's
// and keeps the pull request's), and each release goes through the
// transition table. The trigger word does not narrow it. No Runner Job,
// check run, or Operation Record is ever created.
func (o *Orchestrator) handleUnlock(ctx context.Context, cmd *github.TriggerCommand, repo github.Repository, prNumber int, author string, authorizer *github.Authorizer) string {
	hasWrite, err := authorizer.HasWritePermission(ctx, repo.Owner, repo.Name, author)
	if err != nil {
		slog.ErrorContext(ctx, "checking write permission for unlock",
			"owner", repo.Owner, "repo", repo.Name, "pr_number", prNumber, "actor", author, "error", err)
		return fmt.Sprintf("@%s: turnip could not check your write permission, so nothing was unlocked. Comment again to retry.", author)
	}
	if !hasWrite {
		slog.InfoContext(ctx, "refusing unlock from an author without write permission",
			"owner", repo.Owner, "repo", repo.Name, "pr_number", prNumber, "actor", author)
		return fmt.Sprintf("@%s does not have write permission required to unlock.", author)
	}

	const couldNotRead = "turnip could not read this pull request's locks, so nothing was unlocked. Comment again to retry."

	// held is the lock of each selected project, in the order to report.
	type heldLock struct {
		name   string
		status lock.LockStatus
	}
	var found []heldLock
	if len(cmd.Projects) == 0 {
		prefix := projectKey(repo.Owner, repo.Name, "")
		listed, err := o.locks.ListLocks(ctx, prefix)
		if err != nil {
			slog.ErrorContext(ctx, "listing locks for unlock", "owner", repo.Owner, "repo", repo.Name, "pr_number", prNumber, "error", err)
			return couldNotRead
		}
		for _, l := range listed {
			if l.Status.Locked && l.Status.PRNumber == prNumber {
				found = append(found, heldLock{name: strings.TrimPrefix(l.ProjectKey, prefix), status: l.Status})
			}
		}
		sort.Slice(found, func(i, j int) bool { return found[i].name < found[j].name })
	} else {
		// Every read happens before any release, so a failed read
		// releases nothing.
		seen := make(map[string]bool, len(cmd.Projects))
		for _, name := range cmd.Projects {
			if seen[name] {
				continue
			}
			seen[name] = true
			status, err := o.locks.GetLockStatus(ctx, projectKey(repo.Owner, repo.Name, name))
			if err != nil {
				slog.ErrorContext(ctx, "reading lock for unlock", "owner", repo.Owner, "repo", repo.Name, "project", name, "error", err)
				return couldNotRead
			}
			if status != nil && status.Locked {
				found = append(found, heldLock{name: name, status: *status})
			}
		}
	}

	var unlocked, heldByOther []string
	for _, h := range found {
		key := projectKey(repo.Owner, repo.Name, h.name)
		if h.status.PRNumber != prNumber {
			heldByOther = append(heldByOther, fmt.Sprintf("%s (held by #%d)", h.name, h.status.PRNumber))
			continue
		}
		// Through the transition table like every other lifecycle change.
		// It also guards the race in which the lock changed hands since
		// the read.
		tr, err := o.locks.Apply(ctx, key, prNumber, lock.EventUnlocked, nil)
		switch {
		case err == nil && tr.Released:
			slog.InfoContext(ctx, "lock released via unlock command", "lock_key", key, "pr_number", prNumber, "actor", author)
			unlocked = append(unlocked, h.name)
		case err == nil:
			// Released meanwhile: nothing to report.
		case errors.Is(err, lock.ErrLockedByOtherPR):
			status, statusErr := o.locks.GetLockStatus(ctx, key)
			if statusErr == nil && status != nil && status.Locked {
				heldByOther = append(heldByOther, fmt.Sprintf("%s (held by #%d)", h.name, status.PRNumber))
			} else {
				heldByOther = append(heldByOther, h.name)
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
		body = "No projects matched this unlock command."
	}
	return body
}
