package orchestrator

import (
	"context"
	"fmt"
	"log"

	"github.com/ivanvc/turnip/internal/config"
	"github.com/ivanvc/turnip/internal/github"
)

// HandlePullRequest reacts to pull_request webhooks: "opened"/"synchronize"
// trigger the auto-plan flow (Requirement 2), "closed" releases Locks
// (Requirement 11). Every other action is a no-op.
func (o *Orchestrator) HandlePullRequest(ctx context.Context, event *github.WebhookEvent) error {
	client := o.installationClient(event.Installation.ID)

	switch event.Action {
	case "opened", "synchronize":
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

	targets := planTargetsFor(matched, o.plugins)

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
		log.Printf("orchestrator: fetching config at PR close for %s/%s#%d: %v", owner, repoName, prNumber, err)
		cfg = nil
	}

	var unlocked []string
	if cfg != nil {
		for _, project := range cfg.Projects {
			key := projectKey(owner, repoName, project.Name)
			locked, err := o.locks.IsLockedByPR(ctx, key, prNumber)
			if err != nil || !locked {
				continue
			}
			if err := o.locks.ReleaseLock(ctx, key, prNumber); err != nil {
				log.Printf("orchestrator: releasing lock %q on PR close: %v", key, err)
				continue
			}
			unlocked = append(unlocked, project.Name)
		}
	}

	if len(unlocked) > 0 {
		body := "Unlocked the following Projects since this PR closed:\n"
		for _, name := range unlocked {
			body += fmt.Sprintf("- %s\n", name)
		}
		if _, err := client.PostComment(ctx, owner, repoName, prNumber, body); err != nil {
			log.Printf("orchestrator: posting unlock comment for %s/%s#%d: %v", owner, repoName, prNumber, err)
		}
	}

	if err := o.records.DeletePlanCommentRecord(ctx, owner, repoName, prNumber); err != nil {
		log.Printf("orchestrator: deleting plan comment record for %s/%s#%d: %v", owner, repoName, prNumber, err)
	}

	return nil
}
