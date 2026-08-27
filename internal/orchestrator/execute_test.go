package orchestrator

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivanvc/turnip/internal/config"
	"github.com/ivanvc/turnip/internal/github"
	"github.com/ivanvc/turnip/internal/jobs"
	"github.com/ivanvc/turnip/internal/lock"
	"github.com/ivanvc/turnip/internal/plugin"
)

// fakeLockManager is a scriptable lock.LockManager. Every method defaults
// to a harmless zero behavior unless its corresponding func field is set.
type fakeLockManager struct {
	acquireLockFunc   func(ctx context.Context, projectKey string, prNumber int, url, lockedBy string) (bool, error)
	storePlanDataFunc func(ctx context.Context, projectKey string, prNumber int, planData []byte, summary plugin.ChangeSummary) error
	getPlanDataFunc   func(ctx context.Context, projectKey string, prNumber int) ([]byte, plugin.ChangeSummary, error)
	releaseLockFunc   func(ctx context.Context, projectKey string, prNumber int) error
	getLockStatusFunc func(ctx context.Context, projectKey string) (*lock.LockStatus, error)
	isLockedByPRFunc  func(ctx context.Context, projectKey string, prNumber int) (bool, error)
}

func (f *fakeLockManager) AcquireLock(ctx context.Context, projectKey string, prNumber int, url, lockedBy string) (bool, error) {
	if f.acquireLockFunc != nil {
		return f.acquireLockFunc(ctx, projectKey, prNumber, url, lockedBy)
	}
	return true, nil
}
func (f *fakeLockManager) StorePlanData(ctx context.Context, projectKey string, prNumber int, planData []byte, summary plugin.ChangeSummary) error {
	if f.storePlanDataFunc != nil {
		return f.storePlanDataFunc(ctx, projectKey, prNumber, planData, summary)
	}
	return nil
}
func (f *fakeLockManager) GetPlanData(ctx context.Context, projectKey string, prNumber int) ([]byte, plugin.ChangeSummary, error) {
	if f.getPlanDataFunc != nil {
		return f.getPlanDataFunc(ctx, projectKey, prNumber)
	}
	return []byte("plan-data"), plugin.ChangeSummary{}, nil
}
func (f *fakeLockManager) ReleaseLock(ctx context.Context, projectKey string, prNumber int) error {
	if f.releaseLockFunc != nil {
		return f.releaseLockFunc(ctx, projectKey, prNumber)
	}
	return nil
}
func (f *fakeLockManager) GetLockStatus(ctx context.Context, projectKey string) (*lock.LockStatus, error) {
	if f.getLockStatusFunc != nil {
		return f.getLockStatusFunc(ctx, projectKey)
	}
	return &lock.LockStatus{Locked: true, PRNumber: 99}, nil
}
func (f *fakeLockManager) IsLockedByPR(ctx context.Context, projectKey string, prNumber int) (bool, error) {
	if f.isLockedByPRFunc != nil {
		return f.isLockedByPRFunc(ctx, projectKey, prNumber)
	}
	return true, nil
}

var _ lock.LockManager = (*fakeLockManager)(nil)

// fakeExecuteClient is a minimal github.GitHubClient for execute.go's
// needs: CreateCheckRun, UpdateCheckRun, GenerateInstallationToken.
type fakeExecuteClient struct {
	github.GitHubClient
	createCheckRunErr    error
	generateTokenErr     error
	updateCheckRunCalled int
}

func (f *fakeExecuteClient) CreateCheckRun(ctx context.Context, owner, repo string, opts github.CheckRunOptions) (int64, error) {
	if f.createCheckRunErr != nil {
		return 0, f.createCheckRunErr
	}
	return 555, nil
}
func (f *fakeExecuteClient) UpdateCheckRun(ctx context.Context, owner, repo string, checkRunID int64, opts github.CheckRunOptions) error {
	f.updateCheckRunCalled++
	return nil
}
func (f *fakeExecuteClient) GenerateInstallationToken(ctx context.Context) (string, error) {
	if f.generateTokenErr != nil {
		return "", f.generateTokenErr
	}
	return "token", nil
}

