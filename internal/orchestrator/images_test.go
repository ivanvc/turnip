package orchestrator

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"

	"github.com/ivanvc/turnip/internal/config"
	"github.com/ivanvc/turnip/internal/github"
	"github.com/ivanvc/turnip/internal/jobs"
	"github.com/ivanvc/turnip/internal/lock"
	"github.com/ivanvc/turnip/internal/provisioning"
	"github.com/ivanvc/turnip/internal/rpc"
)

// Each Plugin's Alias is formed from its name, its Provisioning image and
// its ImageTags globs, so the image is stated once.
func TestNewCatalog_FormsAliasesFromEachPlugin(t *testing.T) {
	catalog := NewCatalog(NewPluginRegistry(), nil)

	assert.Equal(t, []config.Entry{
		{Tool: "helmfile", Image: "ghcr.io/helmfile/helmfile", Glob: "v*.*.*"},
		{Tool: "helmfile", Image: "ghcr.io/helmfile/helmfile", Glob: "sha256:*"},
	}, catalog.Aliases["helmfile"])
	assert.Empty(t, catalog.Allowed)
}

func TestConfigFromEnv_AllowedImagesDefaultsToAliasesOnly(t *testing.T) {
	cfg, err := ConfigFromEnv(envMap(nil), NewPluginRegistry())
	require.NoError(t, err)
	assert.Empty(t, cfg.Catalog.Allowed)
	assert.Equal(t, NewCatalog(NewPluginRegistry(), nil), cfg.Catalog)
}

func TestConfigFromEnv_AllowedImagesParsesList(t *testing.T) {
	cfg, err := ConfigFromEnv(envMap(map[string]string{
		"TURNIP_ALLOWED_IMAGES": " helmfile:ghcr.io/org/helmfile-aws@* , helmfile:ghcr.io/helmfile/helmfile@latest,",
	}), NewPluginRegistry())
	require.NoError(t, err)
	assert.Equal(t, []config.Entry{
		{Tool: "helmfile", Image: "ghcr.io/org/helmfile-aws", Glob: "*"},
		{Tool: "helmfile", Image: "ghcr.io/helmfile/helmfile", Glob: "latest"},
	}, cfg.Catalog.Allowed, "surrounding whitespace and empty fields are ignored")
	assert.Len(t, cfg.Catalog.Aliases["helmfile"], 2, "the Aliases stay as the Plugin defines them")
}

// Every offending Entry is named in the one error, so an operator fixes
// them all in one restart.
func TestParseAllowedImages_NamesEveryOffendingEntry(t *testing.T) {
	_, err := ConfigFromEnv(envMap(map[string]string{
		"TURNIP_ALLOWED_IMAGES": "terraform:docker.io/hashicorp/terraform@*," +
			"registry.local:5000/org/helmfile@*," +
			"helmfile:helmfile-aws@*," +
			"helmfile:ghcr.io/org/helmfile-aws:1.7.4@*," +
			"helmfile:ghcr.io/org/helmfile-aws@sha256:abc@*," +
			"helmfile:ghcr.io/org/fine@*",
	}), NewPluginRegistry())
	require.Error(t, err)

	var allowedErr *AllowedImagesError
	require.ErrorAs(t, err, &allowedErr)
	assert.Len(t, allowedErr.Problems, 5, "one problem per offending Entry: %v", allowedErr.Problems)
	assert.Contains(t, err.Error(), "TURNIP_ALLOWED_IMAGES")
	for _, want := range []string{
		`"terraform:docker.io/hashicorp/terraform@*"`,      // not a registered tool
		`"registry.local:5000/org/helmfile@*"`,             // a registry port is not a tool
		`"helmfile:helmfile-aws@*"`,                        // unqualified
		`"helmfile:ghcr.io/org/helmfile-aws:1.7.4@*"`,      // a tag of its own
		`"helmfile:ghcr.io/org/helmfile-aws@sha256:abc@*"`, // a digest of its own
	} {
		assert.Contains(t, err.Error(), want)
	}
	assert.NotContains(t, err.Error(), "ghcr.io/org/fine", "a valid Entry is not named")
}

