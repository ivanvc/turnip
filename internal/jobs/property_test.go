package jobs

import (
	"testing"

	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/ivanvc/turnip/internal/config"
)

func genTool(t *rapid.T) string {
	tools := make([]string, 0, len(toolImages))
	for tool := range toolImages {
		tools = append(tools, tool)
	}
	return rapid.SampledFrom(tools).Draw(t, "tool")
}

// genProject builds a Project the way Parse would have left one: Tool and
// ToolVersion are derived fields, and applyDefaults runs only inside
// Parse, so a hand-built fixture has to set them itself. Setting Uses
// alone would produce a Project with no tool at all.
func genProject(t *rapid.T) config.Project {
	tool := genTool(t)
	return config.Project{
		Name:      rapid.StringMatching(`[a-z][a-z0-9-]{0,15}`).Draw(t, "name"),
		Directory: rapid.StringMatching(`[a-z][a-z0-9/_-]{0,20}`).Draw(t, "directory"),
		Uses:      tool,
		Tool:      tool,
	}
}

func genOperationParams(t *rapid.T) OperationParams {
	return OperationParams{
		OperationID: rapid.StringMatching(`[a-f0-9-]{8,36}`).Draw(t, "operationID"),
		Operation:   rapid.StringMatching(`[a-z]{2,10}`).Draw(t, "operation"),
		RepoURL:     "https://github.com/" + rapid.StringMatching(`[a-z]{2,10}/[a-z]{2,10}`).Draw(t, "repoURL") + ".git",
		CommitSHA:   rapid.StringMatching(`[a-f0-9]{40}`).Draw(t, "commitSHA"),
		GitHubToken: rapid.StringMatching(`[A-Za-z0-9._-]{5,40}`).Draw(t, "githubToken"),
		ServerAddr:  rapid.StringMatching(`[a-z0-9.-]{3,20}:[0-9]{2,5}`).Draw(t, "serverAddr"),
		ExtraArgs:   rapid.SliceOfN(rapid.StringMatching(`--[a-z-]{2,10}`), 0, 3).Draw(t, "extraArgs"),
		PlanData:    []byte(rapid.String().Draw(t, "planData")),
	}
}

// Feature: multi-iac-automation-platform, Property 23: Runner Job Environment Variables
func TestProperty_RunnerJobEnvironmentVariables(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		project := genProject(t)
		params := genOperationParams(t)

		job, err := BuildJob(project, params)
		require.NoError(t, err)
		require.Len(t, job.Spec.Template.Spec.Containers, 1)

		env := envMap(job.Spec.Template.Spec.Containers[0])
		require.Equal(t, params.RepoURL, env["TURNIP_REPO_URL"])
		require.Equal(t, params.CommitSHA, env["TURNIP_COMMIT_SHA"])
		require.Equal(t, project.Directory, env["TURNIP_PROJECT_DIR"])
		require.Equal(t, params.Operation, env["TURNIP_OPERATION"])
	})
}

// Feature: multi-iac-automation-platform, Property 23a: Runner Job Tool Provisioning
func TestProperty_RunnerJobToolProvisioning(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		tool := genTool(t)
		ti := toolImages[tool]
		version := rapid.SampledFrom(ti.versions).Draw(t, "version")

		project := genProject(t)
		project.Uses = tool + "@" + version
		project.Tool = tool
		project.ToolVersion = version

		job, err := BuildJob(project, genOperationParams(t))
		require.NoError(t, err)

		initContainers := job.Spec.Template.Spec.InitContainers
		require.Len(t, initContainers, 1)
		require.Contains(t, initContainers[0].Image, version)
		require.Len(t, initContainers[0].VolumeMounts, 1)

		require.Len(t, job.Spec.Template.Spec.Containers, 1)
		mainMounts := job.Spec.Template.Spec.Containers[0].VolumeMounts
		require.Contains(t, mainMounts, initContainers[0].VolumeMounts[0])
	})
}

// Feature: multi-iac-automation-platform, Property 25: Job Cleanup After Completion
// (reinterpreted: this slice's mechanism is ttlSecondsAfterFinished, not an
// explicit post-completion delete call.)
func TestProperty_JobCleanupAfterCompletion(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		job, err := BuildJob(genProject(t), genOperationParams(t))
		require.NoError(t, err)
		require.NotNil(t, job.Spec.TTLSecondsAfterFinished)
		require.Equal(t, jobTTLSeconds, *job.Spec.TTLSecondsAfterFinished)
	})
}

// Feature: multi-iac-automation-platform, Property 27: Token Propagation to Runner
func TestProperty_TokenPropagationToRunner(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		project := genProject(t)
		params := genOperationParams(t)

		job, err := BuildJob(project, params)
		require.NoError(t, err)
		require.Len(t, job.Spec.Template.Spec.Containers, 1)

		env := envMap(job.Spec.Template.Spec.Containers[0])
		require.Equal(t, params.GitHubToken, env["TURNIP_GITHUB_TOKEN"])
	})
}