// fakeJobCreator publishes a scripted result to the operation-done
// channel as soon as Create is called, extracting the operation ID from
// the Job's labels (jobs.BuildJob always sets turnip.io/operation-id).
// Waits for the subscriber to actually be listening first (Pub/Sub has
// no replay) — this only matters because the fake has near-zero latency;
// a real Runner's round trip makes this ordering a non-issue in practice.
type fakeJobCreator struct {
	// t is assert.TestingT, not *testing.T, so this fake can also be
	// driven from a rapid.Check property test — *rapid.T satisfies
	// testify's TestingT (this repo's established convention).
	t         assert.TestingT
	redis     *redis.Client
	createErr error
	result    github.ProjectResult
	statusFn  func(ctx context.Context, jobName string) (*jobs.JobStatus, error)

	mu          sync.Mutex
	created     int
	capturedJob *batchv1.Job
}

func (f *fakeJobCreator) createCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.created
}

func (f *fakeJobCreator) lastCreatedJob() *batchv1.Job {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.capturedJob
}

func (f *fakeJobCreator) Create(ctx context.Context, job *batchv1.Job) (*batchv1.Job, error) {
	f.mu.Lock()
	f.created++
	f.capturedJob = job
	f.mu.Unlock()
	if f.createErr != nil {
		return nil, f.createErr
	}
	operationID := job.Labels["turnip.io/operation-id"]
	job.Name = "turnip-runner-" + operationID
	go func() {
		// assert, not require: this runs on a non-test goroutine, and
		// require's FailNow/runtime.Goexit doesn't correctly fail the
		// test from here (this repo's testifylint go-require convention).
		ok := assert.Eventually(f.t, func() bool {
			return f.redis.PubSubNumSub(context.Background(), doneChannel(operationID)).Val()[doneChannel(operationID)] > 0
		}, time.Second, time.Millisecond)
		if !ok {
			return
		}
		assert.NoError(f.t, publishDone(context.Background(), f.redis, operationID, f.result))
	}()
	return job, nil
}

func (f *fakeJobCreator) Status(ctx context.Context, jobName string) (*jobs.JobStatus, error) {
	if f.statusFn != nil {
		return f.statusFn(ctx, jobName)
	}
	return &jobs.JobStatus{JobFound: false}, nil
}

func testOrchestrator(t *testing.T, locks lock.LockManager, jobsClient jobCreator) (*Orchestrator, *redis.Client) {
	t.Helper()
	client := newTestRedisClient(t)
	if fake, ok := jobsClient.(*fakeJobCreator); ok && fake.redis == nil {
		fake.redis = client
	}
	o := &Orchestrator{
		locks:         locks,
		jobs:          jobsClient,
		plugins:       testRegistry(),
		records:       newRecordStore(client),
		redis:         client,
		startTimeout:  5 * time.Minute,
		sweepInterval: 30 * time.Second,
	}
	return o, client
}

func testHelmfileTarget() Target {
	return Target{
		Project:     config.Project{Name: "helm-a", Directory: "a", Tool: "helmfile"},
		Operation:   "diff",
		TriggeredBy: "auto",
	}
}

var testRepo = github.Repository{Owner: "owner", Name: "repo", URL: "https://github.com/owner/repo"}
var testPR = github.PullRequest{Number: 42, HeadSHA: "abc123", BaseRef: "main"}

func TestExecuteOne_PlanLockConflictIsRejected(t *testing.T) {
	locks := &fakeLockManager{
		acquireLockFunc: func(ctx context.Context, projectKey string, prNumber int, url, lockedBy string) (bool, error) {
			return false, nil
		},
		getLockStatusFunc: func(ctx context.Context, projectKey string) (*lock.LockStatus, error) {
			return &lock.LockStatus{Locked: true, PRNumber: 7}, nil
		},
	}
	o, _ := testOrchestrator(t, locks, &fakeJobCreator{t: t})
	client := &fakeExecuteClient{}

	result := o.executeOne(context.Background(), client, testRepo, testPR, 1, testHelmfileTarget())
	assert.False(t, result.Success)
	assert.Contains(t, result.Output, "#7")
}

