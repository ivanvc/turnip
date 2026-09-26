package orchestrator

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivanvc/turnip/internal/github"
	"github.com/ivanvc/turnip/internal/lock"
)

// These tests drive the Requirement_Set gate through HandleIssueComment.
// What matters is absence: a withheld Mutating_Operation leaves no Job,
// no Lock event, no check run and no Pull_Request_Record behind. Each is
// asserted through a fake that records the call, never through state
// that happens to look unchanged.

// gateClient adds to fakeCommentEventClient what the gate asks GitHub,
// counting each question: the review listing, the pull request read,
// per-account permission, and check runs created.
type gateClient struct {
	*fakeCommentEventClient

	reviews []github.Review
	// permissions answers per login; a login not listed gets the embedded
	// fake's single permission, which is the commenter's.
	permissions map[string]string

	mu          sync.Mutex
	reviewCalls int
	prCalls     int
	checkRuns   int
}

func (c *gateClient) ListReviews(context.Context, string, string, int) ([]github.Review, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.reviewCalls++
	return c.reviews, nil
}

func (c *gateClient) GetPullRequest(ctx context.Context, owner, repo string, prNumber int) (*github.PullRequest, error) {
	c.mu.Lock()
	c.prCalls++
	c.mu.Unlock()
	return c.fakeCommentEventClient.GetPullRequest(ctx, owner, repo, prNumber)
}

func (c *gateClient) GetCollaboratorPermission(ctx context.Context, owner, repo, username string) (string, error) {
	if p, ok := c.permissions[username]; ok {
		return p, nil
	}
	return c.fakeCommentEventClient.GetCollaboratorPermission(ctx, owner, repo, username)
}

func (c *gateClient) CreateCheckRun(ctx context.Context, owner, repo string, opts github.CheckRunOptions) (int64, error) {
	c.mu.Lock()
	c.checkRuns++
	c.mu.Unlock()
	return c.fakeCommentEventClient.CreateCheckRun(ctx, owner, repo, opts)
}

func (c *gateClient) counts() (reviews, prs, checkRuns int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.reviewCalls, c.prCalls, c.checkRuns
}

// gateLocks wraps fakeLockManager and counts every call that belongs to
// executing a Target: acquiring, transitioning, and the apply's reads of
// the Lock and its plan. GetLockStatus is left out on purpose, since a
// bare apply's selection reads it before the gate runs.
type gateLocks struct {
	*fakeLockManager

	mu    sync.Mutex
	calls []string
}

func (l *gateLocks) record(name string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls = append(l.calls, name)
}

func (l *gateLocks) recorded() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.calls...)
}

func (l *gateLocks) AcquireForPlan(ctx context.Context, projectKey string, prNumber int, url, lockedBy string) (bool, lock.Transition, error) {
	l.record("AcquireForPlan")
	return l.fakeLockManager.AcquireForPlan(ctx, projectKey, prNumber, url, lockedBy)
}

func (l *gateLocks) AcquireLock(ctx context.Context, projectKey string, prNumber int, url, lockedBy string) (bool, error) {
	l.record("AcquireLock")
	return l.fakeLockManager.AcquireLock(ctx, projectKey, prNumber, url, lockedBy)
}

func (l *gateLocks) Apply(ctx context.Context, projectKey string, prNumber int, ev lock.Event, plan *lock.PlanRecord) (lock.Transition, error) {
	l.record("Apply")
	return l.fakeLockManager.Apply(ctx, projectKey, prNumber, ev, plan)
}

func (l *gateLocks) IsLockedByPR(ctx context.Context, projectKey string, prNumber int) (bool, error) {
	l.record("IsLockedByPR")
	return l.fakeLockManager.IsLockedByPR(ctx, projectKey, prNumber)
}

func (l *gateLocks) GetPlan(ctx context.Context, projectKey string, prNumber int) (lock.PlanRecord, error) {
	l.record("GetPlan")
	return l.fakeLockManager.GetPlan(ctx, projectKey, prNumber)
}

