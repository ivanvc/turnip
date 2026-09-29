package orchestrator

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	batchv1 "k8s.io/api/batch/v1"

	"github.com/ivanvc/turnip/internal/config"
	"github.com/ivanvc/turnip/internal/github"
	"github.com/ivanvc/turnip/internal/jobs"
	"github.com/ivanvc/turnip/internal/lock"
)

// Slice 43's checkpoint (runner-defaults tasks.md 3.1): every reader sees
// the Effective_Runner. Each test starts from turnip.yaml bytes through
// config.Parse, never a hand-built Project, so a reader that ever saw the
// Project as written, rather than merged over the top-level `runner:`
// block, would fail here.

// inheritedEnvYAML shares two variables at the top level. web overrides
// one and adds one set to the empty string; api states no runner: at all.
const inheritedEnvYAML = `schemaVersion: v1alpha3
runner:
  env:
    KUBECONFIG: /turnip/src/.turnip/kubeconfig
    AWS_REGION: us-east-1
projects:
  - name: web
    directory: web
    uses: helmfile@v1.7.4
    whenModified: ["web/**"]
    runner:
      env:
        AWS_REGION: us-west-2
        EMPTY: ""
  - name: api
    directory: api
    uses: helmfile@v1.7.4
    whenModified: ["api/**"]
`

// wantWebEnv and wantAPIEnv are the Effective_Runner env each Project's
// tool container must carry.
var (
	wantWebEnv = map[string]string{
		"KUBECONFIG": "/turnip/src/.turnip/kubeconfig",
		"AWS_REGION": "us-west-2",
		"EMPTY":      "",
	}
	wantAPIEnv = map[string]string{
		"KUBECONFIG": "/turnip/src/.turnip/kubeconfig",
		"AWS_REGION": "us-east-1",
	}
)

// parsedProject parses turnip.yaml with the Catalog a Server running the
// test Plugins builds, and returns the named Project, as fetchConfig would
// hand it to executeOne.
func parsedProject(t *testing.T, yaml, name string) config.Project {
	t.Helper()
	cfg, err := config.Parse([]byte(yaml), testCatalog())
	require.NoError(t, err)
	for _, p := range cfg.Projects {
		if p.Name == name {
			return p
		}
	}
	require.FailNow(t, "no such Project in the parsed file", "%q", name)
	return config.Project{}
}

// recordingJobCreator keeps every Job created, not only the last: the
// automatic plan dispatches one per matched Project, concurrently.
type recordingJobCreator struct {
	*fakeJobCreator

	mu   sync.Mutex
	jobs []*batchv1.Job
}

func (r *recordingJobCreator) Create(ctx context.Context, job *batchv1.Job) (*batchv1.Job, error) {
	r.mu.Lock()
	r.jobs = append(r.jobs, job)
	r.mu.Unlock()
	return r.fakeJobCreator.Create(ctx, job)
}

func (r *recordingJobCreator) created() []*batchv1.Job {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]*batchv1.Job(nil), r.jobs...)
}

// runnerDefaultsOrchestrator is an Orchestrator whose Server default
// ServiceAccount is "turnip-runner" and whose TURNIP_ALLOWED_OVERRIDES
// permits runner.serviceAccount or not, recording every Job it creates.
func runnerDefaultsOrchestrator(t *testing.T, locks lock.LockManager, permitServiceAccount bool) (*Orchestrator, *recordingJobCreator) {
	t.Helper()
	jobsClient := &recordingJobCreator{fakeJobCreator: &fakeJobCreator{t: t, result: github.ProjectResult{Success: true}}}
	o, client := testOrchestrator(t, locks, jobsClient)
	jobsClient.redis = client
	o.runnerServiceAccount = "turnip-runner"
	o.allowedOverrides = allowServiceAccount(permitServiceAccount)
	return o, jobsClient
}

// jobsByProject indexes Jobs by the Project each one runs, read from the
// label BuildJob sets on every Job.
func jobsByProject(t *testing.T, created []*batchv1.Job) map[string]*batchv1.Job {
	t.Helper()
	out := make(map[string]*batchv1.Job, len(created))
	for _, job := range created {
		name := job.Labels[jobs.ProjectLabel]
		require.NotEmpty(t, name, "every Job is labeled with its Project")
		out[name] = job
	}
	return out
}

