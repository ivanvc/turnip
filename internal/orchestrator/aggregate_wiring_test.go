package orchestrator

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivanvc/turnip/internal/github"
	"github.com/ivanvc/turnip/internal/lock"
	"github.com/ivanvc/turnip/internal/rpc"
)

// These tests cover where Outcomes enter the record: the sites that
// already finalise or refuse an Operation, and the automatic plan.

func readTestRecord(t *testing.T, o *Orchestrator, ref prRef) prStatus {
	t.Helper()
	st, err := o.records.ReadPRStatus(context.Background(), ref)
	require.NoError(t, err)
	return st
}

// The record is keyed by the commit the Operation ran against, which the
// Operation Record carries; createTestRecord leaves it empty.
var resultRef = prRef{Owner: "owner", Repo: "repo", PRNumber: 42}

func TestHandleResult_RecordsTheOutcomeAndPublishes(t *testing.T) {
	o, client := testResultOrchestrator(t, &fakeLockManager{})

	createTestRecord(t, o, "op-plan", "diff")
	publishedResult(t, o, "op-plan", rpc.OperationResult{Success: true})
	// Helmfile acts without changes, so even an empty diff awaits its apply.
	assert.Equal(t, OutcomeAwaitingApply, readTestRecord(t, o, resultRef).Projects["helm-a"].Outcome)
	assert.Empty(t, client.createdCheckRun.Name, "nothing is published while plans await review")

	createTestRecord(t, o, "op-apply", "apply")
	publishedResult(t, o, "op-apply", rpc.OperationResult{Success: true})
	st := readTestRecord(t, o, resultRef)
	assert.Equal(t, ProjectEntry{Outcome: OutcomeApplied, Operation: "apply", Tool: "helmfile"}, st.Projects["helm-a"])
	assert.Equal(t, aggregateCheckName, client.createdCheckRun.Name)
	assert.Equal(t, "success", client.createdCheckRun.Conclusion)
}

func TestHandleResult_FailedApplyRecordsApplyFailed(t *testing.T) {
	o, client := testResultOrchestrator(t, &fakeLockManager{})

	createTestRecord(t, o, "op-apply", "apply")
	publishedResult(t, o, "op-apply", rpc.OperationResult{Success: false})

	assert.Equal(t, OutcomeApplyFailed, readTestRecord(t, o, resultRef).Projects["helm-a"].Outcome)
	assert.Equal(t, "failure", client.createdCheckRun.Conclusion)
}

func TestReportTimeout_RecordsTheOutcome(t *testing.T) {
	o, _ := testResultOrchestrator(t, &fakeLockManager{})
	o.jobs = &fakeJobCreator{t: t}
	createTestRecord(t, o, "op-plan", "diff")
	rec, err := o.records.get(context.Background(), "op-plan")
	require.NoError(t, err)

	o.reportTimeout(context.Background(), "op-plan", rec)

	assert.Equal(t, OutcomeNotPlanned, readTestRecord(t, o, resultRef).Projects["helm-a"].Outcome)
}

// A plan refused because another pull request holds the Lock leaves the
// Project unplanned, which keeps the aggregate check from passing without
// it — and, being in progress rather than failed, never turns it red.
func TestExecuteOne_RefusedPlanIsRecordedNotPlanned(t *testing.T) {
	locks := &fakeLockManager{
		acquireForPlanFunc: func(ctx context.Context, projectKey string, prNumber int, url, lockedBy string) (bool, lock.Transition, error) {
			return false, lock.Transition{}, nil
		},
	}
	o, _ := testOrchestrator(t, locks, &fakeJobCreator{t: t})

	o.executeOne(context.Background(), &fakeExecuteClient{}, testRepo, testPR, 1, testHelmfileTarget())

	entry := readTestRecord(t, o, testRef).Projects["helm-a"]
	assert.Equal(t, OutcomeNotPlanned, entry.Outcome)
}

// A refused Mutating_Operation ran nothing and changes nothing
// (Requirement 4.7).
func TestExecuteOne_RefusedApplyLeavesTheOutcomeAlone(t *testing.T) {
	locks := &fakeLockManager{
		isLockedByPRFunc: func(ctx context.Context, projectKey string, prNumber int) (bool, error) { return false, nil },
	}
	o, _ := testOrchestrator(t, locks, &fakeJobCreator{t: t})
	require.NoError(t, o.records.WriteOutcome(context.Background(), testRef, "helm-a", ProjectEntry{Outcome: OutcomeApplied, Operation: "apply"}))

	target := testHelmfileTarget()
	target.Operation = "apply"
	result := o.executeOne(context.Background(), &fakeExecuteClient{}, testRepo, testPR, 1, target)
	require.False(t, result.Success)

	assert.Equal(t, OutcomeApplied, readTestRecord(t, o, testRef).Projects["helm-a"].Outcome)
}

var commentRef = prRef{Owner: "owner", Repo: "repo", PRNumber: 42, HeadSHA: "abc"}

// Unlocking abandons a plan; it does not carry it out, so it must not be
// able to satisfy the check (Requirement 3.5).
func TestHandleIssueComment_UnlockLeavesTheRecordAlone(t *testing.T) {
	o := testCommentOrchestrator(t, &fakeLockManager{})
	client := &fakeCommentEventClient{
		permission: "write",
		files:      map[string][]byte{"turnip.yaml": []byte(validTurnipYAML)},
		pr:         &github.PullRequest{Number: 42, HeadSHA: "abc", Open: true, HeadRepo: github.Repository{Owner: "owner", Name: "repo"}},
	}
	require.NoError(t, o.records.WriteOutcome(context.Background(), commentRef, "helm-a", ProjectEntry{Outcome: OutcomeAwaitingApply}))
	before := readTestRecord(t, o, commentRef)

	require.NoError(t, callHandleIssueComment(o, client, commentEvent("/turnip unlock", "alice")))

	assert.Equal(t, before, readTestRecord(t, o, commentRef))
}