// gateFixture is one comment's worth of fakes, with both requirements
// enabled unless a test clears them. The pull request's author is alice,
// who is also the commenter; bob is a second account with write access.
type gateFixture struct {
	o      *Orchestrator
	client *gateClient
	locks  *gateLocks
	jobs   *fakeJobCreator
	waits  *[]time.Duration
}

func newGateFixture(t *testing.T, turnipYAML string, mergeable *bool, reviews ...github.Review) gateFixture {
	t.Helper()
	locks := &gateLocks{fakeLockManager: &fakeLockManager{}}
	o := testCommentOrchestrator(t, locks)
	o.mutationRequirements = []string{MutationRequirementApproved, MutationRequirementMergeable}
	var waits []time.Duration
	o.requirementSleep = func(_ context.Context, d time.Duration) error {
		waits = append(waits, d)
		return nil
	}

	pr := openPR()
	pr.Author = "alice"
	pr.Mergeable = mergeable
	client := &gateClient{
		fakeCommentEventClient: &fakeCommentEventClient{
			permission: "write",
			files:      map[string][]byte{"turnip.yaml": []byte(turnipYAML)},
			pr:         pr,
		},
		reviews:     reviews,
		permissions: map[string]string{"bob": "write"},
	}
	return gateFixture{o: o, client: client, locks: locks, jobs: o.jobs.(*fakeJobCreator), waits: &waits}
}

// refusals returns the posted comments that are the gate's reply.
func refusals(posted []string) []string {
	var out []string
	for _, body := range posted {
		if strings.Contains(body, "was not run. This pull request must first:") {
			out = append(out, body)
		}
	}
	return out
}

// redisKeys lists every key the fixture's Redis holds. A withheld apply
// writes neither an Operation record nor a Pull_Request_Record, so for
// one the list is empty.
func (f gateFixture) redisKeys(t *testing.T) []string {
	t.Helper()
	keys, err := f.o.redis.Keys(context.Background(), "*").Result()
	require.NoError(t, err)
	return keys
}

// A plan is how anyone finds out what a change would do, so it runs with
// every requirement unmet, and the gate never asks GitHub about it.
func TestMutationRequirementsGate_PlanRunsAndAsksNothing(t *testing.T) {
	f := newGateFixture(t, validTurnipYAML, boolPtr(false))

	require.NoError(t, callHandleIssueComment(f.o, f.client, commentEvent("/turnip diff helm-a", "alice")))

	require.Eventually(t, func() bool { return f.jobs.createCount() == 1 }, 2*time.Second, 10*time.Millisecond,
		"the plan runs")
	reviews, prs, _ := f.client.counts()
	assert.Zero(t, reviews, "a plan never lists reviews")
	assert.Equal(t, 1, prs, "only the handler's own read of the pull request")
	assert.Empty(t, *f.waits)
	assert.Empty(t, refusals(f.client.postedComments()))
}

// Unlock is how someone recovers from a pull request that cannot satisfy
// the requirements, so it is never gated.
func TestMutationRequirementsGate_UnlockRuns(t *testing.T) {
	f := newGateFixture(t, validTurnipYAML, boolPtr(false))

	require.NoError(t, callHandleIssueComment(f.o, f.client, commentEvent("/turnip unlock", "alice")))

	posted := f.client.postedComments()
	require.Len(t, posted, 1)
	assert.Contains(t, posted[0], "Unlocked")
	assert.Contains(t, f.locks.recorded(), "Apply", "the Lock is released")
	reviews, prs, _ := f.client.counts()
	assert.Zero(t, reviews)
	assert.Equal(t, 1, prs)
}

