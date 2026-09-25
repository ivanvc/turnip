package orchestrator

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"

	"github.com/ivanvc/turnip/internal/config"
	"github.com/ivanvc/turnip/internal/github"
	"github.com/ivanvc/turnip/internal/jobs"
	"github.com/ivanvc/turnip/internal/lock"
	"github.com/ivanvc/turnip/internal/rpc"
)

// These tests cross from a plan's result to the apply that follows it,
// which no package test does: the digest is read by HandleResult, stored on
// the Lock, and read back by executeOne for the next Job. Each step is
// covered on its own elsewhere; only the round trip shows the apply runs
// what the plan ran rather than what the tag means now.
//
// Feature: tool-images, task 8: the digest round trip.

var (
	roundTripDigestA = "sha256:" + strings.Repeat("a", 64)
	roundTripDigestB = "sha256:" + strings.Repeat("b", 64)
)

// runnerJobs stands in for the Kubernetes Job and the Runner inside it:
// each Job it creates reports its result through the real HandleResult, the
// way the Runner's gRPC stream does, so the plan's digest is recorded by
// the production path. Status answers with whatever image the fake Pod
// reports at the time it is asked.
type runnerJobs struct {
	t      *testing.T
	o      *Orchestrator
	redis  *redis.Client
	result rpc.OperationResult

	mu        sync.Mutex
	created   []*batchv1.Job
	imageID   string
	statusErr error
	statusFor []string
}

func (r *runnerJobs) Create(_ context.Context, job *batchv1.Job) (*batchv1.Job, error) {
	operationID := job.Labels[jobs.OperationIDLabel]
	job.Name = "turnip-runner-" + operationID
	r.mu.Lock()
	r.created = append(r.created, job)
	r.mu.Unlock()

	go func() {
		ctx := context.Background()
		// A Runner cannot report before its Job exists and executeOne is
		// listening: wait for both, as publishedResult does. assert, not
		// require: this is not the test goroutine.
		ok := assert.Eventually(r.t, func() bool {
			rec, err := r.o.records.get(ctx, operationID)
			return err == nil && rec.JobName != "" &&
				r.redis.PubSubNumSub(ctx, doneChannel(operationID)).Val()[doneChannel(operationID)] > 0
		}, 2*time.Second, time.Millisecond)
		if !ok {
			return
		}
		assert.NoError(r.t, r.o.HandleResult(ctx, operationID, r.result))
	}()
	return job, nil
}

func (r *runnerJobs) Status(_ context.Context, jobName string) (*jobs.JobStatus, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.statusFor = append(r.statusFor, jobName)
	if r.statusErr != nil {
		return nil, r.statusErr
	}
	return &jobs.JobStatus{JobFound: true, ImageID: r.imageID}, nil
}

// reports sets what the fake Pod reports from now on.
func (r *runnerJobs) reports(imageID string, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.imageID, r.statusErr = imageID, err
}

// statusAsked names the Jobs whose status was read, in order.
func (r *runnerJobs) statusAsked() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.statusFor...)
}

func (r *runnerJobs) createdJobs() []*batchv1.Job {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]*batchv1.Job(nil), r.created...)
}

// storingLocks is a fakeLockManager that keeps the PlanRecord its plan
// stored and serves it to the apply, as the Lock does in Redis.
func storingLocks() (*fakeLockManager, func() (lock.PlanRecord, bool)) {
	var mu sync.Mutex
	var stored *lock.PlanRecord
	locks := &fakeLockManager{
		storePlanFunc: func(_ context.Context, _ string, _ int, plan lock.PlanRecord) error {
			mu.Lock()
			defer mu.Unlock()
			stored = &plan
			return nil
		},
		getPlanFunc: func(context.Context, string, int) (lock.PlanRecord, error) {
			mu.Lock()
			defer mu.Unlock()
			if stored == nil {
				return lock.PlanRecord{}, lock.ErrNoPlan
			}
			return *stored, nil
		},
	}
	return locks, func() (lock.PlanRecord, bool) {
		mu.Lock()
		defer mu.Unlock()
		if stored == nil {
			return lock.PlanRecord{}, false
		}
		return *stored, true
	}
}

// roundTripProject resolves a `uses:` naming a tag through config.Parse
// and the Catalog a Server with the test Plugins builds, so the Project is
// what fetchConfig would hand executeOne.
func roundTripProject(t *testing.T) config.Project {
	t.Helper()
	cfg, err := config.Parse([]byte(`schemaVersion: v1alpha3
projects:
  - name: helm-a
    directory: a
    uses: helmfile@v1.7.4
`), testCatalog())
	require.NoError(t, err)
	require.Len(t, cfg.Projects, 1)
	project := cfg.Projects[0]
	require.Equal(t, "ghcr.io/helmfile/helmfile", project.Image, "precondition: the Alias resolved to the vendor image")
	require.Equal(t, "v1.7.4", project.ToolVersion, "precondition: a tag, not a digest")
	return project
}