// A refusal of the command — here an operation the tool does not have —
// is about the comment, not the Project, and must not be able to put a
// Project into the record.
func TestHandleIssueComment_UnrecognizedOperationWritesNothing(t *testing.T) {
	o := testCommentOrchestrator(t, &fakeLockManager{})
	client := &fakeCommentEventClient{
		permission: "write",
		files:      map[string][]byte{"turnip.yaml": []byte(validTurnipYAML)},
		pr:         &github.PullRequest{Number: 42, HeadSHA: "abc", Open: true, HeadRepo: github.Repository{Owner: "owner", Name: "repo"}},
	}

	require.NoError(t, callHandleIssueComment(o, client, commentEvent("/turnip bogus helm-a", "alice")))
	require.Eventually(t, func() bool { return len(client.postedComments()) > 0 }, 2*time.Second, 10*time.Millisecond)

	assert.Empty(t, readTestRecord(t, o, commentRef).Projects)
}

// fakeAutoClient is fakePRClient with GitHub's check runs modelled, for the
// automatic path's publishes.
type fakeAutoClient struct {
	*fakePRClient
	checks *fakeChecksClient
}

func (f *fakeAutoClient) CreateCheckRun(ctx context.Context, owner, repo string, opts github.CheckRunOptions) (int64, error) {
	return f.checks.CreateCheckRun(ctx, owner, repo, opts)
}

func (f *fakeAutoClient) UpdateCheckRun(ctx context.Context, owner, repo string, id int64, opts github.CheckRunOptions) error {
	return f.checks.UpdateCheckRun(ctx, owner, repo, id, opts)
}

func autoPlanEvent() *github.WebhookEvent {
	return &github.WebhookEvent{
		Type:         "pull_request",
		Action:       "opened",
		Repository:   github.Repository{Owner: "owner", Name: "repo"},
		PullRequest:  &github.PullRequest{Number: 42, HeadSHA: "abc123", HeadRepo: github.Repository{Owner: "owner", Name: "repo"}},
		Installation: github.Installation{ID: 1},
	}
}

func runAutoPlan(t *testing.T, files map[string][]byte, modified []string) (*Orchestrator, *fakeAutoClient) {
	t.Helper()
	o, _ := testPullRequestOrchestrator(t)
	client := &fakeAutoClient{
		fakePRClient: &fakePRClient{files: files, modifiedFiles: modified},
		checks:       newFakeChecksClient(),
	}
	o.installationClient = func(int64) github.GitHubClient { return client }
	require.NoError(t, o.HandlePullRequest(context.Background(), autoPlanEvent()))
	return o, client
}

func TestAutomaticPlan_InvalidConfigFailsTheCheck(t *testing.T) {
	o, client := runAutoPlan(t, map[string][]byte{"turnip.yaml": []byte("projects: [")}, nil)

	assert.True(t, readTestRecord(t, o, testRef).ConfigInvalid)
	require.NotNil(t, client.checks.current())
	assert.Equal(t, "failure", client.checks.current().Conclusion)
	assert.Len(t, client.postedComments(), 1, "the comment explaining it is still posted")
}

func TestAutomaticPlan_MissingConfigReportsNothing(t *testing.T) {
	o, client := runAutoPlan(t, nil, nil)

	assert.Zero(t, readTestRecord(t, o, testRef).Version)
	assert.Nil(t, client.checks.current())
}

func TestAutomaticPlan_NothingAffectedIsSkipped(t *testing.T) {
	_, client := runAutoPlan(t, map[string][]byte{"turnip.yaml": []byte(validTurnipYAML)}, []string{"README.md"})

	require.NotNil(t, client.checks.current())
	assert.Equal(t, "skipped", client.checks.current().Conclusion)
	assert.Equal(t, "no projects affected", client.checks.current().Title)
}

const unsupportedToolTurnipYAML = `
schemaVersion: v1alpha2
projects:
  - name: infra
    directory: infra
    uses: terraform
    whenModified:
      - "infra/**"
  - name: other
    directory: other
    uses: terraform
    whenModified:
      - "other/**"
`

// An affected Project whose tool this Server cannot run is a turnip.yaml
// problem: it fails the check, and the comment says why (Requirement 7.3).
// A Project naming such a tool that the change does not touch is not
// recorded (7.4).
func TestAutomaticPlan_UnsupportedToolFailsTheCheck(t *testing.T) {
	o, client := runAutoPlan(t, map[string][]byte{"turnip.yaml": []byte(unsupportedToolTurnipYAML)}, []string{"infra/main.tf"})

	require.Eventually(t, func() bool { return len(client.postedComments()) > 0 }, 2*time.Second, 10*time.Millisecond)
	assert.Contains(t, client.postedComments()[0], "Project `infra` uses `terraform`")

	st := readTestRecord(t, o, testRef)
	assert.Equal(t, ProjectEntry{Outcome: OutcomeUnsupported, Tool: "terraform"}, st.Projects["infra"])
	assert.NotContains(t, st.Projects, "other")
	require.NotNil(t, client.checks.current())
	assert.Equal(t, "failure", client.checks.current().Conclusion)
	assert.Equal(t, "unsupported tool: infra uses terraform", client.checks.current().Title)
}
