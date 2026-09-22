package jobs

import (
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivanvc/turnip/internal/config"
)

// testProject builds a Project the way Parse would have left one: Tool is
// derived from Uses, and applyDefaults runs only inside Parse.
//
// Which tool a test picks now decides which Job shape it exercises —
// helmfile is the run-in-image tool, terraform and pulumi are copy-out —
// so tests name the tool they mean rather than relying on a default.
func testProject(tool string) config.Project {
	return config.Project{
		Name:      "web",
		Directory: "infra/web",
		Uses:      tool,
		Tool:      tool,
		With:      map[string]string{"environment": "staging"},
	}
}

func testParams() OperationParams {
	return OperationParams{
		OperationID: "op-123",
		Operation:   "diff",
		RepoURL:     "https://github.com/acme/repo.git",
		CommitSHA:   "abc123",
		BaseRef:     "main",
		GitHubToken: "ghs_token",
		ServerAddr:  "server.turnip.svc:9443",
		ExtraArgs:   []string{"--quiet"},
		PlanData:    []byte("plan-bytes"),
		RunnerImage: "ghcr.io/ivanvc/turnip-runner:test",
	}
}

func envMap(container corev1.Container) map[string]string {
	out := make(map[string]string, len(container.Env))
	for _, e := range container.Env {
		out[e.Name] = e.Value
	}
	return out
}

func mainContainer(t *testing.T, job *batchv1.Job) corev1.Container {
	t.Helper()
	require.Len(t, job.Spec.Template.Spec.Containers, 1)
	return job.Spec.Template.Spec.Containers[0]
}

// initContainerNamed takes require.TestingT rather than *testing.T so the
// property tests can use it too — *rapid.T satisfies testify's TestingT,
// this repo's established convention.
func initContainerNamed(t require.TestingT, name string, job *batchv1.Job) corev1.Container {
	for _, c := range job.Spec.Template.Spec.InitContainers {
		if c.Name == name {
			return c
		}
	}
	require.Failf(t, "initContainer not found", "no initContainer named %q in %v", name, initContainerNames(job))
	return corev1.Container{}
}

func initContainerNames(job *batchv1.Job) []string {
	names := make([]string, 0, len(job.Spec.Template.Spec.InitContainers))
	for _, c := range job.Spec.Template.Spec.InitContainers {
		names = append(names, c.Name)
	}
	return names
}

// Every Job clones in an initContainer running turnip's own image, whatever
// the tool's strategy — that is what lets the container running the tool
// need nothing from its image but the tool.
func TestBuildJob_EveryStrategyClonesInAnInitContainer(t *testing.T) {
	for tool := range toolImages {
		t.Run(tool, func(t *testing.T) {
			job, err := BuildJob(testProject(tool), testParams())
			require.NoError(t, err)

			clone := initContainerNamed(t, "clone", job)
			assert.Equal(t, testParams().RunnerImage, clone.Image, "the clone runs in turnip's image, which has git")
			assert.Equal(t, []string{"clone"}, clone.Args)
			assert.Equal(t, []corev1.VolumeMount{
				{Name: workspaceVolumeName, MountPath: workspaceMountPath},
				{Name: tokenVolumeName, MountPath: tokenMountPath, ReadOnly: true},
			}, clone.VolumeMounts, "it writes the workspace, reads its credential, and touches nothing else")
		})
	}
}

// The GitHub token is set on the clone container and nowhere else. The
// tool's process has no use for an installation token, and under
// run-in-image that process runs in a vendor image executing arbitrary
// tool plugins.
func TestBuildJob_GitHubTokenReachesOnlyTheCloneContainer(t *testing.T) {
	for tool := range toolImages {
		t.Run(tool, func(t *testing.T) {
			job, err := BuildJob(testProject(tool), testParams())
			require.NoError(t, err)

			clone := envMap(initContainerNamed(t, "clone", job))
			assert.Equal(t, "ghs_token", clone["TURNIP_GITHUB_TOKEN"])

			main := envMap(mainContainer(t, job))
			assert.NotContains(t, main, "TURNIP_GITHUB_TOKEN",
				"the tool's process never needs it, and under run-in-image that process is a vendor image")
		})
	}
}