// toolEnv returns the tool container's env as a map, failing on a name
// set twice: a merged variable must reach the container once, not as an
// inherited value followed by the Project's.
func toolEnv(t *testing.T, job *batchv1.Job) map[string]string {
	t.Helper()
	require.NotNil(t, job)
	containers := job.Spec.Template.Spec.Containers
	require.Len(t, containers, 1)
	env := make(map[string]string, len(containers[0].Env))
	for _, e := range containers[0].Env {
		_, dup := env[e.Name]
		require.False(t, dup, "%s is set twice in the tool container", e.Name)
		env[e.Name] = e.Value
	}
	return env
}

func assertEnvSubset(t *testing.T, want, got map[string]string) {
	t.Helper()
	for name, value := range want {
		actual, ok := got[name]
		if assert.True(t, ok, "%s must reach the tool container", name) {
			assert.Equal(t, value, actual, "%s", name)
		}
	}
}

func serviceAccountOf(t *testing.T, job *batchv1.Job) string {
	t.Helper()
	require.NotNil(t, job)
	return job.Spec.Template.Spec.ServiceAccountName
}

func planTarget(project config.Project) Target {
	return Target{Project: project, Operation: "diff", TriggeredBy: "auto"}
}

const topLevelServiceAccountYAML = `schemaVersion: v1alpha3
runner:
  serviceAccount: deployer
projects:
  - name: web
    directory: web
    uses: helmfile@v1.7.4
    whenModified: ["web/**"]
`

const bothLevelsServiceAccountYAML = `schemaVersion: v1alpha3
runner:
  serviceAccount: deployer
projects:
  - name: web
    directory: web
    uses: helmfile@v1.7.4
    whenModified: ["web/**"]
    runner:
      serviceAccount: web-deployer
`

// A top-level serviceAccount is as much the pull request's choice as a
// Project's, so it is refused when not permitted (Requirement 5.1). The
// Project_Check Title does not change with where the value came from
// (5.4); the summary, which the comment also carries, names the top-level
// block (5.2).
func TestRunnerDefaults_TopLevelServiceAccountRefusedWhenNotPermitted(t *testing.T) {
	o, jobsClient := runnerDefaultsOrchestrator(t, &fakeLockManager{}, false)
	client := newSitesClient()
	target := planTarget(parsedProject(t, topLevelServiceAccountYAML, "web"))

	result := o.executeOne(context.Background(), client, testRepo, testPR, 1, target)

	assert.False(t, result.Success)
	const want = `Project "web" inherits runner.serviceAccount "deployer" from the top-level runner: block`
	assert.Contains(t, result.Output, want, "the comment names the top-level block")

	check := requireOneCreated(t, client, target)
	assert.Equal(t, "completed", check.Status)
	assert.Equal(t, "failure", check.Conclusion)
	assert.Equal(t, "runner.serviceAccount is not permitted", check.Title, "the Title is the same whichever block set it")
	assert.Contains(t, check.Summary, want, "the summary names the top-level block")
	assert.Contains(t, check.Summary, "TURNIP_ALLOWED_OVERRIDES")
	assert.NotContains(t, check.Summary, "requested", "the Project did not request it; it inherited it")

	entry := planEntry(OutcomeRefused)
	entry.Setting = overrideServiceAccount
	assert.Equal(t, map[string]ProjectEntry{"web": entry}, readSitesRecord(t, o).Projects)
	assert.Empty(t, jobsClient.created(), "a refused Operation creates no Job")
}

