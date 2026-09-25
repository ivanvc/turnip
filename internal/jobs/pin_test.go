package jobs

import (
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivanvc/turnip/internal/config"
	"github.com/ivanvc/turnip/internal/provisioning"
)

// pinnedHelmfileInput is the Project and OperationParams the pinned Job is
// built from. It is the only input the test has, so a change to how a
// Project reaches BuildJob touches this function and nothing else.
//
// Slice 45 (Requirement 6.1) changes it in exactly two ways: ToolVersion
// becomes "v1.7.4", because `uses:` keeps the "v" as written, and
// OperationParams gains the Helmfile Plugin's provisioning Spec, because
// the jobs package stops keeping its own table of tools.
func pinnedHelmfileInput() (config.Project, OperationParams) {
	project := config.Project{
		Name:        "web",
		Directory:   "infra/web",
		Uses:        "helmfile@v1.7.4",
		Tool:        "helmfile",
		ToolVersion: "v1.7.4",
		With:        map[string]string{"environment": "staging"},
	}
	params := OperationParams{
		OperationID:    "op-123",
		Operation:      "diff",
		RepoURL:        "https://github.com/acme/repo.git",
		CommitSHA:      "abc123",
		BaseRef:        "main",
		ServerAddr:     "server.turnip.svc:9443",
		RunnerImage:    "ghcr.io/ivanvc/turnip-runner:test",
		ServiceAccount: "turnip-runner",
		Submodules:     "recursive",
		Provisioning: provisioning.Spec{
			Strategy: provisioning.RunInImage,
			Image:    "ghcr.io/helmfile/helmfile",
		},
	}
	return project, params
}

// pinnedToolVersion is the one value in the pinned Job that Slice 45 is
// meant to change (Requirement 6.1): TURNIP_TOOL_VERSION carries the
// version as the Project wrote it, so it becomes "v1.7.4". The image does
// not change, because the jobs package's table used to add the "v" and
// now the Project supplies it.
const pinnedToolVersion = "v1.7.4"

// TestBuildJob_PinnedHelmfileJob pins the whole Job BuildJob builds for a
// Helmfile Project, so a refactor of where tool knowledge lives can show
// it runs the same image, provisioned the same way (Slice 45,
// Requirement 6.1). The expected Job is written out literally rather
// than derived from the package's constants, so renaming one of them
// cannot quietly move the Pod's shape along with it.
func TestBuildJob_PinnedHelmfileJob(t *testing.T) {
	project, params := pinnedHelmfileInput()

	job, err := BuildJob(project, params)
	require.NoError(t, err)

	baseEnv := []corev1.EnvVar{
		{Name: "TURNIP_SERVER_ADDR", Value: "server.turnip.svc:9443"},
		{Name: "TURNIP_OPERATION_ID", Value: "op-123"},
		{Name: "TURNIP_PROJECT_NAME", Value: "web"},
		{Name: "TURNIP_PROJECT_DIR", Value: "infra/web"},
		{Name: "TURNIP_TOOL", Value: "helmfile"},
		{Name: "TURNIP_TOOL_VERSION", Value: pinnedToolVersion},
		{Name: "TURNIP_OPERATION", Value: "diff"},
		{Name: "TURNIP_REPO_URL", Value: "https://github.com/acme/repo.git"},
		{Name: "TURNIP_COMMIT_SHA", Value: "abc123"},
		{Name: "TURNIP_BASE_REF", Value: "main"},
		{Name: "TURNIP_TOOL_CONFIG", Value: `{"environment":"staging"}`},
		{Name: "TURNIP_EXTRA_ARGS", Value: "null"},
		{Name: "TURNIP_PLAN_DATA", Value: ""},
		{Name: "TURNIP_WORKSPACE_DIR", Value: "/turnip/src"},
		{Name: "TURNIP_TOKEN_FILE", Value: "/turnip/run/secrets/token"},
	}
	// Each container gets its own copy, so appending to one list can
	// never write through to another's backing array.
	env := func(extra ...corev1.EnvVar) []corev1.EnvVar {
		return append(append([]corev1.EnvVar{}, baseEnv...), extra...)
	}

	binMount := corev1.VolumeMount{Name: "bin", MountPath: "/turnip/bin"}
	workspaceMount := corev1.VolumeMount{Name: "workspace", MountPath: "/turnip/src"}
	tokenMount := corev1.VolumeMount{Name: "turnip-token", MountPath: "/turnip/run/secrets", ReadOnly: true}

	want := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "turnip-runner-",
			Labels: map[string]string{
				"app.kubernetes.io/name":      "turnip-runner",
				"turnip.ivan.vc/operation-id": "op-123",
				"turnip.ivan.vc/project":      "web",
			},
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            ptr.To(int32(0)),
			TTLSecondsAfterFinished: ptr.To(int32(900)),
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					ServiceAccountName: "turnip-runner",
					RestartPolicy:      corev1.RestartPolicyNever,
					InitContainers: []corev1.Container{
						{
							Name:         "copy-runner",
							Image:        "ghcr.io/ivanvc/turnip-runner:test",
							Command:      []string{"sh", "-c", "cp /runner /turnip/bin/runner"},
							VolumeMounts: []corev1.VolumeMount{binMount},
						},
						{
							Name:         "clone",
							Image:        "ghcr.io/ivanvc/turnip-runner:test",
							Args:         []string{"clone"},
							Env:          env(corev1.EnvVar{Name: "TURNIP_CLONE_SUBMODULES", Value: "recursive"}),
							VolumeMounts: []corev1.VolumeMount{workspaceMount, tokenMount},
						},
					},
					Containers: []corev1.Container{
						{
							Name:         "runner",
							Image:        "ghcr.io/helmfile/helmfile:v1.7.4",
							Command:      []string{"/turnip/bin/runner"},
							Env:          env(),
							VolumeMounts: []corev1.VolumeMount{binMount, workspaceMount, tokenMount},
						},
					},
					Volumes: []corev1.Volume{
						{Name: "bin", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
						{Name: "workspace", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
						{
							Name: "turnip-token",
							VolumeSource: corev1.VolumeSource{
								Projected: &corev1.ProjectedVolumeSource{
									Sources: []corev1.VolumeProjection{{
										ServiceAccountToken: &corev1.ServiceAccountTokenProjection{
											Audience:          "turnip.ivan.vc",
											ExpirationSeconds: ptr.To(int64(600)),
											Path:              "token",
										},
									}},
								},
							},
						},
					},
				},
			},
		},
	}

	assert.Equal(t, want, job)
}