func TestBuildJob_CopyOutShape(t *testing.T) {
	job, err := BuildJob(testProject("terraform"), testParams())
	require.NoError(t, err)

	provision := initContainerNamed(t, "provision-terraform", job)
	assert.Contains(t, provision.Image, "hashicorp/terraform")
	require.Len(t, provision.Command, 3)
	assert.Contains(t, provision.Command[2], "/bin/terraform", "copies the vendor's binary")
	assert.Contains(t, provision.Command[2], toolsMountPath+"/terraform", "onto the shared volume")

	// Provisioning runs before the clone so an unpullable version fails
	// before turnip fetches a repository it is about to discard.
	assert.Equal(t, []string{"provision-terraform", "clone"}, initContainerNames(job))

	main := mainContainer(t, job)
	assert.Equal(t, testParams().RunnerImage, main.Image, "turnip's image executes the copied binary")
	assert.Empty(t, main.Command, "it keeps its own entrypoint")
	assert.Equal(t, toolsMountPath, envMap(main)["TURNIP_TOOLS_DIR"])
	assert.ElementsMatch(t, []corev1.VolumeMount{
		{Name: toolsVolumeName, MountPath: toolsMountPath},
		{Name: workspaceVolumeName, MountPath: workspaceMountPath},
		{Name: tokenVolumeName, MountPath: tokenMountPath, ReadOnly: true},
	}, main.VolumeMounts)
}

func TestBuildJob_RunInImageShape(t *testing.T) {
	job, err := BuildJob(testProject("helmfile"), testParams())
	require.NoError(t, err)

	copyRunner := initContainerNamed(t, "copy-runner", job)
	assert.Equal(t, testParams().RunnerImage, copyRunner.Image)
	require.Len(t, copyRunner.Command, 3)
	assert.Contains(t, copyRunner.Command[2], "/runner "+binMountPath+"/runner")

	assert.NotContains(t, initContainerNames(job), "provision-helmfile",
		"nothing is copied out of the vendor image under this strategy")

	main := mainContainer(t, job)
	assert.Contains(t, main.Image, "ghcr.io/helmfile/helmfile",
		"the vendor's own image is the container that runs the tool")
	assert.Equal(t, []string{binMountPath + "/runner"}, main.Command,
		"overriding the vendor entrypoint with turnip's statically linked binary")
	assert.NotContains(t, envMap(main), "TURNIP_TOOLS_DIR",
		"the tool is already on the vendor image's own PATH")
	assert.ElementsMatch(t, []corev1.VolumeMount{
		{Name: binVolumeName, MountPath: binMountPath},
		{Name: workspaceVolumeName, MountPath: workspaceMountPath},
		{Name: tokenVolumeName, MountPath: tokenMountPath, ReadOnly: true},
	}, main.VolumeMounts)
}

func TestBuildJob_VolumesPerStrategy(t *testing.T) {
	copyOutJob, err := BuildJob(testProject("terraform"), testParams())
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{toolsVolumeName, workspaceVolumeName, tokenVolumeName}, volumeNames(copyOutJob))

	runInImageJob, err := BuildJob(testProject("helmfile"), testParams())
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{binVolumeName, workspaceVolumeName, tokenVolumeName}, volumeNames(runInImageJob))

	// Every volume turnip adds is scratch space, with one exception: the
	// credential, which kubelet mints and rotates and so cannot be an
	// emptyDir.
	for _, job := range []*batchv1.Job{copyOutJob, runInImageJob} {
		for _, v := range job.Spec.Template.Spec.Volumes {
			if v.Name == tokenVolumeName {
				assert.NotNil(t, v.Projected, "the credential is a projection, not scratch space")
				continue
			}
			assert.NotNil(t, v.EmptyDir, "volume %q", v.Name)
		}
	}
}

func volumeNames(job *batchv1.Job) []string {
	names := make([]string, 0, len(job.Spec.Template.Spec.Volumes))
	for _, v := range job.Spec.Template.Spec.Volumes {
		names = append(names, v.Name)
	}
	return names
}