// The property the slice exists for: an apply on a pull request that
// satisfies nothing creates nothing at all, and says why once.
func TestMutationRequirementsGate_ApplyWithBothUnmetIsWithheld(t *testing.T) {
	f := newGateFixture(t, validTurnipYAML, boolPtr(false),
		github.Review{Author: "alice", State: "APPROVED"}) // the author's own approval does not count

	logs := captureLogs(t)

	require.NoError(t, callHandleIssueComment(f.o, f.client, commentEvent("/turnip apply helm-a", "alice")))

	rec := findRecord(logs(), "refusing mutating operation with unmet mutation requirements")
	require.NotNil(t, rec, "the refusal is logged")
	assert.Equal(t, "INFO", rec["level"])
	assert.Equal(t, "alice", rec["actor"])
	assert.Equal(t, "apply", rec["operation"])
	assert.Equal(t, []any{"approved", "mergeable"}, rec["unmet"])

	posted := f.client.postedComments()
	require.Len(t, posted, 1, "one reply for the command")
	assert.Equal(t,
		"`/turnip apply helm-a` was not run. This pull request must first:\n"+
			"- be approved by someone with write access other than its author\n"+
			"- have no merge conflicts (GitHub reports one)",
		posted[0])

	assert.Zero(t, f.jobs.createCount(), "no Job")
	assert.Empty(t, f.locks.recorded(), "no Lock event")
	assert.Empty(t, f.locks.appliedEvents(), "no Lock transition")
	_, _, checkRuns := f.client.counts()
	assert.Zero(t, checkRuns, "no check run")
	assert.Empty(t, f.redisKeys(t), "no Pull_Request_Record or Operation record")
}

// Every Operation other than the plan is gated, not only the tool's
// designated apply: helmfile's sync deploys just as apply does.
func TestMutationRequirementsGate_SyncWithBothUnmetIsWithheld(t *testing.T) {
	f := newGateFixture(t, validTurnipYAML, boolPtr(false))

	require.NoError(t, callHandleIssueComment(f.o, f.client, commentEvent("/helmfile sync helm-a", "alice")))

	posted := f.client.postedComments()
	require.Len(t, posted, 1, "one reply for the command")
	assert.Equal(t,
		"`/helmfile sync helm-a` was not run. This pull request must first:\n"+
			"- be approved by someone with write access other than its author\n"+
			"- have no merge conflicts (GitHub reports one)",
		posted[0])

	assert.Zero(t, f.jobs.createCount(), "no Job")
	assert.Empty(t, f.locks.recorded(), "no Lock event")
	assert.Empty(t, f.locks.appliedEvents(), "no Lock transition")
	_, _, checkRuns := f.client.counts()
	assert.Zero(t, checkRuns, "no check run")
	assert.Empty(t, f.redisKeys(t), "no Pull_Request_Record or Operation record")
}

// The control: the same apply, with both requirements met, runs as it
// did before the gate existed.
func TestMutationRequirementsGate_ApplyWithBothMetRuns(t *testing.T) {
	f := newGateFixture(t, validTurnipYAML, boolPtr(true),
		github.Review{Author: "bob", State: "APPROVED"})

	require.NoError(t, callHandleIssueComment(f.o, f.client, commentEvent("/turnip apply helm-a", "alice")))

	require.Eventually(t, func() bool { return f.jobs.createCount() == 1 }, 2*time.Second, 10*time.Millisecond,
		"the apply runs")
	assert.Contains(t, f.locks.recorded(), "GetPlan", "and replays the recorded plan, as today")
	assert.Empty(t, refusals(f.client.postedComments()))
	reviews, prs, _ := f.client.counts()
	assert.Equal(t, 1, reviews)
	assert.Equal(t, 1, prs, "a computed mergeable needs no second read")
}

