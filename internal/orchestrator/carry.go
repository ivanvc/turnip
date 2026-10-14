package orchestrator

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/ivanvc/turnip/internal/github"
)

// dispatchFunc starts one Target's Operation as part of a trigger's
// command. It returns nil once the Operation's Job exists, in which case
// the finalizer completes the Project later, and the Project's refusal
// when no Job could be started, in which case the caller completes it.
// It is a seam so carrying a trigger forward can be tested without Jobs.
type dispatchFunc func(ctx context.Context, client github.GitHubClient, repo github.Repository, pr github.PullRequest, installationID int64, triggerID string, command int, t Target) *github.ProjectResult

// triggerAttempts is how many times a trigger step is tried when Redis
// fails it, and defaultTriggerRetryBackoff the wait before the second try,
// doubled before each one after: under two seconds in all. A step is tried
// again because nothing else will: the Operation is already claimed and
// its record deleted by the time its Project is completed in its trigger,
// so a step given up on leaves the trigger without a comment until it
// expires.
const (
	triggerAttempts            = 4
	defaultTriggerRetryBackoff = 250 * time.Millisecond
)

// retryTrigger runs one step of a trigger against Redis, again after a
// failure, up to triggerAttempts times. It stops early when ctx ends and
// returns the last error.
func (o *Orchestrator) retryTrigger(ctx context.Context, step, triggerID string, fn func() error) error {
	wait := o.triggerRetryBackoff
	for attempt := 1; ; attempt++ {
		err := fn()
		if err == nil || attempt == triggerAttempts {
			return err
		}
		slog.WarnContext(ctx, "trigger step failed, trying again", "step", step, "trigger", triggerID, "attempt", attempt, "error", err)
		select {
		case <-ctx.Done():
			return err
		case <-time.After(wait):
		}
		wait *= 2
	}
}

// startTrigger starts a trigger whose commands are already selected and
// authorized. refused are the Projects refused during selection, which the
// results comment reports with the rest.
//
// A trigger with nothing to dispatch posts its comment at once and leaves
// no record. Any other is recorded before its first Operation is
// dispatched, and its first command begins in the background, on a context
// the webhook's response does not cancel: dispatch takes longer than a
// webhook delivery should wait. From there the trigger belongs to no
// replica in particular (completeProject).
//
// A trigger that cannot be recorded dispatches nothing, and says so on the
// pull request: the Projects refused during selection and the notices are
// already known, and would otherwise reach only the Server's log.
func (o *Orchestrator) startTrigger(ctx context.Context, client github.GitHubClient, meta triggerMeta, refused []github.ProjectResult) error {
	if len(meta.Commands) == 0 {
		o.postResults(ctx, client, meta.Repo, meta.PRNumber, refused, meta.Notices...)
		return nil
	}
	triggerID := uuid.NewString()
	err := o.retryTrigger(ctx, "creating the trigger", triggerID, func() error {
		return o.triggers.Create(ctx, triggerID, meta, refused)
	})
	if err != nil {
		results := append([]github.ProjectResult(nil), refused...)
		for _, targets := range meta.Commands {
			for _, t := range targets {
				results = append(results, rejectedResult(t, fmt.Sprintf("recording the trigger: %v. Nothing was dispatched; comment again to retry.", err)))
			}
		}
		o.postResults(ctx, client, meta.Repo, meta.PRNumber, results, meta.Notices...)
		return err
	}
	detached := context.WithoutCancel(ctx)
	go func() {
		if err := o.beginCommand(detached, triggerID, meta, 0); err != nil {
			slog.ErrorContext(detached, "beginning a trigger's first command",
				"owner", meta.Repo.Owner, "repo", meta.Repo.Name, "pr_number", meta.PRNumber, "trigger", triggerID, "error", err)
		}
	}()
	return nil
}