func TestBuildJob_MainContainerHasAllEnvironmentVariables(t *testing.T) {
	project := testProject("helmfile")
	params := testParams()

	job, err := BuildJob(project, params)
	require.NoError(t, err)
	env := envMap(mainContainer(t, job))

	assert.Equal(t, params.ServerAddr, env["TURNIP_SERVER_ADDR"])
	assert.Equal(t, params.OperationID, env["TURNIP_OPERATION_ID"])
	assert.Equal(t, project.Name, env["TURNIP_PROJECT_NAME"])
	assert.Equal(t, project.Directory, env["TURNIP_PROJECT_DIR"])
	assert.Equal(t, project.Tool, env["TURNIP_TOOL"])
	assert.Equal(t, params.Operation, env["TURNIP_OPERATION"])
	assert.Equal(t, params.RepoURL, env["TURNIP_REPO_URL"])
	assert.Equal(t, params.CommitSHA, env["TURNIP_COMMIT_SHA"])
	assert.Equal(t, params.BaseRef, env["TURNIP_BASE_REF"])
	assert.JSONEq(t, `{"environment":"staging"}`, env["TURNIP_TOOL_CONFIG"])
	assert.JSONEq(t, `["--quiet"]`, env["TURNIP_EXTRA_ARGS"])
	assert.NotEmpty(t, env["TURNIP_PLAN_DATA"])
	assert.Equal(t, workspaceMountPath, env["TURNIP_WORKSPACE_DIR"])
	assert.NotContains(t, env, "PATH", "PATH must be composed by the Runner at startup, not overridden by the Job spec")
}

// The literal paths are pinned, not just compared to their own constants:
// committed configuration in a consumer repository may reference the
// workspace path, so changing it is a breaking change that should fail
// here rather than silently in someone's helmfile.
func TestBuildJob_MountPathsLiveUnderTurnipRoot(t *testing.T) {
	assert.Equal(t, "/turnip/tools", toolsMountPath)
	assert.Equal(t, "/turnip/bin", binMountPath)
	assert.Equal(t, "/turnip/src", workspaceMountPath)
}

func TestBuildJob_RestartPolicyAndTTL(t *testing.T) {
	job, err := BuildJob(testProject("helmfile"), testParams())
	require.NoError(t, err)

	assert.Equal(t, corev1.RestartPolicyNever, job.Spec.Template.Spec.RestartPolicy)
	require.NotNil(t, job.Spec.TTLSecondsAfterFinished)
	assert.Equal(t, jobTTLSeconds, *job.Spec.TTLSecondsAfterFinished)
	require.NotNil(t, job.Spec.BackoffLimit)
	assert.Equal(t, int32(0), *job.Spec.BackoffLimit)
}

// TURNIP_TOOL_CONFIG used to carry the whole `config` map, including two
// keys no Plugin ever read — the tool version, resolved by this package,
// and the ServiceAccount, resolved by the orchestrator. It now carries
// `with` alone, and both of those reach the Job spec through their own
// fields instead.
func TestBuildJob_ToolConfigCarriesOnlyWith(t *testing.T) {
	project := testProject("helmfile")
	project.ToolVersion = "1.7.4"
	project.Runner.ServiceAccount = "turnip-runner"

	job, err := BuildJob(project, testParams())
	require.NoError(t, err)

	main := mainContainer(t, job)
	env := envMap(main)
	assert.JSONEq(t, `{"environment":"staging"}`, env["TURNIP_TOOL_CONFIG"])
	assert.NotContains(t, env["TURNIP_TOOL_CONFIG"], "version",
		"the tool version is this package's to resolve, not a Plugin's to read")
	assert.NotContains(t, env["TURNIP_TOOL_CONFIG"], "serviceAccount",
		"nor is the Pod's identity")

	// Under run-in-image the pinned version selects the *main* container's
	// image, since that is where the tool lives.
	assert.Contains(t, main.Image, "1.7.4")
}

// A Project's env reaches the tool as ordinary Job-spec variables: no
// transport, no Runner-side parsing.
func TestBuildJob_ProjectEnvOnRunnerContainer(t *testing.T) {
	project := testProject("helmfile")
	project.Runner.Env = map[string]string{"AWS_PROFILE": "prod", "HELM_EXPERIMENTAL": "true"}

	job, err := BuildJob(project, testParams())
	require.NoError(t, err)

	env := envMap(mainContainer(t, job))
	assert.Equal(t, "prod", env["AWS_PROFILE"])
	assert.Equal(t, "true", env["HELM_EXPERIMENTAL"])

	assert.NotContains(t, envMap(initContainerNamed(t, "clone", job)), "AWS_PROFILE",
		"a Project's environment configures the tool, not the clone")
}

