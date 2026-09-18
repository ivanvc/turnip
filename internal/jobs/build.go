package jobs

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/ivanvc/turnip/internal/config"
)

const (
	// Everything turnip puts in the Runner Pod lives under a single
	// /turnip root, so a path in a Pod is recognizable at a glance and
	// nothing collides with the tool image's own filesystem.
	//
	// toolsVolumeName/toolsMountPath are the shared emptyDir volume an
	// initContainer copies a tool binary onto, and the main container
	// reads it from (Requirement 8.1/8.2).
	toolsVolumeName = "tools"
	toolsMountPath  = "/turnip/tools"

	// workspaceVolumeName/workspaceMountPath are the emptyDir the Runner
	// clones the repository into. Unlike tools it is mounted on the
	// Runner container only — the initContainer copies a binary and has
	// no business seeing the repository. The path is fixed rather than
	// randomly named because committed configuration may reference it.
	workspaceVolumeName = "workspace"
	workspaceMountPath  = "/turnip/src"

	// jobTTLSeconds bounds how long Kubernetes keeps a finished Job (and
	// its Pod) around (Requirement 7.4/9.1) — matching the reporter's
	// final-result-delivery retry budget (design.md), so a Job whose
	// Runner is still mid-retry is never garbage-collected out from
	// under it.
	jobTTLSeconds = int32(15 * 60)
)

var jobTTLSecondsAfterFinished = ptr.To(jobTTLSeconds)

// OperationParams carries the per-invocation values a Job needs beyond what
// the matched Project already provides.
type OperationParams struct {
	OperationID string
	Operation   string
	RepoURL     string
	CommitSHA   string
	BaseRef     string
	GitHubToken string
	ServerAddr  string
	ExtraArgs   []string
	PlanData    []byte
	// RunnerImage is the Runner container's image, sourced from the
	// Server's own TURNIP_RUNNER_IMAGE config.
	RunnerImage string
	// ServiceAccount is the Kubernetes ServiceAccount the Runner Pod runs
	// as — the identity cloud providers map to an IAM role (EKS Pod
	// Identity/IRSA) and that in-cluster API calls authenticate with.
	// Empty leaves it unset, so the namespace's default ServiceAccount
	// applies. The Server resolves this value (including whether a
	// Project may choose it at all) before calling BuildJob.
	ServiceAccount string
}

