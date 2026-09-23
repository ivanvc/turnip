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
)

// fakeLockManager is a scriptable lock.LockManager. Every method defaults
// to a harmless zero behavior unless its corresponding func field is set.
type fakeLockManager struct {
	acquireLockFunc   func(ctx context.Context, projectKey string, prNumber int, url, lockedBy string) (bool, error)
	storePlanFunc     func(ctx context.Context, projectKey string, prNumber int, plan lock.PlanRecord) error
	getPlanFunc       func(ctx context.Context, projectKey string, prNumber int) (lock.PlanRecord, error)
	releaseLockFunc   func(ctx context.Context, projectKey string, prNumber int) error
	getLockStatusFunc func(ctx context.Context, projectKey string) (*lock.LockStatus, error)
	isLockedByPRFunc  func(ctx context.Context, projectKey string, prNumber int) (bool, error)

	acquireForPlanFunc func(ctx context.Context, projectKey string, prNumber int, url, lockedBy string) (bool, lock.Transition, error)
	applyFunc          func(ctx context.Context, projectKey string, prNumber int, ev lock.Event, plan *lock.PlanRecord) (lock.Transition, error)

	// states models just enough of a real Lock for the transition table to
	// be exercised rather than restated. An unseen key is StatePlanning:
	// that is what a Lock looks like immediately after a plan was
	// dispatched, which is the situation every test reaching HandleResult
	// is actually in.
	mu      sync.Mutex
	states  map[string]lock.LockState
	applied []lock.Event
}

func (f *fakeLockManager) AcquireForPlan(ctx context.Context, projectKey string, prNumber int, url, lockedBy string) (bool, lock.Transition, error) {
	if f.acquireForPlanFunc != nil {
		return f.acquireForPlanFunc(ctx, projectKey, prNumber, url, lockedBy)
	}
	// Honor a test that only scripted the older acquire, so existing
	// contention tests keep meaning what they meant.
	if f.acquireLockFunc != nil {
		ok, err := f.acquireLockFunc(ctx, projectKey, prNumber, url, lockedBy)
		if err != nil || !ok {
			return ok, lock.Transition{}, err
		}
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	from, seen := f.state(projectKey)
	if !seen {
		f.setState(projectKey, lock.StatePlanning)
		return true, lock.Transition{To: lock.StatePlanning}, nil
	}
	tr, _ := lock.ApplyEvent(from, lock.EventPlanDispatched)
	f.setState(projectKey, tr.To)
	return true, tr, nil
}

func (f *fakeLockManager) Apply(ctx context.Context, projectKey string, prNumber int, ev lock.Event, plan *lock.PlanRecord) (lock.Transition, error) {
	if f.applyFunc != nil {
		return f.applyFunc(ctx, projectKey, prNumber, ev, plan)
	}

	// A test that scripted which Projects this pull request holds keeps
	// deciding that: a Lock this PR does not hold has no transition to
	// take, which is what the close path reads as "skip".
	if f.isLockedByPRFunc != nil {
		held, err := f.isLockedByPRFunc(ctx, projectKey, prNumber)
		if err != nil || !held {
			return lock.Transition{}, err
		}
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	f.applied = append(f.applied, ev)
	from, _ := f.state(projectKey)
	tr, ok := lock.ApplyEvent(from, ev)
	if !ok {
		return tr, nil
	}

	// The older injection points still witness the effects, so tests
	// written against them keep asserting what they meant — a Lock was
	// released, a plan was recorded — rather than which entry point did
	// it, which is this slice's business and not theirs.
	if tr.Released {
		if f.releaseLockFunc != nil {
			if err := f.releaseLockFunc(ctx, projectKey, prNumber); err != nil {
				return lock.Transition{}, err
			}
		}
		delete(f.states, projectKey)
		return tr, nil
	}

	if plan != nil && f.storePlanFunc != nil {
		if err := f.storePlanFunc(ctx, projectKey, prNumber, *plan); err != nil {
			return lock.Transition{}, err
		}
	}
	f.setState(projectKey, tr.To)
	return tr, nil
}

// state reports the modeled state and whether the key was known. Callers
// hold f.mu.
func (f *fakeLockManager) state(projectKey string) (lock.LockState, bool) {
	s, ok := f.states[projectKey]
	if !ok {
		return lock.StatePlanning, false
	}
	return s, true
}

// appliedEvents returns the edges this fake was asked to take, so a test
// can witness which one the orchestrator classified rather than inferring
// it from the state that resulted.
func (f *fakeLockManager) appliedEvents() []lock.Event {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]lock.Event(nil), f.applied...)
}