// Kubernetes expands $(VAR) inside env values against the variables
// declared earlier in the same list, which would silently rewrite a value
// the Project meant literally. Doubling every $ is the documented escape,
// and an escaped reference is left alone whether or not the name exists.
func TestBuildJob_ProjectEnvValuesAreEscapedAgainstExpansion(t *testing.T) {
	project := testProject("helmfile")
	project.Runner.Env = map[string]string{
		"EXPANDABLE": "$(TURNIP_TOOL) and $HOME",
		"PLAIN":      "no dollars here",
	}

	job, err := BuildJob(project, testParams())
	require.NoError(t, err)

	env := envMap(mainContainer(t, job))
	assert.Equal(t, "$$(TURNIP_TOOL) and $$HOME", env["EXPANDABLE"])
	assert.Equal(t, "no dollars here", env["PLAIN"], "a value with no $ is byte-identical")
}

// Map iteration order is random; a Job spec that reorders between builds
// is noise in every diff of it.
func TestBuildJob_ProjectEnvInSortedKeyOrder(t *testing.T) {
	project := testProject("helmfile")
	project.Runner.Env = map[string]string{"CHARLIE": "3", "ALPHA": "1", "BRAVO": "2"}

	job, err := BuildJob(project, testParams())
	require.NoError(t, err)

	var got []string
	for _, e := range mainContainer(t, job).Env {
		if _, ok := project.Runner.Env[e.Name]; ok {
			got = append(got, e.Name)
		}
	}
	assert.Equal(t, []string{"ALPHA", "BRAVO", "CHARLIE"}, got)
}

func TestBuildJob_ServiceAccountFromParams(t *testing.T) {
	params := testParams()
	params.ServiceAccount = "turnip-runner"

	job, err := BuildJob(testProject("helmfile"), params)
	require.NoError(t, err)
	assert.Equal(t, "turnip-runner", job.Spec.Template.Spec.ServiceAccountName)
}

func TestBuildJob_EmptyServiceAccountLeavesPodOnNamespaceDefault(t *testing.T) {
	params := testParams()
	params.ServiceAccount = ""

	job, err := BuildJob(testProject("helmfile"), params)
	require.NoError(t, err)
	assert.Empty(t, job.Spec.Template.Spec.ServiceAccountName)
}

// Under copy-out the Runner image is the main container; under
// run-in-image it is the image the runner binary is copied out of. Both
// roles are the same parameter.
func TestBuildJob_RunnerImageFromParams(t *testing.T) {
	params := testParams()
	params.RunnerImage = "ghcr.io/ivanvc/turnip-runner:v1.2.3"

	copyOutJob, err := BuildJob(testProject("terraform"), params)
	require.NoError(t, err)
	assert.Equal(t, params.RunnerImage, mainContainer(t, copyOutJob).Image)

	runInImageJob, err := BuildJob(testProject("helmfile"), params)
	require.NoError(t, err)
	assert.Equal(t, params.RunnerImage, initContainerNamed(t, "copy-runner", runInImageJob).Image)
	assert.Equal(t, params.RunnerImage, initContainerNamed(t, "clone", runInImageJob).Image)
	assert.NotEqual(t, params.RunnerImage, mainContainer(t, runInImageJob).Image,
		"the main container is the vendor's image here")
}

func TestBuildJob_UnrecognizedVersionReturnsErrorAndNoJob(t *testing.T) {
	project := testProject("terraform")
	project.ToolVersion = "not-a-real-version"

	job, err := BuildJob(project, testParams())
	require.Error(t, err)
	assert.Nil(t, job)
	var unrecognized *UnrecognizedVersionError
	require.ErrorAs(t, err, &unrecognized)
}

