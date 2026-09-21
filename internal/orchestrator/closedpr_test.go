package orchestrator

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivanvc/turnip/internal/github"
	"github.com/ivanvc/turnip/internal/lock"
)

// openPR and closedPR are the two fixtures this file turns on. Written
// out rather than shared with the rest of the package so that the field
// under test is visible in every case.
func openPR() *github.PullRequest {
	return &github.PullRequest{Number: 42, HeadSHA: "abc", Open: true,
		HeadRepo: github.Repository{Owner: "owner", Name: "repo"}}
}

func closedPR() *github.PullRequest {
	return &github.PullRequest{Number: 42, HeadSHA: "abc", Open: false,
		HeadRepo: github.Repository{Owner: "owner", Name: "repo"}}
}

// A closed pull request runs nothing, and says so. Merged is the same
// state by construction — GitHub reports a merged pull request as
// "closed", and the mapping tests in internal/github pin that — so the
// interesting assertions here are about what the orchestrator does with
// Open, not about how it was derived.
func TestHandleIssueComment_ClosedPullRequestIsRefused(t *testing.T) {
	var acquired int
	locks := &fakeLockManager{
		acquireForPlanFunc: func(ctx context.Context, projectKey string, prNumber int, url, lockedBy string) (bool, lock.Transition, error) {
			acquired++
			return true, lock.Transition{}, nil
		},
	}
	o := testCommentOrchestrator(t, locks)
	client := &fakeCommentEventClient{
		permission: "write",
		files:      map[string][]byte{"turnip.yaml": []byte(validTurnipYAML)},
		pr:         closedPR(),
	}

	err := callHandleIssueComment(o, client, commentEvent("/turnip diff", "alice"))

	// ErrRefused, so the webhook answers 200 and counts the delivery once
	// as rejected rather than redelivering it.
	require.ErrorIs(t, err, github.ErrRefused)

	require.Len(t, client.postedComments(), 1, "one reply per Trigger Command, not one per Project")
	assert.Contains(t, client.postedComments()[0], "closed")

	assert.Empty(t, client.getFileCallLog(),
		"the refusal must run before the configuration is read")
	assert.Zero(t, acquired, "and before any Lock is acquired")
}

// The control. If this fails, a fixture is describing a closed pull
// request rather than the guard being wrong.
func TestHandleIssueComment_OpenPullRequestStillRuns(t *testing.T) {
	o := testCommentOrchestrator(t, &fakeLockManager{})
	client := &fakeCommentEventClient{
		permission: "write",
		files:      map[string][]byte{"turnip.yaml": []byte(validTurnipYAML)},
		pr:         openPR(),
	}

	require.NoError(t, callHandleIssueComment(o, client, commentEvent("/turnip diff helm-a", "alice")))
	assert.NotEmpty(t, client.getFileCallLog(), "an open pull request reads its configuration")
}

// Open is false by omission, and that must refuse rather than run. This
// pins the direction against a future reader who "fixes" the fixture
// churn by flipping the field's sense.
func TestHandleIssueComment_ZeroValueOpenIsRefused(t *testing.T) {
	o := testCommentOrchestrator(t, &fakeLockManager{})
	client := &fakeCommentEventClient{
		permission: "write",
		files:      map[string][]byte{"turnip.yaml": []byte(validTurnipYAML)},
		// Deliberately no Open field at all.
		pr: &github.PullRequest{Number: 42, HeadSHA: "abc",
			HeadRepo: github.Repository{Owner: "owner", Name: "repo"}},
	}

	err := callHandleIssueComment(o, client, commentEvent("/turnip diff", "alice"))
	require.ErrorIs(t, err, github.ErrRefused)
	assert.Empty(t, client.getFileCallLog())
}

// Both guards apply; the fork one must win. Nothing else in the suite
// would notice if they were swapped, and swapping them undoes Slice 15's
// decision that a fork gets no feedback — not this slice's.
func TestHandleIssueComment_ClosedForkPullRequestIsSilent(t *testing.T) {
	o := testCommentOrchestrator(t, &fakeLockManager{})
	client := &fakeCommentEventClient{
		permission: "write",
		files:      map[string][]byte{"turnip.yaml": []byte(validTurnipYAML)},
		pr: &github.PullRequest{Number: 42, HeadSHA: "abc", Open: false,
			HeadRepo: github.Repository{Owner: "contributor", Name: "repo"}},
	}

	err := callHandleIssueComment(o, client, commentEvent("/turnip diff", "alice"))

	require.ErrorIs(t, err, github.ErrRefused)
	assert.Empty(t, client.postedComments(),
		"a fork's refusal is silent, and a closed fork must not gain a reply through this slice")
}

