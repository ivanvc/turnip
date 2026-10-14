package orchestrator

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"

	"github.com/ivanvc/turnip/internal/config"
	"github.com/ivanvc/turnip/internal/github"
	"github.com/ivanvc/turnip/internal/jobs"
)

const (
	testRepositoryID     = int64(123456789)
	testRepositorySecret = "turnip-repository-123456789"
	testRunnerNamespace  = "turnip-runners"
)

// repositorySecretsVolume returns the built Job's repository-secrets
// volume, or nil when it has none.
func repositorySecretsVolume(t *testing.T, job *batchv1.Job) *corev1.Volume {
	t.Helper()
	require.NotNil(t, job, "a Job must have been built")
	for i, v := range job.Spec.Template.Spec.Volumes {
		if v.Name == "repository-secrets" {
			return &job.Spec.Template.Spec.Volumes[i]
		}
	}
	return nil
}

func secretsTarget(keys ...string) Target {
	t := testHelmfileTarget()
	t.Project.Runner.Secrets = config.SecretKeys(keys)
	return t
}

// onlyOperationRecord reads the one Operation Record executeOne left
// behind, for an Operation no Runner reported on (dispatchOnly), so
// nothing deleted it.
func onlyOperationRecord(t *testing.T, o *Orchestrator) *OperationRecord {
	t.Helper()
	keys, err := o.records.ScanOperationKeys(context.Background())
	require.NoError(t, err)
	require.Len(t, keys, 1)
	raw, err := o.redis.Get(context.Background(), keys[0]).Bytes()
	require.NoError(t, err)
	var rec OperationRecord
	require.NoError(t, json.Unmarshal(raw, &rec))
	return &rec
}

// The id from the event names the Secret the Pod mounts from, and the
// Operation Record keeps it for the sweep's hint.
func TestExecuteOne_ProjectListingKeysMountsTheRepositorysSecret(t *testing.T) {
	jobsClient := &fakeJobCreator{t: t, result: github.ProjectResult{Success: true}}
	o, _ := testOrchestrator(t, &fakeLockManager{}, jobsClient)
	repo := testRepo
	repo.ID = testRepositoryID

	refused := o.dispatchOnly(context.Background(), &fakeExecuteClient{}, repo, testPR, secretsTarget("lab-kubeconfig"))

	require.Nil(t, refused)
	vol := repositorySecretsVolume(t, jobsClient.lastCreatedJob())
	require.NotNil(t, vol, "a Project listing keys gets the repository-secrets volume")
	require.NotNil(t, vol.Secret)
	assert.Equal(t, testRepositorySecret, vol.Secret.SecretName)
	assert.Equal(t, []corev1.KeyToPath{{Key: "lab-kubeconfig", Path: "lab-kubeconfig"}}, vol.Secret.Items)

	assert.Equal(t, testRepositoryID, onlyOperationRecord(t, o).RepositoryID)
}

func TestOperationRecord_RepositoryIDIsStoredAsRepositoryID(t *testing.T) {
	data, err := json.Marshal(OperationRecord{RepositoryID: testRepositoryID})
	require.NoError(t, err)

	var fields map[string]any
	require.NoError(t, json.Unmarshal(data, &fields))
	assert.InDelta(t, float64(testRepositoryID), fields["repository_id"], 0)
}

// A Project listing keys with no repository id has no Secret that could be
// its repository's. BuildJob refuses it, and the Operation is refused the
// way any Job that cannot be built is: titled, with nothing created.
func TestExecuteOne_KeysWithoutARepositoryIDAreRefused(t *testing.T) {
	jobsClient := &fakeJobCreator{t: t}
	o, _ := testOrchestrator(t, &fakeLockManager{}, jobsClient)
	client := &fakeExecuteClient{}

	result := o.runOne(t, context.Background(), client, testRepo, testPR, 1, secretsTarget("lab-kubeconfig"))

	assert.False(t, result.Success)
	assert.Contains(t, result.Output, "building job")
	assert.Equal(t, "Runner Job could not be built", client.updatedCheckRun.Title)
	assert.Equal(t, "failure", client.updatedCheckRun.Conclusion)
	assert.Zero(t, jobsClient.createCount(), "no Job is created")

	keys, err := o.records.ScanOperationKeys(context.Background())
	require.NoError(t, err)
	assert.Empty(t, keys, "the record of an Operation that never dispatched is deleted")
}