func TestBuildJob_ExplicitVersionSelectsMatchingImage(t *testing.T) {
	project := testProject("terraform")
	wantVersion := toolImages["terraform"].versions[1]
	project.ToolVersion = wantVersion

	job, err := BuildJob(project, testParams())
	require.NoError(t, err)
	assert.Contains(t, initContainerNamed(t, "provision-terraform", job).Image, wantVersion)
}

// A Project must be able to request any well-formed version its tool's
// vendor actually publishes, not just one of turnip's own hardcoded
// examples — otherwise adopting a new release still requires a turnip code
// change, exactly the bundled-binary bottleneck the initContainer design
// exists to avoid.
func TestBuildJob_VersionNotInExampleListStillBuilds(t *testing.T) {
	project := testProject("terraform")
	const notInList = "9.9.9"
	require.NotContains(t, toolImages["terraform"].versions, notInList)
	project.ToolVersion = notInList

	job, err := BuildJob(project, testParams())
	require.NoError(t, err)
	assert.Contains(t, initContainerNamed(t, "provision-terraform", job).Image, notInList)
}

// The Runner's credential is a ServiceAccountToken projection scoped to
// turnip's own audience. The audience is the load-bearing part: a token
// minted for the API server's audience would be replayable by whoever
// received it *as* the Runner's ServiceAccount, which is exactly what
// scoping prevents.
func TestBuildJob_ProjectsAnAudienceScopedToken(t *testing.T) {
	for tool := range toolImages {
		t.Run(tool, func(t *testing.T) {
			job, err := BuildJob(testProject(tool), testParams())
			require.NoError(t, err)

			var projection *corev1.ServiceAccountTokenProjection
			for _, v := range job.Spec.Template.Spec.Volumes {
				if v.Name == tokenVolumeName {
					require.NotNil(t, v.Projected)
					require.Len(t, v.Projected.Sources, 1)
					projection = v.Projected.Sources[0].ServiceAccountToken
				}
			}
			require.NotNil(t, projection, "no projected token volume")

			assert.Equal(t, TokenAudience, projection.Audience,
				"a token for the API server's own audience would be replayable as the Pod's ServiceAccount")
			require.NotNil(t, projection.ExpirationSeconds)
			assert.Equal(t, tokenExpirationSeconds, *projection.ExpirationSeconds)
			assert.Equal(t, tokenFileName, projection.Path)
		})
	}
}

// The credential rides a field of the Pod spec turnip already writes, so
// a Project choosing its own ServiceAccount (Slice 13) changes nothing
// about how the Runner authenticates. This is the property to protect:
// any rule that makes the projection depend on the account name has
// reintroduced the coupling.
func TestBuildJob_TokenProjectionIsIndependentOfServiceAccount(t *testing.T) {
	withDefault := testParams()
	withDefault.ServiceAccount = ""
	chosen := testParams()
	chosen.ServiceAccount = "project-chosen"

	a, err := BuildJob(testProject("helmfile"), withDefault)
	require.NoError(t, err)
	b, err := BuildJob(testProject("helmfile"), chosen)
	require.NoError(t, err)

	assert.Equal(t, tokenVolumeOf(t, a), tokenVolumeOf(t, b),
		"the identity that matters is the Pod's, not the account's")
	assert.Equal(t, "project-chosen", b.Spec.Template.Spec.ServiceAccountName)
}

func tokenVolumeOf(t require.TestingT, job *batchv1.Job) corev1.Volume {
	for _, v := range job.Spec.Template.Spec.Volumes {
		if v.Name == tokenVolumeName {
			return v
		}
	}
	require.Fail(t, "no projected token volume")
	return corev1.Volume{}
}

// Both containers that reach the Server need to prove who they are: the
// clone reports its own failures over the same authenticated stream the
// tool's container later uses, so the token path is base environment
// rather than per-container.
func TestBuildJob_TokenPathReachesBothContainers(t *testing.T) {
	for tool := range toolImages {
		t.Run(tool, func(t *testing.T) {
			job, err := BuildJob(testProject(tool), testParams())
			require.NoError(t, err)

			want := tokenMountPath + "/" + tokenFileName
			assert.Equal(t, want, envMap(initContainerNamed(t, "clone", job))["TURNIP_TOKEN_FILE"])
			assert.Equal(t, want, envMap(mainContainer(t, job))["TURNIP_TOKEN_FILE"])
		})
	}
}