func TestExecuteOne_ApplyWithoutLockIsRejected(t *testing.T) {
	locks := &fakeLockManager{
		isLockedByPRFunc: func(ctx context.Context, projectKey string, prNumber int) (bool, error) {
			return false, nil
		},
	}
	o, _ := testOrchestrator(t, locks, &fakeJobCreator{t: t})
	client := &fakeExecuteClient{}

	target := testHelmfileTarget()
	target.Operation = "apply"
	result := o.executeOne(context.Background(), client, testRepo, testPR, 1, target)
	assert.False(t, result.Success)
	assert.Contains(t, result.Output, "new plan is required")
}

func TestExecuteOne_ApplyWithoutPlanDataIsRejected(t *testing.T) {
	locks := &fakeLockManager{
		getPlanDataFunc: func(ctx context.Context, projectKey string, prNumber int) ([]byte, plugin.ChangeSummary, error) {
			return nil, plugin.ChangeSummary{}, lock.ErrNoPlanData
		},
	}
	o, _ := testOrchestrator(t, locks, &fakeJobCreator{t: t})
	client := &fakeExecuteClient{}

	target := testHelmfileTarget()
	target.Operation = "apply"
	result := o.executeOne(context.Background(), client, testRepo, testPR, 1, target)
	assert.False(t, result.Success)
	assert.Contains(t, result.Output, "new plan is required")
}

func TestExecuteOne_JobCreationErrorIsRejectedAndRecordCleanedUp(t *testing.T) {
	locks := &fakeLockManager{}
	jobsClient := &fakeJobCreator{t: t, createErr: errors.New("boom")}
	o, redisClient := testOrchestrator(t, locks, jobsClient)
	client := &fakeExecuteClient{}

	result := o.executeOne(context.Background(), client, testRepo, testPR, 1, testHelmfileTarget())
	assert.False(t, result.Success)
	assert.Equal(t, 1, client.updateCheckRunCalled)

	keys, err := o.records.ScanOperationKeys(context.Background())
	require.NoError(t, err)
	assert.Empty(t, keys, "operation record should be deleted after a job creation failure")
	_ = redisClient
}

func TestExecuteOne_SuccessfulPlanPublishesResult(t *testing.T) {
	locks := &fakeLockManager{}
	want := github.ProjectResult{ProjectName: "helm-a", Tool: "helmfile", Operation: "diff", Success: true, Output: "no changes"}
	jobsClient := &fakeJobCreator{t: t, result: want}
	o, _ := testOrchestrator(t, locks, jobsClient)
	client := &fakeExecuteClient{}

	result := o.executeOne(context.Background(), client, testRepo, testPR, 1, testHelmfileTarget())
	assert.Equal(t, want, result)
}

func TestExecuteOne_JobCarriesPullRequestBaseRef(t *testing.T) {
	locks := &fakeLockManager{}
	jobsClient := &fakeJobCreator{t: t, result: github.ProjectResult{Success: true}}
	o, _ := testOrchestrator(t, locks, jobsClient)
	client := &fakeExecuteClient{}

	o.executeOne(context.Background(), client, testRepo, testPR, 1, testHelmfileTarget())

	job := jobsClient.lastCreatedJob()
	require.NotNil(t, job)
	require.Len(t, job.Spec.Template.Spec.Containers, 1)
	env := make(map[string]string, len(job.Spec.Template.Spec.Containers[0].Env))
	for _, e := range job.Spec.Template.Spec.Containers[0].Env {
		env[e.Name] = e.Value
	}
	assert.Equal(t, testPR.BaseRef, env["TURNIP_BASE_REF"])
}

func TestExecuteTargets_RunsConcurrentlyAndWaitsForAll(t *testing.T) {
	locks := &fakeLockManager{}
	jobsClient := &fakeJobCreator{t: t, result: github.ProjectResult{Success: true}}
	o, _ := testOrchestrator(t, locks, jobsClient)
	client := &fakeExecuteClient{}

	targets := []Target{testHelmfileTarget(), testHelmfileTarget()}
	targets[1].Project.Name = "helm-b"

	results := o.executeTargets(context.Background(), client, testRepo, testPR, 1, targets)
	assert.Len(t, results, 2)
}
