package orchestrator

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/ivanvc/turnip/internal/github"
)

// mergeableRetries and mergeableRetryWait bound the wait for GitHub to
// compute mergeability: at most 3 further reads, 1 second apart, which
// keeps the gate well inside GitHub's 10-second webhook delivery.
const (
	mergeableRetries   = 3
	mergeableRetryWait = time.Second
)

// unmetReason is why an Mutation_Requirement did not hold. Each reads
// differently in the reply, because each asks something different of
// the author.
type unmetReason int

const (
	// reasonUnmet: GitHub answered, and the answer is no.
	reasonUnmet unmetReason = iota
	// reasonConflict: GitHub reports a merge conflict.
	reasonConflict
	// reasonNotYetKnown: GitHub had not finished computing mergeability
	// after the bounded wait.
	reasonNotYetKnown
	// reasonCouldNotCheck: asking GitHub failed. The gate fails closed.
	reasonCouldNotCheck
)

// unmetRequirement is one Mutation_Requirement a pull request did not
// satisfy, and why.
type unmetRequirement struct {
	Name   string
	Reason unmetReason
}

// mutationRequirementCheck evaluates a Requirement_Set against one pull
// request. It holds what a comment already has: the installation client,
// the comment's Authorizer (which caches permission per account) and the
// pull request read at the start of the comment.
type mutationRequirementCheck struct {
	client     github.GitHubClient
	authorizer *github.Authorizer
	owner      string
	repo       string
	pr         *github.PullRequest

	// sleep waits between mergeability reads. Nil waits for real,
	// returning early with the context's error when it is done.
	sleep func(ctx context.Context, d time.Duration) error
}

// evaluate returns every requirement in requirements the pull request
// does not satisfy, in knownMutationRequirements order so the reply reads
// the same however the setting was written. Empty means all are met.
func (c *mutationRequirementCheck) evaluate(ctx context.Context, requirements []string) []unmetRequirement {
	var unmet []unmetRequirement
	for _, name := range knownMutationRequirements {
		if !slices.Contains(requirements, name) {
			continue
		}
		var reason unmetReason
		var met bool
		switch name {
		case MutationRequirementApproved:
			met, reason = c.approved(ctx)
		case MutationRequirementMergeable:
			met, reason = c.mergeable(ctx)
		}
		if !met {
			unmet = append(unmet, unmetRequirement{Name: name, Reason: reason})
		}
	}
	return unmet
}

// approved reports whether an account other than the author, with write
// permission, stands at APPROVED. An account's standing is its latest
// APPROVED, CHANGES_REQUESTED or DISMISSED review; COMMENTED and PENDING
// leave it as it was, as they do in GitHub's own review decision. The
// commit a review was given on is never read: whether a push dismisses
// an approval is the repository's branch protection to decide.
func (c *mutationRequirementCheck) approved(ctx context.Context) (bool, unmetReason) {
	reviews, err := c.client.ListReviews(ctx, c.owner, c.repo, c.pr.Number)
	if err != nil {
		slog.ErrorContext(ctx, "listing reviews for a mutation requirement",
			"owner", c.owner, "repo", c.repo, "pr_number", c.pr.Number, "error", err)
		return false, reasonCouldNotCheck
	}

	// Keyed case-insensitively, as GitHub logins are; order keeps the
	// first appearance so candidates are asked in a stable order.
	standing := make(map[string]string)
	login := make(map[string]string)
	var order []string
	for _, r := range reviews {
		switch r.State {
		case "APPROVED", "CHANGES_REQUESTED", "DISMISSED":
		default:
			continue
		}
		key := strings.ToLower(r.Author)
		if _, seen := standing[key]; !seen {
			order = append(order, key)
		}
		standing[key] = r.State
		login[key] = r.Author
	}

	author := strings.ToLower(c.pr.Author)
	couldNotCheck := false
	for _, key := range order {
		if key == author || standing[key] != "APPROVED" {
			continue
		}
		ok, err := c.authorizer.HasWritePermission(ctx, c.owner, c.repo, login[key])
		if err != nil {
			// Another approver may still settle it; only if none does is
			// this failure the answer.
			slog.ErrorContext(ctx, "checking an approver's write permission for a mutation requirement",
				"owner", c.owner, "repo", c.repo, "pr_number", c.pr.Number, "approver", login[key], "error", err)
			couldNotCheck = true
			continue
		}
		if ok {
			return true, 0
		}
	}
	if couldNotCheck {
		return false, reasonCouldNotCheck
	}
	return false, reasonUnmet
}

// mergeable reports GitHub's conflict-only answer. When the pull request
// already read says nil, GitHub is still computing it, so it is read
// again up to mergeableRetries times, mergeableRetryWait apart. Still nil
// after that is neither met nor a conflict: it is not yet known.
func (c *mutationRequirementCheck) mergeable(ctx context.Context) (bool, unmetReason) {
	value := c.pr.Mergeable
	for attempt := 0; value == nil && attempt < mergeableRetries; attempt++ {
		if err := c.wait(ctx, mergeableRetryWait); err != nil {
			slog.ErrorContext(ctx, "waiting for GitHub to compute mergeability",
				"owner", c.owner, "repo", c.repo, "pr_number", c.pr.Number, "error", err)
			return false, reasonCouldNotCheck
		}
		pr, err := c.client.GetPullRequest(ctx, c.owner, c.repo, c.pr.Number)
		if err != nil {
			slog.ErrorContext(ctx, "reading the pull request for a mutation requirement",
				"owner", c.owner, "repo", c.repo, "pr_number", c.pr.Number, "error", err)
			return false, reasonCouldNotCheck
		}
		value = pr.Mergeable
	}
	switch {
	case value == nil:
		return false, reasonNotYetKnown
	case !*value:
		return false, reasonConflict
	default:
		return true, 0
	}
}

func (c *mutationRequirementCheck) wait(ctx context.Context, d time.Duration) error {
	if c.sleep != nil {
		return c.sleep(ctx, d)
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// mutationRequirementsReply renders the one reply a command gets when its
// Mutating_Operation Targets are withheld, naming every unmet
// requirement. command is the command as written, without backticks.
func mutationRequirementsReply(command string, unmet []unmetRequirement) string {
	var b strings.Builder
	fmt.Fprintf(&b, "`%s` was not run. This pull request must first:", command)
	for _, u := range unmet {
		b.WriteString("\n- ")
		b.WriteString(unmetRequirementLine(u))
	}
	return b.String()
}

func unmetRequirementLine(u unmetRequirement) string {
	var line string
	switch u.Name {
	case MutationRequirementApproved:
		line = "be approved by someone with write access other than its author"
	case MutationRequirementMergeable:
		line = "have no merge conflicts"
	default:
		line = "satisfy `" + u.Name + "`"
	}
	switch u.Reason {
	case reasonConflict:
		return line + " (GitHub reports one)"
	case reasonNotYetKnown:
		return line + " (GitHub has not finished checking; try again in a moment)"
	case reasonCouldNotCheck:
		return line + " (turnip could not check this; try again)"
	default:
		return line
	}
}