// The reopen half. The second and third cases are what prove the action
// joined the existing arm rather than getting one of its own — a separate
// arm would pass the first and fail these.
func TestHandlePullRequest_ReopenedPlansLikeAnOpen(t *testing.T) {
	t.Run("plans the Projects its changes match", func(t *testing.T) {
		o, _ := testPullRequestOrchestrator(t)
		fakeClient := &fakePRClient{
			files:         map[string][]byte{"turnip.yaml": []byte(validTurnipYAML)},
			modifiedFiles: []string{"a/main.tf"},
		}
		o.installationClient = func(id int64) github.GitHubClient { return fakeClient }

		event := &github.WebhookEvent{
			Action:       "reopened",
			Repository:   github.Repository{Owner: "owner", Name: "repo"},
			PullRequest:  openPR(),
			Installation: github.Installation{ID: 1},
		}
		require.NoError(t, o.HandlePullRequest(context.Background(), event))

		assert.NotEmpty(t, fakeClient.getFileCallLog(),
			"a reopened pull request is planned exactly as an opened one is")
	})

	t.Run("a reopened draft is not planned", func(t *testing.T) {
		o, _ := testPullRequestOrchestrator(t)
		fakeClient := &fakePRClient{files: map[string][]byte{"turnip.yaml": []byte(validTurnipYAML)}}
		o.installationClient = func(id int64) github.GitHubClient { return fakeClient }

		pr := openPR()
		pr.Draft = true
		event := &github.WebhookEvent{
			Action:       "reopened",
			Repository:   github.Repository{Owner: "owner", Name: "repo"},
			PullRequest:  pr,
			Installation: github.Installation{ID: 1},
		}
		require.NoError(t, o.HandlePullRequest(context.Background(), event))

		assert.Empty(t, fakeClient.getFileCallLog(),
			"the draft guard belongs to the arm reopened joined, so it applies without a second rule")
	})

	t.Run("a reopened fork pull request is refused", func(t *testing.T) {
		o, _ := testPullRequestOrchestrator(t)
		fakeClient := &fakePRClient{files: map[string][]byte{"turnip.yaml": []byte(validTurnipYAML)}}
		o.installationClient = func(id int64) github.GitHubClient { return fakeClient }

		pr := openPR()
		pr.HeadRepo = github.Repository{Owner: "contributor", Name: "repo"}
		event := &github.WebhookEvent{
			Action:       "reopened",
			Repository:   github.Repository{Owner: "owner", Name: "repo"},
			PullRequest:  pr,
			Installation: github.Installation{ID: 1},
		}

		err := o.HandlePullRequest(context.Background(), event)
		require.ErrorIs(t, err, github.ErrRefused,
			"the fork refusal belongs to the same arm, and applies without a second rule")
		assert.Empty(t, fakeClient.getFileCallLog())
	})
}

// Requirement 4, pinned by a test that names this slice's field. The
// existing ClosedDraft/HandlePRClosed family predates Open, so none of
// them would notice a guard placed where it can reach the cleanup path.
func TestHandlePullRequest_ClosedEventStillReleasesLocksWhenNotOpen(t *testing.T) {
	var released []string
	locks := &fakeLockManager{
		isLockedByPRFunc: func(ctx context.Context, projectKey string, prNumber int) (bool, error) {
			return true, nil
		},
		releaseLockFunc: func(ctx context.Context, projectKey string, prNumber int) error {
			released = append(released, projectKey)
			return nil
		},
	}
	o, _ := testPullRequestOrchestrator(t)
	o.locks = locks
	fakeClient := &fakePRClient{files: map[string][]byte{"turnip.yaml": []byte(validTurnipYAML)}}
	o.installationClient = func(id int64) github.GitHubClient { return fakeClient }

	event := &github.WebhookEvent{
		Action:     "closed",
		Repository: github.Repository{Owner: "owner", Name: "repo"},
		// Open is false, which is exactly what a closed event reports.
		PullRequest:  closedPR(),
		Installation: github.Installation{ID: 1},
	}
	require.NoError(t, o.HandlePullRequest(context.Background(), event))

	assert.NotEmpty(t, released,
		"closing must still release Locks; a guard one level too high would strand them")
}