// The same refusal, reached from a Trigger Comment through fetchConfig
// rather than a Project parsed by the test: the reply names the top-level
// block.
func TestRunnerDefaults_TopLevelServiceAccountRefusalReachesTheComment(t *testing.T) {
	o, jobsClient := runnerDefaultsOrchestrator(t, &fakeLockManager{}, false)
	client := &fakeCommentEventClient{
		permission: "write",
		files:      map[string][]byte{"turnip.yaml": []byte(topLevelServiceAccountYAML)},
		pr:         openPR(),
	}

	require.NoError(t, callHandleIssueComment(o, client, commentEvent("/turnip diff web", "alice")))

	require.Eventually(t, func() bool { return len(client.postedComments()) >= 1 }, 2*time.Second, 10*time.Millisecond)
	posted := client.postedComments()
	require.Len(t, posted, 1)
	assert.Contains(t, posted[0], `Project "web" inherits runner.serviceAccount "deployer" from the top-level runner: block`)
	assert.Empty(t, jobsClient.created())
}

// Permitted, the inherited ServiceAccount is the one the Pod runs as, not
// the Server's default.
func TestRunnerDefaults_TopLevelServiceAccountReachesThePodWhenPermitted(t *testing.T) {
	o, jobsClient := runnerDefaultsOrchestrator(t, &fakeLockManager{}, true)
	project := parsedProject(t, topLevelServiceAccountYAML, "web")

	result := o.executeOne(context.Background(), &fakeExecuteClient{}, testRepo, testPR, 1, planTarget(project))
	require.True(t, result.Success, "the Job must be built and dispatched: %s", result.Output)

	require.Len(t, jobsClient.created(), 1)
	assert.Equal(t, "deployer", serviceAccountOf(t, jobsClient.created()[0]))
}

// The Project's own value wins over the top level's (Requirement 2.1), at
// the gate and in the Pod: permitted, the Pod runs as it; refused, the
// message says the Project requested it, since that is the block to edit.
func TestRunnerDefaults_ProjectServiceAccountWinsOverTopLevel(t *testing.T) {
	t.Run("permitted", func(t *testing.T) {
		o, jobsClient := runnerDefaultsOrchestrator(t, &fakeLockManager{}, true)
		project := parsedProject(t, bothLevelsServiceAccountYAML, "web")

		result := o.executeOne(context.Background(), &fakeExecuteClient{}, testRepo, testPR, 1, planTarget(project))
		require.True(t, result.Success, "the Job must be built and dispatched: %s", result.Output)

		require.Len(t, jobsClient.created(), 1)
		assert.Equal(t, "web-deployer", serviceAccountOf(t, jobsClient.created()[0]))
	})

	t.Run("refused", func(t *testing.T) {
		o, jobsClient := runnerDefaultsOrchestrator(t, &fakeLockManager{}, false)
		client := newSitesClient()
		target := planTarget(parsedProject(t, bothLevelsServiceAccountYAML, "web"))

		result := o.executeOne(context.Background(), client, testRepo, testPR, 1, target)

		assert.False(t, result.Success)
		check := requireOneCreated(t, client, target)
		assert.Equal(t, "runner.serviceAccount is not permitted", check.Title)
		assert.Contains(t, check.Summary, `Project "web" requested runner.serviceAccount "web-deployer"`)
		assert.NotContains(t, check.Summary, "top-level")
		assert.Empty(t, jobsClient.created())
	})
}

// Neither level sets one: the Server's default applies, and there is
// nothing to gate (Requirement 2.3).
func TestRunnerDefaults_NoServiceAccountAnywhereUsesTheServerDefault(t *testing.T) {
	o, jobsClient := runnerDefaultsOrchestrator(t, &fakeLockManager{}, false)
	project := parsedProject(t, inheritedEnvYAML, "api")

	result := o.executeOne(context.Background(), &fakeExecuteClient{}, testRepo, testPR, 1, planTarget(project))
	require.True(t, result.Success, "the Job must be built and dispatched: %s", result.Output)

	require.Len(t, jobsClient.created(), 1)
	assert.Equal(t, "turnip-runner", serviceAccountOf(t, jobsClient.created()[0]))
}

