package orchestrator

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivanvc/turnip/internal/github"
	"github.com/ivanvc/turnip/internal/jobs"
	"github.com/ivanvc/turnip/internal/rpc"
)

// triggerKeys returns every trigger record in Redis.
func triggerKeys(t *testing.T, o *Orchestrator) []string {
	t.Helper()
	keys, err := o.redis.Keys(context.Background(), "trigger:*").Result()
	require.NoError(t, err)
	return keys
}

func operationKeys(t *testing.T, o *Orchestrator) []string {
	t.Helper()
	keys, err := o.records.ScanOperationKeys(context.Background())
	require.NoError(t, err)
	return keys
}

// A comment's commands run one after the other and are reported together:
// each command is begun once the one before it is finalized, the last
// finalization posts the results, and the trigger leaves nothing behind.
func TestHandleIssueComment_TriggerRunsEveryCommandPostsOnceAndIsDeleted(t *testing.T) {
	o := testCommentOrchestrator(t, &fakeLockManager{})
	jobsClient, ok := o.jobs.(*fakeJobCreator)
	require.True(t, ok)
	client := &fakeCommentEventClient{
		permission: "write",
		files:      map[string][]byte{"turnip.yaml": []byte(validTurnipYAML)},
		pr:         openPR(),
	}

	require.NoError(t, callHandleIssueComment(o, client, commentEvent("/turnip diff helm-a\n/turnip apply helm-a", "alice")))

	require.Eventually(t, func() bool { return len(triggerKeys(t, o)) == 0 && jobsClient.createCount() == 2 },
		5*time.Second, 5*time.Millisecond, "the trigger is deleted once its comment is posted")
	jobsClient.waitForReports()

	posted := client.postedComments()
	require.Len(t, posted, 2, "one plan comment and one apply comment, each once")
	assert.Contains(t, posted[0], "diff")
	assert.Contains(t, posted[1], "apply")
	assert.Empty(t, operationKeys(t, o), "every Operation Record is deleted at finalization")
}

// A trigger with nothing to dispatch (every Project refused during
// selection) posts its comment at once and is never recorded.
func TestHandleIssueComment_NothingToDispatchPostsAtOnceWithNoTrigger(t *testing.T) {
	o := testCommentOrchestrator(t, &fakeLockManager{})
	jobsClient, ok := o.jobs.(*fakeJobCreator)
	require.True(t, ok)
	client := &fakeCommentEventClient{
		permission: "read",
		files:      map[string][]byte{"turnip.yaml": []byte(validTurnipYAML)},
		pr:         openPR(),
	}

	require.NoError(t, callHandleIssueComment(o, client, commentEvent("/turnip apply helm-a", "alice")))

	// Posted by the time the webhook is answered: nothing is left to wait for.
	posted := client.postedComments()
	require.Len(t, posted, 1)
	assert.Contains(t, posted[0], "write permission is required")
	assert.Empty(t, triggerKeys(t, o), "no trigger record for a trigger that dispatches nothing")
	assert.Zero(t, jobsClient.createCount())
}