func (f *fakeLockManager) setState(projectKey string, s lock.LockState) {
	if f.states == nil {
		f.states = make(map[string]lock.LockState)
	}
	f.states[projectKey] = s
}

func (f *fakeLockManager) AcquireLock(ctx context.Context, projectKey string, prNumber int, url, lockedBy string) (bool, error) {
	if f.acquireLockFunc != nil {
		return f.acquireLockFunc(ctx, projectKey, prNumber, url, lockedBy)
	}
	return true, nil
}
func (f *fakeLockManager) StorePlan(ctx context.Context, projectKey string, prNumber int, plan lock.PlanRecord) error {
	if f.storePlanFunc != nil {
		return f.storePlanFunc(ctx, projectKey, prNumber, plan)
	}
	return nil
}
func (f *fakeLockManager) GetPlan(ctx context.Context, projectKey string, prNumber int) (lock.PlanRecord, error) {
	if f.getPlanFunc != nil {
		return f.getPlanFunc(ctx, projectKey, prNumber)
	}
	return lock.PlanRecord{Data: []byte("plan-data")}, nil
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

	f.mu.Lock()
	defer f.mu.Unlock()
	// A key this fake has modeled reports the state it modeled. One it
	// has not reports StatePlanReady, which is what a Lock carrying a
	// usable plan looks like — the situation every test that reaches a
	// mutating Operation without first running a plan is describing.
	state, seen := f.state(projectKey)
	if !seen {
		state = lock.StatePlanReady
	}
	return &lock.LockStatus{Locked: true, PRNumber: 99, State: state}, nil
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
	createCheckRunErr error
	generateTokenErr  error
	// gitmodules is served by GetFile for .gitmodules; absent means the
	// repository declares no submodules. Read-only after construction.
	gitmodules           []byte
	updateCheckRunCalled int
	// createdCheckRun/updatedCheckRun capture the last options each call
	// received, so tests can assert what the checks tab would show —
	// notably Title/Summary, which is where an Operation's outcome is
	// carried (the Name stays stable for required status checks).
	//
	// executeTargets runs one goroutine per Target against a single
	// client, so these writes need the mutex. Tests read the fields
	// directly without locking, which is safe: executeTargets waits on
	// its WaitGroup before returning, ordering every write before any
	// read a test performs afterwards.
	mu              sync.Mutex
	createdCheckRun github.CheckRunOptions
	updatedCheckRun github.CheckRunOptions
	// tokenScope records what the last mint was asked to cover, so a test
	// can assert the Operation narrowed the credential rather than merely
	// obtaining one. Written from the same per-Target goroutines as the
	// two above, and guarded for the same reason.
	tokenScope github.TokenScope
}

func (f *fakeExecuteClient) CreateCheckRun(ctx context.Context, owner, repo string, opts github.CheckRunOptions) (int64, error) {
	f.mu.Lock()
	f.createdCheckRun = opts
	f.mu.Unlock()
	if f.createCheckRunErr != nil {
		return 0, f.createCheckRunErr
	}
	return 555, nil
}
func (f *fakeExecuteClient) UpdateCheckRun(ctx context.Context, owner, repo string, checkRunID int64, opts github.CheckRunOptions) error {
	f.mu.Lock()
	f.updateCheckRunCalled++
	f.updatedCheckRun = opts
	f.mu.Unlock()
	return nil
}

// GetFile serves .gitmodules for the token-scope lookup. The embedded
// GitHubClient is nil, so without this every execute test would panic the
// moment scoping started reading the repository.
func (f *fakeExecuteClient) GetFile(_ context.Context, _, _, path, _ string) ([]byte, error) {
	if path == ".gitmodules" && f.gitmodules != nil {
		return f.gitmodules, nil
	}
	return nil, github.ErrFileNotFound
}