// A top-level env variable reaches the tool container beside the
// Project's own, the Project's winning on a clash and an empty value set
// as empty (Requirements 3.1, 3.2, 3.4). env is ungated at both levels
// (5.3): the Server here permits no override at all. The apply is a
// Mutating_Operation, run from the digest its plan recorded, so the
// Effective_Runner reaches the Operations that change things too.
func TestRunnerDefaults_InheritedEnvReachesTheToolContainer(t *testing.T) {
	for _, operation := range []string{"diff", "apply"} {
		t.Run(operation, func(t *testing.T) {
			// A fresh Lock per subtest: the plan leaves its Lock planning,
			// which would refuse the apply for want of a recorded plan.
			locks := &fakeLockManager{getPlanFunc: func(context.Context, string, int) (lock.PlanRecord, error) {
				return lock.PlanRecord{ImageDigest: testImageDigest}, nil
			}}
			o, jobsClient := runnerDefaultsOrchestrator(t, locks, false)

			for _, tc := range []struct {
				project string
				want    map[string]string
			}{
				{"web", wantWebEnv},
				{"api", wantAPIEnv},
			} {
				target := Target{Project: parsedProject(t, inheritedEnvYAML, tc.project), Operation: operation, TriggeredBy: "comment"}
				result := o.executeOne(context.Background(), &fakeExecuteClient{}, testRepo, testPR, 1, target)
				require.True(t, result.Success, "the Job must be built and dispatched: %s", result.Output)

				env := toolEnv(t, jobsClient.lastCreatedJob())
				assertEnvSubset(t, tc.want, env)
				if tc.project == "api" {
					assert.NotContains(t, env, "EMPTY", "one Project's own variable does not leak into another")
				}
			}
		})
	}
}

// The automatic plan reads turnip.yaml through fetchConfig and dispatches
// every matched Project: each Job carries its Project's Effective_Runner
// env (Requirement 7.1).
func TestRunnerDefaults_AutomaticPlanSeesInheritedEnv(t *testing.T) {
	o, jobsClient := runnerDefaultsOrchestrator(t, &fakeLockManager{}, false)
	client := &fakePRClient{
		files:         map[string][]byte{"turnip.yaml": []byte(inheritedEnvYAML)},
		modifiedFiles: []string{"web/helmfile.yaml", "api/helmfile.yaml"},
	}
	o.installationClient = func(int64) github.GitHubClient { return client }

	event := &github.WebhookEvent{
		Action:       "opened",
		Repository:   github.Repository{Owner: "owner", Name: "repo"},
		PullRequest:  openPR(),
		Installation: github.Installation{ID: 1},
	}
	require.NoError(t, o.HandlePullRequest(context.Background(), event))

	require.Eventually(t, func() bool { return len(client.postedComments()) >= 1 }, 2*time.Second, 10*time.Millisecond)
	created := jobsClient.created()
	require.Len(t, created, 2, "one Job per matched Project")

	byProject := jobsByProject(t, created)
	assertEnvSubset(t, wantWebEnv, toolEnv(t, byProject["web"]))
	assertEnvSubset(t, wantAPIEnv, toolEnv(t, byProject["api"]))
	assert.NotContains(t, toolEnv(t, byProject["api"]), "EMPTY")
	for name, job := range byProject {
		assert.Equal(t, "turnip-runner", serviceAccountOf(t, job), "%s sets no serviceAccount at either level", name)
	}
}

// A Trigger Comment reads turnip.yaml on its own path: its Job carries the
// Effective_Runner env as well (Requirement 7.1).
func TestRunnerDefaults_CommentTriggeredOperationSeesInheritedEnv(t *testing.T) {
	o, jobsClient := runnerDefaultsOrchestrator(t, &fakeLockManager{}, false)
	client := &fakeCommentEventClient{
		permission: "write",
		files:      map[string][]byte{"turnip.yaml": []byte(inheritedEnvYAML)},
		pr:         openPR(),
	}

	require.NoError(t, callHandleIssueComment(o, client, commentEvent("/turnip diff web", "alice")))

	require.Eventually(t, func() bool { return len(client.postedComments()) >= 1 }, 2*time.Second, 10*time.Millisecond)
	created := jobsClient.created()
	require.Len(t, created, 1)
	assertEnvSubset(t, wantWebEnv, toolEnv(t, created[0]))
}