// The automatic plan is a trigger too: recorded while its Operation runs,
// naming the Operation's command, and posted by the finalization.
func TestHandlePullRequest_AutomaticPlanIsATrigger(t *testing.T) {
	redisClient := newTestRedisClient(t)
	client := &fakePRClient{
		files:         map[string][]byte{"turnip.yaml": []byte(validTurnipYAML)},
		modifiedFiles: []string{"a/main.tf"},
	}
	// No Runner reports until the test says so, so the trigger can be read
	// while its Operation is outstanding.
	jobsClient := &fakeJobCreator{t: t}
	o := &Orchestrator{
		locks:              &fakeLockManager{},
		jobs:               jobsClient,
		plugins:            testRegistry(),
		catalog:            testCatalog(),
		records:            newRecordStore(redisClient),
		triggers:           newTriggerStore(redisClient),
		redis:              redisClient,
		installationClient: func(int64) github.GitHubClient { return client },
		startTimeout:       5 * time.Minute,
	}
	event := &github.WebhookEvent{
		Action:       "opened",
		Repository:   github.Repository{Owner: "owner", Name: "repo"},
		PullRequest:  &github.PullRequest{Number: 42, HeadSHA: "abc", BaseRef: "main", HeadRepo: github.Repository{Owner: "owner", Name: "repo"}},
		Installation: github.Installation{ID: 7},
	}
	require.NoError(t, o.HandlePullRequest(context.Background(), event))

	var rec *OperationRecord
	require.Eventually(t, func() bool {
		keys := operationKeys(t, o)
		if len(keys) != 1 {
			return false
		}
		var err error
		rec, err = o.records.get(context.Background(), keys[0][len("operation:"):])
		return err == nil && rec != nil && rec.JobName != ""
	}, 5*time.Second, 5*time.Millisecond)

	require.NotEmpty(t, rec.TriggerID, "the Operation Record names its trigger")
	assert.Zero(t, rec.CommandIndex)
	assert.Equal(t, []string{triggerKey(rec.TriggerID)}, triggerKeys(t, o))
	st, err := o.triggers.Get(context.Background(), rec.TriggerID)
	require.NoError(t, err)
	require.NotNil(t, st)
	assert.Equal(t, int64(7), st.Meta.InstallationID)
	assert.Equal(t, 42, st.Meta.PRNumber)
	assert.Equal(t, "abc", st.Meta.HeadSHA)
	assert.Equal(t, "main", st.Meta.BaseRef)
	assert.Empty(t, client.postedComments(), "nothing is posted while the Operation is outstanding")

	require.NoError(t, o.HandleResult(context.Background(), rec.OperationID, rpc.OperationResult{Success: true, Output: "no changes"}))

	// Inside the call that finalized it, so nothing is waited for.
	posted := client.postedComments()
	require.Len(t, posted, 1)
	assert.Contains(t, posted[0], "no changes")
	assert.Empty(t, triggerKeys(t, o))

	// The Runner's retry of a result already finalized adds nothing.
	require.NoError(t, o.HandleResult(context.Background(), rec.OperationID, rpc.OperationResult{Success: true, Output: "no changes"}))
	assert.Len(t, client.postedComments(), 1)
}

// Decision 7: an Operation that names no trigger, or one Redis no longer
// has, is finalized all the same (check, Lock, record) and adds its result
// to nothing.
func TestHandleResult_OperationWithNoTriggerIsFinalizedWithoutError(t *testing.T) {
	for name, triggerID := range map[string]string{
		"names no trigger":        "",
		"names a missing trigger": "gone",
	} {
		t.Run(name, func(t *testing.T) {
			o, client := testResultOrchestrator(t, &fakeLockManager{})
			createTestRecord(t, o, "op-1", "diff")
			require.NoError(t, o.records.mutate(context.Background(), "op-1", func(rec *OperationRecord) { rec.TriggerID = triggerID }))

			require.NoError(t, o.HandleResult(context.Background(), "op-1", rpc.OperationResult{Success: true}))

			assert.Equal(t, 1, client.updateCheckRunCalled, "the check is completed")
			assert.Equal(t, "completed", client.updatedCheckRun.Status)
			assert.Empty(t, operationKeys(t, o), "the record is deleted")
			assert.Empty(t, triggerKeys(t, o), "and no trigger is created for it")
		})
	}
}