// Two apply commands ask GitHub once between them. Mergeability starts
// unknown, so the one evaluation spends its bounded retries; a second
// evaluation would double both the reads and the waits.
func TestMutationRequirementsGate_TwoApplyCommandsAskOnce(t *testing.T) {
	f := newGateFixture(t, multiProjectTurnipYAML, nil)

	require.NoError(t, callHandleIssueComment(f.o, f.client,
		commentEvent("/turnip apply helm-a\n/helmfile apply helm-b", "alice")))

	reviews, prs, _ := f.client.counts()
	assert.Equal(t, 1, reviews, "reviews are listed once per comment")
	assert.Equal(t, 1+mergeableRetries, prs, "the handler's read, then the bounded retries, once")
	assert.Equal(t, []time.Duration{mergeableRetryWait, mergeableRetryWait, mergeableRetryWait}, *f.waits)

	got := refusals(f.client.postedComments())
	require.Len(t, got, 2, "one reply per command")
	assert.True(t, strings.HasPrefix(got[0], "`/turnip apply helm-a` was not run."))
	assert.True(t, strings.HasPrefix(got[1], "`/helmfile apply helm-b` was not run."))
	for _, body := range got {
		assert.Contains(t, body, "GitHub has not finished checking; try again in a moment")
	}
	assert.Zero(t, f.jobs.createCount())
	assert.Empty(t, f.locks.recorded())
}

// An empty Requirement_Set is today's behavior: nothing is asked and the
// apply runs on a pull request that would satisfy nothing.
func TestMutationRequirementsGate_EmptySetAsksNothing(t *testing.T) {
	f := newGateFixture(t, validTurnipYAML, boolPtr(false))
	f.o.mutationRequirements = nil

	require.NoError(t, callHandleIssueComment(f.o, f.client, commentEvent("/turnip apply helm-a", "alice")))

	require.Eventually(t, func() bool { return f.jobs.createCount() == 1 }, 2*time.Second, 10*time.Millisecond)
	reviews, prs, _ := f.client.counts()
	assert.Zero(t, reviews)
	assert.Equal(t, 1, prs)
	assert.Empty(t, *f.waits)
	assert.Empty(t, refusals(f.client.postedComments()))
}

// A commenter without write permission is refused per Project as before,
// and the gate, which runs after that check, never asks about reviews.
func TestMutationRequirementsGate_NoWritePermissionRefusedFirst(t *testing.T) {
	f := newGateFixture(t, validTurnipYAML, nil)
	f.client.permission = "read"

	require.NoError(t, callHandleIssueComment(f.o, f.client, commentEvent("/turnip apply helm-a", "alice")))

	require.Eventually(t, func() bool {
		for _, body := range f.client.postedComments() {
			if strings.Contains(body, "write permission is required") {
				return true
			}
		}
		return false
	}, 2*time.Second, 10*time.Millisecond, "refused as today")
	reviews, prs, _ := f.client.counts()
	assert.Zero(t, reviews, "reviews are never listed")
	assert.Equal(t, 1, prs, "and mergeability is never retried")
	assert.Empty(t, *f.waits)
	assert.Empty(t, refusals(f.client.postedComments()))
	assert.Zero(t, f.jobs.createCount())
}

// A plan and an apply in one comment: the plan runs and the apply alone
// is withheld, so the gate's refusal does not spill onto its neighbor.
func TestMutationRequirementsGate_PlanBesideWithheldApplyStillRuns(t *testing.T) {
	f := newGateFixture(t, validTurnipYAML, boolPtr(true))

	require.NoError(t, callHandleIssueComment(f.o, f.client,
		commentEvent("/turnip diff helm-a\n/turnip apply helm-a", "alice")))

	require.Eventually(t, func() bool { return f.jobs.createCount() == 1 }, 2*time.Second, 10*time.Millisecond,
		"the plan runs")
	got := refusals(f.client.postedComments())
	require.Len(t, got, 1)
	assert.Equal(t,
		"`/turnip apply helm-a` was not run. This pull request must first:\n"+
			"- be approved by someone with write access other than its author",
		got[0])
	assert.NotContains(t, f.locks.recorded(), "GetPlan", "the apply never reaches its Lock")
}