// An image listed for two tools leaves turnip unable to tell which Plugin
// drives it, whether the two Entries are the operator's own or one is an
// Alias the operator cannot change.
func TestParseAllowedImages_RefusesAnImageListedForTwoTools(t *testing.T) {
	t.Run("across an Alias and the Access_List", func(t *testing.T) {
		_, err := parseAllowedImages("pulumi:ghcr.io/helmfile/helmfile@latest", testRegistry())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "ghcr.io/helmfile/helmfile")
		assert.Contains(t, err.Error(), "helmfile:ghcr.io/helmfile/helmfile@v*.*.* (built-in alias)")
		assert.Contains(t, err.Error(), "pulumi:ghcr.io/helmfile/helmfile@latest")
	})

	t.Run("within the Access_List", func(t *testing.T) {
		_, err := parseAllowedImages("helmfile:ghcr.io/org/iac@*,pulumi:ghcr.io/org/iac@latest", testRegistry())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "helmfile:ghcr.io/org/iac@*")
		assert.Contains(t, err.Error(), "pulumi:ghcr.io/org/iac@latest")
	})

	t.Run("reported with the other offending Entries", func(t *testing.T) {
		_, err := parseAllowedImages("helmfile:unqualified@*,pulumi:ghcr.io/helmfile/helmfile@*", testRegistry())
		var allowedErr *AllowedImagesError
		require.ErrorAs(t, err, &allowedErr)
		assert.Len(t, allowedErr.Problems, 2)
	})

	t.Run("the same tool may repeat an image", func(t *testing.T) {
		catalog, err := parseAllowedImages("helmfile:ghcr.io/helmfile/helmfile@latest,helmfile:ghcr.io/helmfile/helmfile@*", testRegistry())
		require.NoError(t, err)
		assert.Len(t, catalog.Allowed, 2)
	})
}

// configFileClient serves one turnip.yaml at the first path fetchConfig
// tries.
type configFileClient struct {
	github.GitHubClient
	data []byte
}

func (c configFileClient) GetFile(_ context.Context, _, _, path, _ string) ([]byte, error) {
	if path == configFilePaths[0] {
		return c.data, nil
	}
	return nil, github.ErrFileNotFound
}

// The Catalog reaches config.Parse: an image the Access_List allows
// resolves to the tool its Entry declares, and one it does not is a
// validation error of the file.
func TestFetchConfig_ResolvesUsesAgainstTheCatalog(t *testing.T) {
	catalog, err := parseAllowedImages("helmfile:ghcr.io/org/helmfile-aws@*", NewPluginRegistry())
	require.NoError(t, err)

	cfg, err := fetchConfig(context.Background(), configFileClient{data: []byte(`schemaVersion: v1alpha3
projects:
  - directory: web
    uses: ghcr.io/org/helmfile-aws@latest
`)}, "o", "r", "sha", catalog)
	require.NoError(t, err)
	require.Len(t, cfg.Projects, 1)
	assert.Equal(t, "helmfile", cfg.Projects[0].Tool)
	assert.Equal(t, "ghcr.io/org/helmfile-aws", cfg.Projects[0].Image)
	assert.Equal(t, "latest", cfg.Projects[0].ToolVersion)

	_, err = fetchConfig(context.Background(), configFileClient{data: []byte(`schemaVersion: v1alpha3
projects:
  - directory: web
    uses: ghcr.io/org/other@latest
`)}, "o", "r", "sha", catalog)
	var validationErrs config.ValidationErrors
	require.ErrorAs(t, err, &validationErrs)
	assert.Contains(t, err.Error(), "TURNIP_ALLOWED_IMAGES")
}

func TestJobImage(t *testing.T) {
	const digest = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	project := config.Project{Image: "ghcr.io/org/helmfile-aws", ToolVersion: "latest"}

	ref, pullAlways := jobImage(project, true, "")
	assert.Equal(t, "ghcr.io/org/helmfile-aws:latest", ref)
	assert.True(t, pullAlways, "a plan sees what the tag means now")

	project.ToolVersion = digest
	ref, pullAlways = jobImage(project, true, "")
	assert.Equal(t, "ghcr.io/org/helmfile-aws@"+digest, ref)
	assert.False(t, pullAlways, "a digest cannot change, so the node's cache serves it")

	project.ToolVersion = "latest"
	const recorded = "sha256:fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210"
	ref, pullAlways = jobImage(project, false, recorded)
	assert.Equal(t, "ghcr.io/org/helmfile-aws@"+recorded, ref, "a Mutating_Operation runs the digest its plan recorded, not the tag")
	assert.False(t, pullAlways)
}

func TestImageDigest(t *testing.T) {
	const digest = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	cases := map[string]string{
		"ghcr.io/helmfile/helmfile@" + digest:                   digest,
		"docker-pullable://ghcr.io/helmfile/helmfile@" + digest: digest,
		"docker.io/library/alpine@" + digest:                    digest,
		digest:                                                  "", // a local image ID names no manifest
		"":                                                      "",
		"ghcr.io/helmfile/helmfile@sha512:" + digest[len("sha256:"):]: "",
		"ghcr.io/helmfile/helmfile@sha256:abc":                        "",
	}
	for imageID, want := range cases {
		assert.Equal(t, want, imageDigest(imageID), "imageID %q", imageID)
	}
}

