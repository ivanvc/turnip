package orchestrator

import (
	"context"
	"log/slog"

	"github.com/ivanvc/turnip/internal/github"
)

// postResults partitions results by kind and posts them per Requirement
// 10: the plan-kind group is minimized-then-reposted (Decision 4, gated
// by o.minimizeOutdatedPlanComments), the apply-kind group is always
// posted fresh and never tracked or minimized.
// notices are command-level caveats rendered above the verdict. They
// describe the trigger's selection rather than any one Project, so they
// attach to the plan-kind comment when there is one and the apply-kind
// comment otherwise — never both, since one event's selection should not
// be reported twice.
func (o *Orchestrator) postResults(ctx context.Context, client github.GitHubClient, repo github.Repository, prNumber int, results []github.ProjectResult, notices ...string) {
	planResults, applyResults := o.partitionByKind(results)

	if len(planResults) > 0 {
		o.postPlanResults(ctx, client, repo, prNumber, planResults, notices...)
		notices = nil
	}
	if len(applyResults) > 0 {
		o.postApplyResults(ctx, client, repo, prNumber, applyResults, notices...)
	}
}

// partitionByKind splits results into plan-kind (Operation equals that
// result's own Tool's GetPlanOperation()) and apply-kind (every other
// operation) per Requirement 10.2. A result whose Tool can't be resolved
// (shouldn't normally happen — every Target came from a candidate whose
// tool was already validated) is treated as apply-kind, the more
// conservative bucket (never auto-collapsed).
func (o *Orchestrator) partitionByKind(results []github.ProjectResult) (plan, apply []github.ProjectResult) {
	for _, r := range results {
		if o.isPlanResult(r) {
			plan = append(plan, r)
		} else {
			apply = append(apply, r)
		}
	}
	return plan, apply
}

func (o *Orchestrator) isPlanResult(r github.ProjectResult) bool {
	p, ok := o.plugins[r.Tool]
	return ok && p.GetPlanOperation() == r.Operation
}

func (o *Orchestrator) postPlanResults(ctx context.Context, client github.GitHubClient, repo github.Repository, prNumber int, results []github.ProjectResult, notices ...string) {
	if o.minimizeOutdatedPlanComments {
		if rec, err := o.records.GetPlanCommentRecord(ctx, repo.Owner, repo.Name, prNumber); err != nil {
			slog.ErrorContext(ctx, "reading plan comment record", "owner", repo.Owner, "repo", repo.Name, "pr_number", prNumber, "error", err)
		} else if rec != nil {
			for _, nodeID := range rec.NodeIDs {
				if err := client.MinimizeComment(ctx, nodeID); err != nil {
					// Soft failure (Decision 3): a comment that fails to
					// collapse is a readability regression, not a
					// correctness one — posting the new comment proceeds.
					slog.ErrorContext(ctx, "minimizing comment", "node_id", nodeID, "error", err)
				}
			}
		}
		// A missing record (never existed, or its safety-net TTL expired)
		// is not an error — the minimize step is simply skipped, per
		// Requirement 10.6. No fallback scan is performed.
	}

	bodies := github.BuildConsolidatedComment(results, notices...)
	nodeIDs := postBodies(ctx, client, repo, prNumber, bodies)

	if o.minimizeOutdatedPlanComments {
		if err := o.records.SetPlanCommentRecord(ctx, repo.Owner, repo.Name, prNumber, nodeIDs); err != nil {
			slog.ErrorContext(ctx, "writing plan comment record", "owner", repo.Owner, "repo", repo.Name, "pr_number", prNumber, "error", err)
		}
	}
}

// postApplyResults always posts fresh — never minimized, never tracked,
// never updated in place (Requirement 10.7): an apply is a permanent
// record of what happened to real infrastructure, not a superseded
// prediction the way an older plan is.
func (o *Orchestrator) postApplyResults(ctx context.Context, client github.GitHubClient, repo github.Repository, prNumber int, results []github.ProjectResult, notices ...string) {
	bodies := github.BuildConsolidatedComment(results, notices...)
	postBodies(ctx, client, repo, prNumber, bodies)
}

// postBodies posts every body as a fresh comment, returning the posted
// comments' GraphQL node IDs (only meaningful to the plan-kind caller).
func postBodies(ctx context.Context, client github.GitHubClient, repo github.Repository, prNumber int, bodies []string) []string {
	var nodeIDs []string
	for _, body := range bodies {
		posted, err := client.PostComment(ctx, repo.Owner, repo.Name, prNumber, body)
		if err != nil {
			slog.ErrorContext(ctx, "posting comment", "owner", repo.Owner, "repo", repo.Name, "pr_number", prNumber, "error", err)
			continue
		}
		nodeIDs = append(nodeIDs, posted.NodeID)
	}
	return nodeIDs
}