func (f *fakeExecuteClient) GenerateInstallationToken(ctx context.Context, scope github.TokenScope) (github.InstallationToken, error) {
	f.mu.Lock()
	f.tokenScope = scope
	f.mu.Unlock()
	if f.generateTokenErr != nil {
		return github.InstallationToken{}, f.generateTokenErr
	}
	return github.InstallationToken{Token: "token"}, nil
}

// fakeJobCreator publishes a scripted result to the operation-done
// channel as soon as Create is called, extracting the operation ID from
// the Job's labels (jobs.BuildJob always sets jobs.OperationIDLabel).
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
	operationID := job.Labels[jobs.OperationIDLabel]
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
		locks:   locks,
		jobs:    jobsClient,
		plugins: testRegistry(),
		records: newRecordStore(client),
		redis:   client,
		// Matches what a real Server has. Left nil, every override would
		// be refused — including a Project that merely pins a tool
		// version — and the failure would look like a lock or job problem
		// rather than an overrides one.
		allowedOverrides: defaultAllowedOverrides(),
		startTimeout:     5 * time.Minute,
		sweepInterval:    30 * time.Second,
	}
	return o, client
}

func testHelmfileTarget() Target {
	return Target{
		Project:     config.Project{Name: "helm-a", Directory: "a", Uses: "helmfile", Tool: "helmfile"},
		Operation:   "diff",
		TriggeredBy: "auto",
	}
}

var testRepo = github.Repository{Owner: "owner", Name: "repo", URL: "https://github.com/owner/repo"}
var testPR = github.PullRequest{Number: 42, HeadSHA: "abc123", BaseRef: "main"}

// jobEnvValue reads one environment value off a built Job, searching both
// the init and main containers — the Job's own environment is the only
// place a replayed scope is observable end to end.
func jobEnvValue(t *testing.T, job *batchv1.Job, name string) string {
	t.Helper()
	spec := job.Spec.Template.Spec
	for _, c := range spec.InitContainers {
		for _, env := range c.Env {
			if env.Name == name {
				return env.Value
			}
		}
	}
	for _, c := range spec.Containers {
		for _, env := range c.Env {
			if env.Name == name {
				return env.Value
			}
		}
	}
	t.Fatalf("no %s on the built Job", name)
	return ""
}

// The case that was impossible before this slice. A Helmfile plan records
// no artifact, so the apply that follows must still run. Asserted as
// behavior rather than as a mock expectation, because mock expectations
// handing back bytes the real plugin never produces are exactly what hid
// this bug for the life of the project.
func TestExecuteOne_ApplyAfterAPlanWithNoArtifactRuns(t *testing.T) {
	locks := &fakeLockManager{
		getPlanFunc: func(ctx context.Context, projectKey string, prNumber int) (lock.PlanRecord, error) {
			return lock.PlanRecord{Args: []string{"-l", "name=web"}}, nil
		},
	}
	jobsClient := &fakeJobCreator{t: t, result: github.ProjectResult{Success: true}}
	o, _ := testOrchestrator(t, locks, jobsClient)

	target := testHelmfileTarget()
	target.Operation = "apply"
	result := o.executeOne(context.Background(), &fakeExecuteClient{}, testRepo, testPR, 1, target)

	assert.True(t, result.Success, "an apply must not be refused merely because its plan produced no bytes")
	assert.Equal(t, 1, jobsClient.createCount())
}

// Every mutating Operation replays the scope the plan recorded, not just
// the apply: a sync running unscoped after a scoped diff would change more
// than anyone reviewed.
func TestExecuteOne_MutatingOperationReplaysTheRecordedScope(t *testing.T) {
	locks := &fakeLockManager{
		getPlanFunc: func(ctx context.Context, projectKey string, prNumber int) (lock.PlanRecord, error) {
			return lock.PlanRecord{Args: []string{"-l", "name=web"}}, nil
		},
	}
	jobsClient := &fakeJobCreator{t: t, result: github.ProjectResult{Success: true}}
	o, _ := testOrchestrator(t, locks, jobsClient)

	target := testHelmfileTarget()
	target.Operation = "sync"
	o.executeOne(context.Background(), &fakeExecuteClient{}, testRepo, testPR, 1, target)

	job := jobsClient.lastCreatedJob()
	require.NotNil(t, job, "a sync with a recorded plan must reach a Job")
	assert.Contains(t, jobEnvValue(t, job, "TURNIP_EXTRA_ARGS"), "name=web")
}

