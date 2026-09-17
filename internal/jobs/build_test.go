package jobs

import (
	"testing"

	corev1 "k8s.io/api/core/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivanvc/turnip/internal/config"
)

func testProject(tool string) config.Project {
	return config.Project{
		Name:      "web",
		Directory: "infra/web",
		Tool:      tool,
		Config:    map[string]string{"environment": "staging"},
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

func TestBuildJob_OneInitContainerWithVendorImageAndCopyCommand(t *testing.T) {
	for tool, ti := range toolImages {
		t.Run(tool, func(t *testing.T) {
			job, err := BuildJob(testProject(tool), testParams())
			require.NoError(t, err)

			initContainers := job.Spec.Template.Spec.InitContainers
			require.Len(t, initContainers, 1)
			assert.Contains(t, initContainers[0].Image, ti.image[:len(ti.image)-2]) // template minus "%s"
			require.Len(t, initContainers[0].Command, 3)
			assert.Contains(t, initContainers[0].Command[2], ti.binaryPath)
			assert.Contains(t, initContainers[0].Command[2], toolsMountPath+"/"+tool)
		})
	}
}

func TestBuildJob_MainContainerHasAllEnvironmentVariables(t *testing.T) {
	project := testProject("helmfile")
	params := testParams()

	job, err := BuildJob(project, params)
	require.NoError(t, err)

	require.Len(t, job.Spec.Template.Spec.Containers, 1)
	env := envMap(job.Spec.Template.Spec.Containers[0])

	assert.Equal(t, params.ServerAddr, env["TURNIP_SERVER_ADDR"])
	assert.Equal(t, params.OperationID, env["TURNIP_OPERATION_ID"])
	assert.Equal(t, project.Name, env["TURNIP_PROJECT_NAME"])
	assert.Equal(t, project.Directory, env["TURNIP_PROJECT_DIR"])
	assert.Equal(t, project.Tool, env["TURNIP_TOOL"])
	assert.Equal(t, params.Operation, env["TURNIP_OPERATION"])
	assert.Equal(t, params.RepoURL, env["TURNIP_REPO_URL"])
	assert.Equal(t, params.CommitSHA, env["TURNIP_COMMIT_SHA"])
	assert.Equal(t, params.BaseRef, env["TURNIP_BASE_REF"])
	assert.Equal(t, params.GitHubToken, env["TURNIP_GITHUB_TOKEN"])
	assert.JSONEq(t, `{"environment":"staging"}`, env["TURNIP_TOOL_CONFIG"])
	assert.JSONEq(t, `["--quiet"]`, env["TURNIP_EXTRA_ARGS"])
	assert.NotEmpty(t, env["TURNIP_PLAN_DATA"])
	assert.Equal(t, toolsMountPath, env["TURNIP_TOOLS_DIR"])
	assert.Equal(t, workspaceMountPath, env["TURNIP_WORKSPACE_DIR"])
	assert.NotContains(t, env, "PATH", "PATH must be composed by the Runner at startup, not overridden by the Job spec")
}

// The literal paths are pinned, not just compared to their own
// constants: committed configuration in a consumer repository may
// reference the workspace path, so changing it is a breaking change that
// should fail here rather than silently in someone's helmfile.
func TestBuildJob_MountPathsLiveUnderTurnipRoot(t *testing.T) {
	assert.Equal(t, "/turnip/tools", toolsMountPath)
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

func TestBuildJob_ToolsAndWorkspaceVolumes(t *testing.T) {
	job, err := BuildJob(testProject("helmfile"), testParams())
	require.NoError(t, err)
	spec := job.Spec.Template.Spec

	require.Len(t, spec.Volumes, 2)
	assert.Equal(t, toolsVolumeName, spec.Volumes[0].Name)
	require.NotNil(t, spec.Volumes[0].EmptyDir)
	assert.Equal(t, workspaceVolumeName, spec.Volumes[1].Name)
	require.NotNil(t, spec.Volumes[1].EmptyDir)

	toolsMount := corev1.VolumeMount{Name: toolsVolumeName, MountPath: toolsMountPath}
	workspaceMount := corev1.VolumeMount{Name: workspaceVolumeName, MountPath: workspaceMountPath}

	// The initContainer copies one binary; the repository is none of its
	// business, so it mounts tools alone — asserted as an exact list, since
	// "contains tools" would pass even if the workspace leaked into it.
	assert.Equal(t, []corev1.VolumeMount{toolsMount}, spec.InitContainers[0].VolumeMounts)
	assert.ElementsMatch(t, []corev1.VolumeMount{toolsMount, workspaceMount}, spec.Containers[0].VolumeMounts)
}

// A Project's env reaches the tool as ordinary Job-spec variables
// (Decision 3): no transport, no Runner-side parsing.
func TestBuildJob_ProjectEnvOnRunnerContainer(t *testing.T) {
	project := testProject("helmfile")
	project.Env = map[string]string{"AWS_PROFILE": "prod", "HELM_EXPERIMENTAL": "true"}

	job, err := BuildJob(project, testParams())
	require.NoError(t, err)

	env := envMap(job.Spec.Template.Spec.Containers[0])
	assert.Equal(t, "prod", env["AWS_PROFILE"])
	assert.Equal(t, "true", env["HELM_EXPERIMENTAL"])
}

// Kubernetes expands $(VAR) inside env values against the variables
// declared earlier in the same list, which would silently rewrite a value
// the Project meant literally. Doubling every $ is the documented escape,
// and an escaped reference is left alone whether or not the name exists.
func TestBuildJob_ProjectEnvValuesAreEscapedAgainstExpansion(t *testing.T) {
	project := testProject("helmfile")
	project.Env = map[string]string{
		"EXPANDABLE": "$(TURNIP_TOOL) and $HOME",
		"PLAIN":      "no dollars here",
	}

	job, err := BuildJob(project, testParams())
	require.NoError(t, err)

	env := envMap(job.Spec.Template.Spec.Containers[0])
	assert.Equal(t, "$$(TURNIP_TOOL) and $$HOME", env["EXPANDABLE"])
	assert.Equal(t, "no dollars here", env["PLAIN"], "a value with no $ is byte-identical")
}

// Map iteration order is random; a Job spec that reorders between builds
// is noise in every diff of it.
func TestBuildJob_ProjectEnvInSortedKeyOrder(t *testing.T) {
	project := testProject("helmfile")
	project.Env = map[string]string{"CHARLIE": "3", "ALPHA": "1", "BRAVO": "2"}

	job, err := BuildJob(project, testParams())
	require.NoError(t, err)

	var got []string
	for _, e := range job.Spec.Template.Spec.Containers[0].Env {
		if _, ok := project.Env[e.Name]; ok {
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

func TestBuildJob_RunnerImageFromParams(t *testing.T) {
	params := testParams()
	params.RunnerImage = "ghcr.io/ivanvc/turnip-runner:v1.2.3"

	job, err := BuildJob(testProject("helmfile"), params)
	require.NoError(t, err)

	require.Len(t, job.Spec.Template.Spec.Containers, 1)
	assert.Equal(t, params.RunnerImage, job.Spec.Template.Spec.Containers[0].Image)
}

func TestBuildJob_UnrecognizedVersionReturnsErrorAndNoJob(t *testing.T) {
	project := testProject("terraform")
	project.Config = map[string]string{"version": "not-a-real-version"}

	job, err := BuildJob(project, testParams())
	require.Error(t, err)
	assert.Nil(t, job)
	var unrecognized *UnrecognizedVersionError
	require.ErrorAs(t, err, &unrecognized)
}

func TestBuildJob_ExplicitVersionSelectsMatchingImage(t *testing.T) {
	project := testProject("terraform")
	wantVersion := toolImages["terraform"].versions[1]
	project.Config = map[string]string{"version": wantVersion}

	job, err := BuildJob(project, testParams())
	require.NoError(t, err)
	assert.Contains(t, job.Spec.Template.Spec.InitContainers[0].Image, wantVersion)
}

// TestBuildJob_VersionNotInExampleListStillBuilds is the fix for a real
// gap: a Project must be able to request any well-formed version its
// tool's vendor actually publishes, not just one of turnip's own
// hardcoded examples — otherwise adopting a new release still requires a
// turnip code change, exactly the bundled-binary bottleneck Requirement
// 8's initContainer design exists to avoid.
func TestBuildJob_VersionNotInExampleListStillBuilds(t *testing.T) {
	project := testProject("terraform")
	const notInList = "9.9.9"
	require.NotContains(t, toolImages["terraform"].versions, notInList)
	project.Config = map[string]string{"version": notInList}

	job, err := BuildJob(project, testParams())
	require.NoError(t, err)
	assert.Contains(t, job.Spec.Template.Spec.InitContainers[0].Image, notInList)
}