// mainContainer returns the Job's container named "runner", which runs the
// tool's image under RunInImage.
func mainContainer(t *testing.T, o *Orchestrator, target Target, jobsClient *fakeJobCreator) corev1.Container {
	t.Helper()
	result := o.executeOne(context.Background(), &fakeExecuteClient{}, testRepo, testPR, 1, target)
	require.True(t, result.Success, "the Job must be built and dispatched: %s", result.Output)
	job := jobsClient.lastCreatedJob()
	require.NotNil(t, job)
	require.Len(t, job.Spec.Template.Spec.Containers, 1)
	return job.Spec.Template.Spec.Containers[0]
}

func TestExecuteOne_PlanRunsTheTagPulledEveryTime(t *testing.T) {
	jobsClient := &fakeJobCreator{t: t, result: github.ProjectResult{Success: true}}
	o, _ := testOrchestrator(t, &fakeLockManager{}, jobsClient)

	c := mainContainer(t, o, testHelmfileTarget(), jobsClient)
	assert.Equal(t, "ghcr.io/helmfile/helmfile:v1.7.4", c.Image)
	assert.Equal(t, corev1.PullAlways, c.ImagePullPolicy)
}

func TestExecuteOne_PlanNamingADigestUsesTheCache(t *testing.T) {
	jobsClient := &fakeJobCreator{t: t, result: github.ProjectResult{Success: true}}
	o, _ := testOrchestrator(t, &fakeLockManager{}, jobsClient)
	target := testHelmfileTarget()
	target.Project.ToolVersion = testImageDigest

	c := mainContainer(t, o, target, jobsClient)
	assert.Equal(t, "ghcr.io/helmfile/helmfile@"+testImageDigest, c.Image)
	assert.Equal(t, corev1.PullIfNotPresent, c.ImagePullPolicy)
}

func TestExecuteOne_MutatingOperationRunsTheRecordedDigest(t *testing.T) {
	locks := &fakeLockManager{getPlanFunc: func(context.Context, string, int) (lock.PlanRecord, error) {
		return lock.PlanRecord{ImageDigest: testImageDigest}, nil
	}}
	jobsClient := &fakeJobCreator{t: t, result: github.ProjectResult{Success: true}}
	o, _ := testOrchestrator(t, locks, jobsClient)
	target := testHelmfileTarget()
	target.Operation = "apply"

	c := mainContainer(t, o, target, jobsClient)
	assert.Equal(t, "ghcr.io/helmfile/helmfile@"+testImageDigest, c.Image, "the tag is not resolved again")
	assert.Equal(t, corev1.PullIfNotPresent, c.ImagePullPolicy)
}

// A custom image is chosen for what it carries beside the tool, which only
// running inside it provides, so it runs RunInImage even when its tool's
// Plugin copies the binary out of its own image.
func TestExecuteOne_CustomImageRunsInImageWhateverThePluginStrategy(t *testing.T) {
	jobsClient := &fakeJobCreator{t: t, result: github.ProjectResult{Success: true}}
	o, _ := testOrchestrator(t, &fakeLockManager{}, jobsClient)
	o.plugins["helmfile"] = provisionedPlugin{
		fakePlugin: o.plugins["helmfile"].(*fakePlugin),
		spec:       provisioning.Spec{Strategy: provisioning.CopyOut, Image: "ghcr.io/helmfile/helmfile", BinaryPath: "/usr/local/bin/helmfile"},
	}
	target := testHelmfileTarget()
	target.Project.Image = "ghcr.io/org/helmfile-aws"
	target.Project.ToolVersion = "latest"

	c := mainContainer(t, o, target, jobsClient)
	assert.Equal(t, "ghcr.io/org/helmfile-aws:latest", c.Image)
	for _, init := range jobsClient.lastCreatedJob().Spec.Template.Spec.InitContainers {
		assert.NotEqual(t, "provision-helmfile", init.Name, "RunInImage copies no binary out")
	}
}