// Only the plan chooses a scope. Table-driven so an Operation a future
// Plugin adds is not silently exempt, and asserted by absence of side
// effects — asserting the message alone would pass with the guard placed
// after the Lock, the check run or the Job.
func TestExecuteOne_MutatingOperationsRefuseArguments(t *testing.T) {
	for _, operation := range []string{"apply", "sync"} {
		t.Run(operation, func(t *testing.T) {
			jobsClient := &fakeJobCreator{t: t}
			o, _ := testOrchestrator(t, &fakeLockManager{}, jobsClient)
			client := &fakeExecuteClient{}

			target := testHelmfileTarget()
			target.Operation = operation
			target.ExtraArgs = []string{"-l", "name=web"}
			result := o.executeOne(context.Background(), client, testRepo, testPR, 1, target)

			assert.False(t, result.Success)
			assert.Contains(t, result.Output, "does not accept arguments")
			assert.Contains(t, result.Output, "-l name=web", "the refusal names what it refused")
			assert.Zero(t, jobsClient.createCount(), "no Job for an Operation that never ran")
			assert.Zero(t, client.updateCheckRunCalled, "and no check run either")
		})
	}
}

// The other half of the same rule, guarding against an over-broad refusal:
// a plan still accepts arguments, because it is the Operation whose output
// a human reviews.
// The Project_Check is named Operation first, so every diff lists together
// in the checks tab (aggregate-check-run Requirement 2.1).
func TestExecuteOne_ProjectCheckIsNamedOperationFirst(t *testing.T) {
	jobsClient := &fakeJobCreator{t: t, result: github.ProjectResult{Success: true}}
	o, _ := testOrchestrator(t, &fakeLockManager{}, jobsClient)
	client := &fakeExecuteClient{}

	o.executeOne(context.Background(), client, testRepo, testPR, 1, testHelmfileTarget())

	assert.Equal(t, "turnip/diff/helm-a", client.createdCheckRun.Name)
}

func TestExecuteOne_PlanStillAcceptsArguments(t *testing.T) {
	jobsClient := &fakeJobCreator{t: t, result: github.ProjectResult{Success: true}}
	o, _ := testOrchestrator(t, &fakeLockManager{}, jobsClient)

	target := testHelmfileTarget()
	target.ExtraArgs = []string{"-l", "name=web"}
	result := o.executeOne(context.Background(), &fakeExecuteClient{}, testRepo, testPR, 1, target)

	assert.True(t, result.Success)
	require.Equal(t, 1, jobsClient.createCount())
	assert.Contains(t, jobEnvValue(t, jobsClient.lastCreatedJob(), "TURNIP_EXTRA_ARGS"), "name=web")
}

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

	dispatchedBefore := scrapeMetric(t, "turnip_operations_dispatched_total", map[string]string{"tool": "helmfile", "operation": "diff", "outcome": "rejected"})
	lockBefore := scrapeMetric(t, "turnip_lock_attempts_total", map[string]string{"outcome": "rejected"})

	result := o.executeOne(context.Background(), client, testRepo, testPR, 1, testHelmfileTarget())
	assert.False(t, result.Success)
	assert.Contains(t, result.Output, "#7")

	assert.InDelta(t, dispatchedBefore+1, scrapeMetric(t, "turnip_operations_dispatched_total", map[string]string{"tool": "helmfile", "operation": "diff", "outcome": "rejected"}), 0.0001)
	assert.InDelta(t, lockBefore+1, scrapeMetric(t, "turnip_lock_attempts_total", map[string]string{"outcome": "rejected"}), 0.0001)
}