// Without keys the id plays no part, so a zero one changes nothing.
func TestExecuteOne_NoKeysNeedNoRepositoryID(t *testing.T) {
	jobsClient := &fakeJobCreator{t: t, result: github.ProjectResult{Success: true}}
	o, _ := testOrchestrator(t, &fakeLockManager{}, jobsClient)

	result := o.runOne(t, context.Background(), &fakeExecuteClient{}, testRepo, testPR, 1, testHelmfileTarget())

	assert.True(t, result.Success, result.Output)
	assert.Nil(t, repositorySecretsVolume(t, jobsClient.lastCreatedJob()))
}

const secretsTurnipYAML = `
schemaVersion: v1alpha3
projects:
  - name: lab
    directory: a
    uses: helmfile@v1.7.4
    runner:
      secrets: [lab-kubeconfig]
    whenModified:
      - "a/**"
`

// The automatic path: the id comes from the pull_request webhook.
func TestHandlePullRequest_JobMountsTheEventRepositorysSecret(t *testing.T) {
	client := newTestRedisClient(t)
	fakeClient := &fakePRClient{
		files:         map[string][]byte{"turnip.yaml": []byte(secretsTurnipYAML)},
		modifiedFiles: []string{"a/helmfile.yaml"},
	}
	jobsClient := &fakeJobCreator{t: t, result: github.ProjectResult{Success: true, ProjectName: "lab"}}
	o := &Orchestrator{
		locks:              &fakeLockManager{},
		jobs:               jobsClient,
		plugins:            testRegistry(),
		catalog:            testCatalog(),
		records:            newRecordStore(client),
		triggers:           newTriggerStore(client),
		redis:              client,
		installationClient: func(id int64) github.GitHubClient { return fakeClient },
	}
	bindFakeRunner(o)

	event := &github.WebhookEvent{
		Action:       "opened",
		Repository:   github.Repository{ID: testRepositoryID, Owner: "owner", Name: "repo"},
		PullRequest:  &github.PullRequest{Number: 42, HeadSHA: "abc", HeadRepo: github.Repository{Owner: "owner", Name: "repo"}},
		Installation: github.Installation{ID: 1},
	}
	require.NoError(t, o.HandlePullRequest(context.Background(), event))

	require.Eventually(t, func() bool { return jobsClient.createCount() == 1 }, 2*time.Second, 10*time.Millisecond)
	vol := repositorySecretsVolume(t, jobsClient.lastCreatedJob())
	require.NotNil(t, vol)
	require.NotNil(t, vol.Secret)
	assert.Equal(t, testRepositorySecret, vol.Secret.SecretName)
}

// The comment path fetches the pull request separately, and that fetch
// carries no repository id. The id must still be the issue_comment
// webhook's.
func TestHandleIssueComment_JobMountsTheEventRepositorysSecret(t *testing.T) {
	o := testCommentOrchestrator(t, &fakeLockManager{})
	jobsClient, ok := o.jobs.(*fakeJobCreator)
	require.True(t, ok)
	client := &fakeCommentEventClient{
		permission: "write",
		files:      map[string][]byte{"turnip.yaml": []byte(secretsTurnipYAML)},
		pr:         &github.PullRequest{Number: 42, HeadSHA: "abc", Open: true, HeadRepo: github.Repository{Owner: "owner", Name: "repo"}},
	}
	event := commentEvent("/turnip diff lab", "alice")
	event.Repository.ID = testRepositoryID

	require.NoError(t, callHandleIssueComment(o, client, event))

	require.Eventually(t, func() bool { return jobsClient.createCount() == 1 }, 2*time.Second, 10*time.Millisecond)
	vol := repositorySecretsVolume(t, jobsClient.lastCreatedJob())
	require.NotNil(t, vol)
	require.NotNil(t, vol.Secret)
	assert.Equal(t, testRepositorySecret, vol.Secret.SecretName)
}

// sweepRecord times out rec against a Pod reported as status and returns
// the Project's result handed to the results comment, plus the check run
// the sweep completed.
func sweepRecord(t *testing.T, rec *OperationRecord, status *jobs.JobStatus) (github.ProjectResult, github.CheckRunOptions) {
	t.Helper()
	jobsClient := &fakeJobCreator{t: t, statusFn: func(context.Context, string) (*jobs.JobStatus, error) {
		return status, nil
	}}
	o, client := testSweepOrchestrator(t, jobsClient)
	o.runnerNamespace = testRunnerNamespace
	require.NoError(t, o.records.Create(context.Background(), rec))

	r := finalizedResult(t, o, rec.OperationID, func() {
		o.sweepOnce(context.Background())
	})
	return r, client.updatedCheckRun
}