// The sweep's start timeout completes the Project in its trigger where it
// used to publish, inside the tick that timed it out: here it is the last
// Project of the last command, so the tick posts the comment.
func TestSweepOnce_StartTimeoutCompletesTheProjectInItsTrigger(t *testing.T) {
	jobsClient := &fakeJobCreator{t: t, statusFn: func(context.Context, string) (*jobs.JobStatus, error) {
		return &jobs.JobStatus{JobFound: true, PodPhase: "Pending"}, nil
	}}
	o, _ := testSweepOrchestrator(t, jobsClient)
	client := &fakeCommentEventClient{}
	o.installationClient = func(int64) github.GitHubClient { return client }
	ctx := context.Background()

	rec := sweepTestRecord("op-1", time.Now().Add(-time.Minute), false)
	rec.TriggerID, rec.CommandIndex = "t1", 0
	meta := triggerMeta{
		Repo:     github.Repository{Owner: "owner", Name: "repo"},
		PRNumber: 42,
		Commands: [][]Target{{{Project: rec.Project, Operation: "diff"}}},
	}
	require.NoError(t, o.triggers.Create(ctx, "t1", meta, nil))
	require.NoError(t, o.triggers.BeginCommand(ctx, "t1", 0, []string{rec.Project.Name}))
	require.NoError(t, o.records.Create(ctx, rec))

	failedBefore := scrapeMetric(t, "turnip_operations_dispatched_total", map[string]string{"tool": "helmfile", "operation": "diff", "outcome": "failure"})
	durationBefore := scrapeMetric(t, "turnip_operation_duration_seconds", map[string]string{"tool": "helmfile", "operation": "diff"})

	o.sweepOnce(ctx)

	posted := client.postedComments()
	require.Len(t, posted, 1, "the tick that timed out the last Operation posts the comment")
	assert.Contains(t, posted[0], "Pod still Pending after 5 minutes")
	assert.Empty(t, triggerKeys(t, o))
	assert.Empty(t, operationKeys(t, o))
	assert.InDelta(t, failedBefore+1, scrapeMetric(t, "turnip_operations_dispatched_total", map[string]string{"tool": "helmfile", "operation": "diff", "outcome": "failure"}), 0.0001)
	assert.InDelta(t, durationBefore+1, scrapeMetric(t, "turnip_operation_duration_seconds", map[string]string{"tool": "helmfile", "operation": "diff"}), 0.0001)

	// A second sweep finds nothing, and a late result is ignored.
	o.sweepOnce(ctx)
	require.NoError(t, o.HandleResult(ctx, "op-1", rpc.OperationResult{Success: true}))
	assert.Len(t, client.postedComments(), 1)
}

// A refusal after the Operation Record exists (the Job could not be
// created) deletes the record and is completed by the dispatcher, once:
// with nothing else in the trigger, the dispatcher posts the comment.
func TestBeginCommand_RefusalAfterTheRecordIsCompletedByTheDispatcherOnce(t *testing.T) {
	jobsClient := &fakeJobCreator{t: t, createErr: assert.AnError}
	o, _ := testOrchestrator(t, &fakeLockManager{}, jobsClient)
	client := &fakePRClient{}
	o.installationClient = func(int64) github.GitHubClient { return client }
	ctx := context.Background()
	meta := triggerMeta{
		Repo:           testRepo,
		InstallationID: 1,
		PRNumber:       testPR.Number,
		HeadSHA:        testPR.HeadSHA,
		BaseRef:        testPR.BaseRef,
		Commands:       [][]Target{{testHelmfileTarget()}},
	}
	require.NoError(t, o.triggers.Create(ctx, "t1", meta, nil))

	require.NoError(t, o.beginCommand(ctx, "t1", meta, 0))

	assert.Equal(t, 1, jobsClient.createCount())
	assert.Empty(t, operationKeys(t, o), "the record of a Job that was never created is deleted")
	posted := client.postedComments()
	require.Len(t, posted, 1, "the dispatcher completed the Project and finished the trigger")
	assert.Contains(t, posted[0], "creating job")
	assert.Empty(t, triggerKeys(t, o))

	// Nothing is left to finalize it a second time.
	o.sweepOnce(ctx)
	assert.Len(t, client.postedComments(), 1)
}