// beginCommand begins command of the trigger: it sets every pending field
// first, then dispatches the command's Projects concurrently and completes
// each one that was refused. A command with nothing dispatched is
// therefore finished here, by the replica dispatching it, and the trigger
// moves on to the next command.
func (o *Orchestrator) beginCommand(ctx context.Context, triggerID string, meta triggerMeta, command int) error {
	if command >= len(meta.Commands) {
		return fmt.Errorf("orchestrator: trigger %q has no command %d", triggerID, command)
	}
	targets := meta.Commands[command]
	if len(targets) == 0 {
		// Nothing would ever complete this command, so skip it.
		return o.advance(ctx, triggerID, meta, command+1)
	}
	names := make([]string, len(targets))
	for i, t := range targets {
		names[i] = t.Project.Name
	}
	// Trying again is safe: nothing of the command is dispatched until its
	// pending fields are set, so setting them twice changes nothing.
	err := o.retryTrigger(ctx, "beginning a command", triggerID, func() error {
		return o.triggers.BeginCommand(ctx, triggerID, command, names)
	})
	if err != nil {
		return err
	}

	client := o.installationClient(meta.InstallationID)
	pr := github.PullRequest{Number: meta.PRNumber, HeadSHA: meta.HeadSHA, BaseRef: meta.BaseRef, HeadRef: meta.HeadRef}

	dispatch := o.dispatch
	if dispatch == nil {
		dispatch = o.executeOne
	}

	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		errs []error
	)
	for _, t := range targets {
		wg.Add(1)
		go func(t Target) {
			defer wg.Done()
			refusal := dispatch(ctx, client, meta.Repo, pr, meta.InstallationID, triggerID, command, t)
			if refusal == nil {
				return
			}
			if err := o.completeProject(ctx, triggerID, command, t.Project.Name, *refusal); err != nil {
				mu.Lock()
				errs = append(errs, err)
				mu.Unlock()
			}
		}(t)
	}
	wg.Wait()
	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

// completeProject adds a Project's result to its trigger. The one caller
// that completes the last Project of a command carries the trigger
// forward before returning. An empty trigger id, a trigger Redis no longer
// has and a Project that already completed are logged and nothing more.
//
// Each step against Redis is tried again when it fails (retryTrigger).
// Every try of the completion carries one token, so a try after a
// completion that Redis ran but whose answer was lost is given that
// answer, and the caller that finished the command still carries it
// forward.
func (o *Orchestrator) completeProject(ctx context.Context, triggerID string, command int, project string, result github.ProjectResult) error {
	if triggerID == "" {
		slog.InfoContext(ctx, "operation names no trigger, nothing to add its result to", "project", project)
		return nil
	}
	token := uuid.NewString()
	var pending int
	err := o.retryTrigger(ctx, "completing a Project", triggerID, func() (err error) {
		pending, err = o.triggers.Complete(ctx, triggerID, command, project, token, result)
		return err
	})
	if err != nil {
		return err
	}
	if pending < 0 {
		slog.InfoContext(ctx, "no trigger to add the result to, or it was already added", "trigger", triggerID, "command", command, "project", project)
		return nil
	}
	if pending > 0 {
		return nil
	}
	st, err := o.getTrigger(ctx, triggerID)
	if err != nil {
		return err
	}
	if st == nil {
		slog.InfoContext(ctx, "trigger is gone, nothing to carry forward", "trigger", triggerID)
		return nil
	}
	return o.advance(ctx, triggerID, st.Meta, command+1)
}

// advance begins the command at next, or, when none is left, posts the
// trigger's results comment and deletes the trigger. Only the caller
// returned 0 for the last command gets here, so the comment is posted
// once.
func (o *Orchestrator) advance(ctx context.Context, triggerID string, meta triggerMeta, next int) error {
	if next < len(meta.Commands) {
		return o.beginCommand(ctx, triggerID, meta, next)
	}
	st, err := o.getTrigger(ctx, triggerID)
	if err != nil {
		return err
	}
	if st == nil {
		slog.InfoContext(ctx, "trigger is gone, nothing to post", "trigger", triggerID)
		return nil
	}
	client := o.installationClient(meta.InstallationID)
	o.postResults(ctx, client, meta.Repo, meta.PRNumber, st.Results, meta.Notices...)
	return o.retryTrigger(ctx, "deleting the trigger", triggerID, func() error {
		return o.triggers.Delete(ctx, triggerID)
	})
}

// getTrigger reads a trigger back, trying again when Redis fails.
func (o *Orchestrator) getTrigger(ctx context.Context, triggerID string) (st *triggerState, err error) {
	err = o.retryTrigger(ctx, "reading the trigger", triggerID, func() (err error) {
		st, err = o.triggers.Get(ctx, triggerID)
		return err
	})
	return st, err
}