func roundTripOrchestrator(t *testing.T) (*Orchestrator, *runnerJobs, *sitesClient, func() (lock.PlanRecord, bool)) {
	t.Helper()
	locks, stored := storingLocks()
	runner := &runnerJobs{t: t, result: rpc.OperationResult{Success: true, Changes: rpc.ChangeSummary{Add: 1}}}
	o, client := testOrchestrator(t, locks, runner)
	runner.o, runner.redis = o, client
	checks := newSitesClient()
	o.installationClient = func(int64) github.GitHubClient { return checks }
	return o, runner, checks, stored
}

func runPlan(t *testing.T, o *Orchestrator, checks *sitesClient, project config.Project) Target {
	t.Helper()
	plan := Target{Project: project, Operation: "diff", TriggeredBy: "auto"}
	result := o.executeOne(context.Background(), checks, testRepo, testPR, 1, plan)
	require.True(t, result.Success, "the plan must run and report: %s", result.Output)
	return plan
}

func TestDigestRoundTrip_ApplyRunsTheDigestThePlanRan(t *testing.T) {
	o, runner, checks, stored := roundTripOrchestrator(t)
	project := roundTripProject(t)

	runner.reports("ghcr.io/helmfile/helmfile@"+roundTripDigestA, nil)
	runPlan(t, o, checks, project)

	planJobs := runner.createdJobs()
	require.Len(t, planJobs, 1)
	require.Len(t, planJobs[0].Spec.Template.Spec.Containers, 1)
	planContainer := planJobs[0].Spec.Template.Spec.Containers[0]
	assert.Equal(t, "ghcr.io/helmfile/helmfile:v1.7.4", planContainer.Image, "the plan runs the tag")
	assert.Equal(t, corev1.PullAlways, planContainer.ImagePullPolicy)

	record, ok := stored()
	require.True(t, ok, "the plan's result must store a PlanRecord")
	assert.Equal(t, roundTripDigestA, record.ImageDigest, "the digest the plan's Pod ran is recorded on the Lock")
	assert.Equal(t, []string{planJobs[0].Name}, runner.statusAsked(), "the digest is read from the plan's own Job")

	// The tag moves: a Pod pulling it now would run another image.
	runner.reports("ghcr.io/helmfile/helmfile@"+roundTripDigestB, nil)

	apply := Target{Project: project, Operation: "apply", TriggeredBy: "comment"}
	result := o.executeOne(context.Background(), checks, testRepo, testPR, 1, apply)
	require.True(t, result.Success, "the apply must run: %s", result.Output)

	allJobs := runner.createdJobs()
	require.Len(t, allJobs, 2)
	require.Len(t, allJobs[1].Spec.Template.Spec.Containers, 1)
	applyContainer := allJobs[1].Spec.Template.Spec.Containers[0]
	assert.Equal(t, "ghcr.io/helmfile/helmfile@"+roundTripDigestA, applyContainer.Image,
		"the apply runs the recorded digest, not the tag resolved again")
	assert.Equal(t, corev1.PullIfNotPresent, applyContainer.ImagePullPolicy)
	assert.NotContains(t, applyContainer.Image, roundTripDigestB[len("sha256:"):])
}

func TestDigestRoundTrip_PlanWithoutADigestRefusesItsApply(t *testing.T) {
	cases := map[string]struct {
		imageID string
		err     error
	}{
		"a local image id, not a repository digest": {imageID: roundTripDigestA},
		"the status call fails":                     {err: errors.New("apiserver unavailable")},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			o, runner, checks, stored := roundTripOrchestrator(t)
			project := roundTripProject(t)

			runner.reports(tc.imageID, tc.err)
			runPlan(t, o, checks, project)

			record, ok := stored()
			require.True(t, ok, "the plan still succeeds and is recorded")
			require.Empty(t, record.ImageDigest, "precondition: no digest could be recorded")
			status, err := o.locks.GetLockStatus(context.Background(), "owner/repo/helm-a")
			require.NoError(t, err)
			require.Equal(t, lock.StatePlanReady, status.State, "precondition: the Lock is applicable, so only the digest refuses")

			before := readSitesRecord(t, o)
			require.Contains(t, before.Projects, "helm-a", "precondition: the plan wrote the record")

			// Whatever the Pod would report now must not rescue the apply.
			runner.reports("ghcr.io/helmfile/helmfile@"+roundTripDigestB, nil)

			apply := Target{Project: project, Operation: "apply", TriggeredBy: "comment"}
			result := o.executeOne(context.Background(), checks, testRepo, testPR, 1, apply)

			assert.False(t, result.Success)
			assert.Contains(t, result.Output, "the image this plan ran could not be recorded; re-plan")
			assert.Len(t, runner.createdJobs(), 1, "no Job for the apply")
			assertNoProjectCheck(t, checks, apply)
			assert.Equal(t, before, readSitesRecord(t, o), "the Pull_Request_Record is unchanged")
		})
	}
}