// BuildJob constructs the Kubernetes Job that runs one Operation for one
// Project. It resolves the Project's requested tool version before
// constructing anything else, returning an error immediately — and building
// no part of the Job spec — on an unrecognized version (Requirement 8.4).
func BuildJob(project config.Project, op OperationParams) (*batchv1.Job, error) {
	version, err := resolveVersion(project.Tool, project.ToolVersion)
	if err != nil {
		return nil, err
	}
	ti := toolImages[project.Tool]

	toolConfig, err := json.Marshal(project.With)
	if err != nil {
		return nil, fmt.Errorf("jobs: marshal tool config: %w", err)
	}
	extraArgs, err := json.Marshal(op.ExtraArgs)
	if err != nil {
		return nil, fmt.Errorf("jobs: marshal extra args: %w", err)
	}

	env := []corev1.EnvVar{
		{Name: "TURNIP_SERVER_ADDR", Value: op.ServerAddr},
		{Name: "TURNIP_OPERATION_ID", Value: op.OperationID},
		{Name: "TURNIP_PROJECT_NAME", Value: project.Name},
		{Name: "TURNIP_PROJECT_DIR", Value: project.Directory},
		{Name: "TURNIP_TOOL", Value: project.Tool},
		{Name: "TURNIP_OPERATION", Value: op.Operation},
		{Name: "TURNIP_REPO_URL", Value: op.RepoURL},
		{Name: "TURNIP_COMMIT_SHA", Value: op.CommitSHA},
		{Name: "TURNIP_BASE_REF", Value: op.BaseRef},
		{Name: "TURNIP_GITHUB_TOKEN", Value: op.GitHubToken},
		{Name: "TURNIP_TOOL_CONFIG", Value: string(toolConfig)},
		{Name: "TURNIP_EXTRA_ARGS", Value: string(extraArgs)},
		{Name: "TURNIP_PLAN_DATA", Value: base64.StdEncoding.EncodeToString(op.PlanData)},
		// TURNIP_TOOLS_DIR tells the Runner where the initContainer copied
		// the tool binary, so it can prepend that directory to its own
		// process's PATH at startup. This can't be done as a static PATH
		// override here instead: Kubernetes' $(VAR) env-value substitution
		// only resolves references to other variables declared in this
		// same list, never a running container's image-provided PATH, so
		// there is no way to express "prepend to whatever PATH already
		// is" from the Job spec alone.
		{Name: "TURNIP_TOOLS_DIR", Value: toolsMountPath},
		// TURNIP_WORKSPACE_DIR is the mounted workspace volume. The Runner
		// clones into it as-is and never removes it; absent the variable
		// it falls back to a temporary directory of its own.
		{Name: "TURNIP_WORKSPACE_DIR", Value: workspaceMountPath},
	}

	// A Project's own environment rides the Job spec rather than a
	// transport of its own, so the Runner needs no code for it — the
	// variables are simply present in the process it spawns. They come
	// last so a Project can never shadow one of turnip's.
	//
	// Kubernetes expands $(VAR) in env values against the variables
	// declared earlier in this same list, which would silently rewrite a
	// value that happens to contain $(...). Doubling every $ is the
	// documented escape, and an escaped reference is left alone whether
	// or not the name it mentions exists. Sorted so the spec is
	// deterministic and diffable.
	for _, name := range slices.Sorted(maps.Keys(project.Runner.Env)) {
		env = append(env, corev1.EnvVar{
			Name:  name,
			Value: strings.ReplaceAll(project.Runner.Env[name], "$", "$$"),
		})
	}

	toolsVolumeMount := corev1.VolumeMount{Name: toolsVolumeName, MountPath: toolsMountPath}
	workspaceVolumeMount := corev1.VolumeMount{Name: workspaceVolumeName, MountPath: workspaceMountPath}

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "turnip-runner-",
			Labels: map[string]string{
				"app.kubernetes.io/name": "turnip-runner",
				OperationIDLabel:         op.OperationID,
				ProjectLabel:             project.Name,
			},
		},
		Spec: batchv1.JobSpec{
			// A failed Operation is a reported failure, not something
			// Kubernetes should retry by re-running the whole Pod — that
			// would re-run e.g. `terraform apply` a second time, which is
			// exactly the drift locking exists to prevent. RestartPolicy
			// alone only governs in-Pod container restarts; BackoffLimit
			// is what stops the Job controller from creating a *new* Pod
			// after this one fails, so both are set to disable retries.
			BackoffLimit:            new(int32),
			TTLSecondsAfterFinished: jobTTLSecondsAfterFinished,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					ServiceAccountName: op.ServiceAccount,
					RestartPolicy:      corev1.RestartPolicyNever,
					InitContainers: []corev1.Container{
						{
							Name:         "provision-" + project.Tool,
							Image:        fmt.Sprintf(ti.image, version),
							Command:      []string{"sh", "-c", fmt.Sprintf("cp %s %s/%s", ti.binaryPath, toolsMountPath, project.Tool)},
							VolumeMounts: []corev1.VolumeMount{toolsVolumeMount},
						},
					},
					Containers: []corev1.Container{
						{
							Name:         "runner",
							Image:        op.RunnerImage,
							Env:          env,
							VolumeMounts: []corev1.VolumeMount{toolsVolumeMount, workspaceVolumeMount},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: toolsVolumeName,
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
							},
						},
						{
							Name: workspaceVolumeName,
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
							},
						},
					},
				},
			},
		},
	}

	return job, nil
}