func secretsSweepRecord(keys ...string) *OperationRecord {
	rec := sweepTestRecord("op-1", time.Now().Add(-time.Minute), false)
	rec.Project.Name = "lab"
	rec.RepositoryID = testRepositoryID
	rec.Project.Runner.Secrets = config.SecretKeys(keys)
	return rec
}

func TestSweep_PendingPodWithKeysAddsTheSecretHint(t *testing.T) {
	rec := secretsSweepRecord("lab-kubeconfig", "cloud-key")
	result, check := sweepRecord(t, rec, &jobs.JobStatus{JobFound: true, PodPhase: corev1.PodPending})

	diagnostic := "Job turnip-runner-op-1: Pod still Pending after 5 minutes"
	assert.Equal(t, diagnostic+"\n"+
		"This Project mounts keys lab-kubeconfig, cloud-key of Secret turnip-repository-123456789 in namespace turnip-runners; if that Secret or one of those keys is missing, the Pod cannot start.",
		result.Output)
	assert.Equal(t, diagnostic, check.Title, "the check's title is the diagnostic alone")
}

// A waiting container changes the diagnostic, not whether the Pod is still
// Pending, so the hint still applies.
func TestSweep_PendingPodWithAWaitingContainerAddsTheSecretHint(t *testing.T) {
	result, check := sweepRecord(t, secretsSweepRecord("lab-kubeconfig"),
		&jobs.JobStatus{JobFound: true, PodPhase: corev1.PodPending, PodReason: "ContainerCreating"})

	diagnostic := "Job turnip-runner-op-1: container stuck (ContainerCreating)"
	assert.Equal(t, diagnostic+"\n"+
		"This Project mounts keys lab-kubeconfig of Secret turnip-repository-123456789 in namespace turnip-runners; if that Secret or one of those keys is missing, the Pod cannot start.",
		result.Output)
	assert.Equal(t, diagnostic, check.Title)
}

func TestSweep_PendingPodWithoutKeysHasNoHint(t *testing.T) {
	for name, keys := range map[string]config.SecretKeys{"absent": nil, "empty": {}} {
		t.Run(name, func(t *testing.T) {
			rec := secretsSweepRecord()
			rec.Project.Runner.Secrets = keys
			result, _ := sweepRecord(t, rec, &jobs.JobStatus{JobFound: true, PodPhase: corev1.PodPending})

			assert.Equal(t, "Job turnip-runner-op-1: Pod still Pending after 5 minutes", result.Output)
		})
	}
}

func TestSweep_PodNotPendingWithKeysHasNoHint(t *testing.T) {
	for name, tc := range map[string]struct {
		status *jobs.JobStatus
		want   string
	}{
		"running": {&jobs.JobStatus{JobFound: true, PodPhase: corev1.PodRunning}, "Job turnip-runner-op-1: Pod still Running after 5 minutes"},
		"failed":  {&jobs.JobStatus{JobFound: true, PodPhase: corev1.PodFailed}, "Job turnip-runner-op-1: Pod still Failed after 5 minutes"},
		"no pod":  {&jobs.JobStatus{JobFound: true}, "Job turnip-runner-op-1: no Pod found yet after 5 minutes"},
		"no job":  {&jobs.JobStatus{JobFound: false}, "No Job/Pod found for this Operation; it may have been deleted or never successfully scheduled"},
	} {
		t.Run(name, func(t *testing.T) {
			result, _ := sweepRecord(t, secretsSweepRecord("lab-kubeconfig"), tc.status)

			assert.Equal(t, tc.want, result.Output)
		})
	}
}

// operation-lifecycle, Scenario: A missing repository Secret.
func TestSweep_AMissingRepositorySecret(t *testing.T) {
	result, check := sweepRecord(t, secretsSweepRecord("lab-kubeconfig"),
		&jobs.JobStatus{JobFound: true, PodPhase: corev1.PodPending})

	assert.Equal(t, "Job turnip-runner-op-1: Pod still Pending after 5 minutes", check.Title)
	assert.Equal(t, "failure", check.Conclusion)
	assert.Equal(t, "lab", result.ProjectName)
	assert.False(t, result.Success)
	assert.True(t, strings.HasSuffix(result.Output,
		"\nThis Project mounts keys lab-kubeconfig of Secret turnip-repository-123456789 in namespace turnip-runners; if that Secret or one of those keys is missing, the Pod cannot start."),
		"lab's output ends with the hint line: %q", result.Output)
}