// Requirement 7.2: the refusal names the blocking pull request *and*
// links to it. The URL was always present in the status being read — only
// the number reached the comment.
func TestExecuteOne_PlanLockConflictCarriesTheBlockingPullRequest(t *testing.T) {
	locks := &fakeLockManager{
		acquireLockFunc: func(ctx context.Context, projectKey string, prNumber int, url, lockedBy string) (bool, error) {
			return false, nil
		},
		getLockStatusFunc: func(ctx context.Context, projectKey string) (*lock.LockStatus, error) {
			return &lock.LockStatus{
				Locked:         true,
				PRNumber:       7,
				PullRequestURL: "https://github.com/owner/repo/pull/7",
			}, nil
		},
	}
	o, _ := testOrchestrator(t, locks, &fakeJobCreator{t: t})

	result := o.executeOne(context.Background(), &fakeExecuteClient{}, testRepo, testPR, 1, testHelmfileTarget())

	require.NotNil(t, result.BlockedBy, "a contended lock must identify its holder")
	assert.Equal(t, 7, result.BlockedBy.Number)
	assert.Equal(t, "https://github.com/owner/repo/pull/7", result.BlockedBy.URL)
}

// Requirement 5.3: a holder that cannot be determined is reported without
// inventing a reference, so the comment says the Project is locked and
// stops there.
func TestExecuteOne_PlanLockConflictWithUnknownHolderInventsNothing(t *testing.T) {
	locks := &fakeLockManager{
		acquireLockFunc: func(ctx context.Context, projectKey string, prNumber int, url, lockedBy string) (bool, error) {
			return false, nil
		},
		getLockStatusFunc: func(ctx context.Context, projectKey string) (*lock.LockStatus, error) {
			return &lock.LockStatus{Locked: false}, nil
		},
	}
	o, _ := testOrchestrator(t, locks, &fakeJobCreator{t: t})

	result := o.executeOne(context.Background(), &fakeExecuteClient{}, testRepo, testPR, 1, testHelmfileTarget())

	assert.False(t, result.Success)
	assert.Nil(t, result.BlockedBy, "an unidentifiable holder must not be fabricated")
	assert.Contains(t, result.Output, "locked by another PR")
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
		getPlanFunc: func(ctx context.Context, projectKey string, prNumber int) (lock.PlanRecord, error) {
			return lock.PlanRecord{}, lock.ErrNoPlan
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

	dispatchedBefore := scrapeMetric(t, "turnip_operations_dispatched_total", map[string]string{"tool": "helmfile", "operation": "diff", "outcome": "success"})
	lockBefore := scrapeMetric(t, "turnip_lock_attempts_total", map[string]string{"outcome": "acquired"})
	durationCountBefore := scrapeMetric(t, "turnip_operation_duration_seconds", map[string]string{"tool": "helmfile", "operation": "diff"})

	result := o.executeOne(context.Background(), client, testRepo, testPR, 1, testHelmfileTarget())
	assert.Equal(t, want, result)

	assert.InDelta(t, dispatchedBefore+1, scrapeMetric(t, "turnip_operations_dispatched_total", map[string]string{"tool": "helmfile", "operation": "diff", "outcome": "success"}), 0.0001)
	assert.InDelta(t, lockBefore+1, scrapeMetric(t, "turnip_lock_attempts_total", map[string]string{"outcome": "acquired"}), 0.0001)
	assert.InDelta(t, durationCountBefore+1, scrapeMetric(t, "turnip_operation_duration_seconds", map[string]string{"tool": "helmfile", "operation": "diff"}), 0.0001)
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

// A check run that can't be created is invisible on the PR otherwise:
// the comment arrives, no check run appears, and the reason lives only in
// the Server log. The operation must still run — only the note is new.
func TestExecuteOne_CheckRunCreationFailureIsNotedInResult(t *testing.T) {
	locks := &fakeLockManager{}
	jobsClient := &fakeJobCreator{t: t, result: github.ProjectResult{Success: true, Output: "diff output"}}
	o, _ := testOrchestrator(t, locks, jobsClient)
	client := &fakeExecuteClient{createCheckRunErr: errors.New(`422 "summary", "title" weren't supplied`)}

	result := o.executeOne(context.Background(), client, testRepo, testPR, 1, testHelmfileTarget())

	assert.True(t, result.Success, "a failed check run must not fail the operation")
	assert.Contains(t, result.Output, "diff output", "the Runner's own output is kept")
	assert.Contains(t, result.Output, "check run could not be recorded")
	assert.Contains(t, result.Output, "422", "the underlying GitHub error is included")
	assert.Equal(t, 1, jobsClient.createCount(), "the Runner Job still runs")
}

func TestExecuteOne_SucceedingCheckRunAddsNoNote(t *testing.T) {
	locks := &fakeLockManager{}
	jobsClient := &fakeJobCreator{t: t, result: github.ProjectResult{Success: true, Output: "diff output"}}
	o, _ := testOrchestrator(t, locks, jobsClient)
	client := &fakeExecuteClient{}

	result := o.executeOne(context.Background(), client, testRepo, testPR, 1, testHelmfileTarget())

	assert.Equal(t, "diff output", result.Output)
}

func TestExecuteOne_JobCarriesServerDefaultServiceAccount(t *testing.T) {
	locks := &fakeLockManager{}
	jobsClient := &fakeJobCreator{t: t, result: github.ProjectResult{Success: true}}
	o, _ := testOrchestrator(t, locks, jobsClient)
	o.runnerServiceAccount = "turnip-runner"
	client := &fakeExecuteClient{}

	o.executeOne(context.Background(), client, testRepo, testPR, 1, testHelmfileTarget())

	job := jobsClient.lastCreatedJob()
	require.NotNil(t, job)
	assert.Equal(t, "turnip-runner", job.Spec.Template.Spec.ServiceAccountName)
}

func TestExecuteOne_ProjectServiceAccountRefusedWhenNotAllowed(t *testing.T) {
	locks := &fakeLockManager{}
	jobsClient := &fakeJobCreator{t: t, result: github.ProjectResult{Success: true}}
	o, _ := testOrchestrator(t, locks, jobsClient)
	o.runnerServiceAccount = "turnip-runner"
	o.allowedOverrides = map[string]bool{}
	client := &fakeExecuteClient{}

	target := testHelmfileTarget()
	target.Project.Runner.ServiceAccount = "atlantis"

	result := o.executeOne(context.Background(), client, testRepo, testPR, 1, target)

	assert.False(t, result.Success)
	assert.Contains(t, result.Output, "atlantis")
	assert.Equal(t, 0, jobsClient.createCount(), "a refused ServiceAccount must not create a Job")
}

func TestExecuteOne_ProjectServiceAccountUsedWhenAllowed(t *testing.T) {
	locks := &fakeLockManager{}
	jobsClient := &fakeJobCreator{t: t, result: github.ProjectResult{Success: true}}
	o, _ := testOrchestrator(t, locks, jobsClient)
	o.runnerServiceAccount = "turnip-runner"
	o.allowedOverrides = map[string]bool{overrideServiceAccount: true}
	client := &fakeExecuteClient{}

	target := testHelmfileTarget()
	target.Project.Runner.ServiceAccount = "turnip-runner-project"

	o.executeOne(context.Background(), client, testRepo, testPR, 1, target)

	job := jobsClient.lastCreatedJob()
	require.NotNil(t, job)
	assert.Equal(t, "turnip-runner-project", job.Spec.Template.Spec.ServiceAccountName)
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

func TestExecuteOne_CheckRunCreatedWithInProgressTitle(t *testing.T) {
	locks := &fakeLockManager{}
	jobsClient := &fakeJobCreator{t: t, result: github.ProjectResult{Success: true}}
	o, _ := testOrchestrator(t, locks, jobsClient)
	client := &fakeExecuteClient{}

	o.executeOne(context.Background(), client, testRepo, testPR, 1, testHelmfileTarget())

	assert.Equal(t, "in progress", client.createdCheckRun.Title)
	assert.NotEmpty(t, client.createdCheckRun.Summary, "GitHub rejects check-run output without a summary")
	assert.Equal(t, testPR.HeadSHA, client.createdCheckRun.HeadSHA)
}
