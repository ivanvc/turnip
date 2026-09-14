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
			assert.Contains(t, initContainers[0].Command[2], "/tools/"+tool)
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
	assert.NotContains(t, env, "PATH", "PATH must be composed by the Runner at startup, not overridden by the Job spec")
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

func TestBuildJob_SharedVolumeMountedByBothContainers(t *testing.T) {
	job, err := BuildJob(testProject("helmfile"), testParams())
	require.NoError(t, err)

	require.Len(t, job.Spec.Template.Spec.Volumes, 1)
	assert.Equal(t, toolsVolumeName, job.Spec.Template.Spec.Volumes[0].Name)
	require.NotNil(t, job.Spec.Template.Spec.Volumes[0].EmptyDir)

	assert.Contains(t, job.Spec.Template.Spec.InitContainers[0].VolumeMounts, corev1.VolumeMount{Name: toolsVolumeName, MountPath: toolsMountPath})
	assert.Contains(t, job.Spec.Template.Spec.Containers[0].VolumeMounts, corev1.VolumeMount{Name: toolsVolumeName, MountPath: toolsMountPath})
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