// planResultOrchestrator is a result-handling Orchestrator whose fake Pod
// reports imageID, capturing the PlanRecord the plan's Lock event stores.
func planResultOrchestrator(t *testing.T, status func() (*jobs.JobStatus, error)) (*Orchestrator, *lock.PlanRecord) {
	t.Helper()
	stored := &lock.PlanRecord{}
	locks := &fakeLockManager{applyFunc: func(_ context.Context, _ string, _ int, ev lock.Event, plan *lock.PlanRecord) (lock.Transition, error) {
		if plan != nil {
			*stored = *plan
		}
		return lock.Transition{From: lock.StatePlanning, To: lock.StatePlanReady}, nil
	}}
	o, _ := testResultOrchestrator(t, locks)
	o.jobs = &fakeJobCreator{t: t, statusFn: func(_ context.Context, jobName string) (*jobs.JobStatus, error) {
		assert.Equal(t, "turnip-runner-op-plan", jobName)
		return status()
	}}
	createTestRecord(t, o, "op-plan", "diff")
	require.NoError(t, o.records.SetJobName(context.Background(), "op-plan", "turnip-runner-op-plan"))
	return o, stored
}

func TestHandleResult_RecordsThePlanPodsImageDigest(t *testing.T) {
	o, stored := planResultOrchestrator(t, func() (*jobs.JobStatus, error) {
		return &jobs.JobStatus{JobFound: true, ImageID: "ghcr.io/helmfile/helmfile@" + testImageDigest}, nil
	})

	publishedResult(t, o, "op-plan", rpc.OperationResult{Success: true, Changes: rpc.ChangeSummary{Add: 1}})

	assert.Equal(t, testImageDigest, stored.ImageDigest)
}

// A digest that cannot be read leaves the plan applicable but records
// none, which its Mutating_Operation then refuses.
func TestHandleResult_RecordsNoDigestWhenItCannotBeRead(t *testing.T) {
	cases := map[string]func() (*jobs.JobStatus, error){
		"status call fails": func() (*jobs.JobStatus, error) { return nil, errors.New("apiserver unavailable") },
		"job gone":          func() (*jobs.JobStatus, error) { return &jobs.JobStatus{JobFound: false}, nil },
		"no image id yet":   func() (*jobs.JobStatus, error) { return &jobs.JobStatus{JobFound: true}, nil },
		"a local image id": func() (*jobs.JobStatus, error) {
			return &jobs.JobStatus{JobFound: true, ImageID: testImageDigest}, nil
		},
	}
	for name, status := range cases {
		t.Run(name, func(t *testing.T) {
			o, stored := planResultOrchestrator(t, status)

			got := publishedResult(t, o, "op-plan", rpc.OperationResult{Success: true, Changes: rpc.ChangeSummary{Add: 1}})

			assert.True(t, got.Success, "the plan still succeeds")
			assert.Empty(t, stored.ImageDigest)
			assert.Equal(t, 1, stored.Summary.Add, "the rest of the plan is still recorded")
		})
	}
}

func TestTimeoutDiagnostic_ImagePullFailure(t *testing.T) {
	for _, reason := range []string{"ErrImagePull", "ImagePullBackOff"} {
		status := &jobs.JobStatus{JobFound: true, PodPhase: "Pending", PodReason: reason, ToolWaiting: true}

		mutating := timeoutDiagnostic("job-1", status, true)
		assert.Contains(t, mutating, reason)
		assert.Contains(t, mutating, "may no longer exist in the registry")
		assert.Contains(t, mutating, "re-plan")

		plan := timeoutDiagnostic("job-1", status, false)
		assert.Equal(t, "Job job-1: container stuck ("+reason+")", plan, "a plan pulled a tag, not a recorded digest")

		runnerImage := &jobs.JobStatus{JobFound: true, PodPhase: "Pending", PodReason: reason, ToolWaiting: false}
		assert.Equal(t, "Job job-1: container stuck ("+reason+")", timeoutDiagnostic("job-1", runnerImage, true),
			"a Runner image that cannot be pulled is not the recorded digest's doing")
	}

	other := timeoutDiagnostic("job-1", &jobs.JobStatus{JobFound: true, PodReason: "CreateContainerConfigError"}, true)
	assert.Equal(t, "Job job-1: container stuck (CreateContainerConfigError)", other)
}

func TestSweepOnce_MutatingImagePullFailureSaysToReplan(t *testing.T) {
	jobsClient := &fakeJobCreator{t: t, statusFn: func(ctx context.Context, jobName string) (*jobs.JobStatus, error) {
		return &jobs.JobStatus{JobFound: true, PodPhase: "Pending", PodReason: "ImagePullBackOff", ToolWaiting: true}, nil
	}}
	o, client := testSweepOrchestrator(t, jobsClient)
	rec := sweepTestRecord("op-apply", time.Now().Add(-time.Minute), false)
	rec.Operation = "apply"
	require.NoError(t, o.records.Create(context.Background(), rec))

	o.sweepOnce(context.Background())

	assert.Contains(t, client.updatedCheckRun.Text, "may no longer exist in the registry")
}
