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
	// reads it from (Requirement 8.1/8.2). Used by copyOut only.
	toolsVolumeName = "tools"
	toolsMountPath  = "/turnip/tools"

	// binVolumeName/binMountPath carry turnip's *own* runner binary into a
	// vendor image under runInImage. Deliberately separate from the tools
	// volume: what lands here is turnip's binary, not the tool's, and
	// naming it "tools" would make the Pod lie about what it holds.
	binVolumeName = "bin"
	binMountPath  = "/turnip/bin"

	// workspaceVolumeName/workspaceMountPath are the emptyDir the
	// repository is cloned into, by the clone initContainer. Both that
	// container and the one running the tool mount it; the path is fixed
	// rather than randomly named because committed configuration may
	// reference it.
	workspaceVolumeName = "workspace"
	workspaceMountPath  = "/turnip/src"

	// tokenVolumeName/tokenMountPath/tokenFileName carry the Runner's
	// proof of identity: a ServiceAccount token projected with turnip's
	// own audience, which the Server validates with a TokenReview before
	// letting the caller write to an Operation (Slice 25).
	//
	// It is a volume projection — a field of the Pod spec turnip already
	// writes — and not a change to any ServiceAccount object. That is what
	// keeps Slice 13's runner.serviceAccount override orthogonal: a
	// Project may still choose which account the Pod runs as, because the
	// identity that matters here is the Pod's, not the account's.
	tokenVolumeName = "turnip-token"
	tokenMountPath  = "/turnip/run/secrets"
	tokenFileName   = "token"

	// tokenExpirationSeconds is how long kubelet lets a projected token
	// live before rotating it in place. Ten minutes is the API server's
	// own floor; asking for less gets silently rounded up. The Runner
	// re-reads the file on every stream it opens rather than caching what
	// it read at startup, so a rotation mid-Operation costs nothing.
	tokenExpirationSeconds = int64(10 * 60)

	// jobTTLSeconds bounds how long Kubernetes keeps a finished Job (and
	// its Pod) around (Requirement 7.4/9.1) — matching the reporter's
	// final-result-delivery retry budget (design.md), so a Job whose
	// Runner is still mid-retry is never garbage-collected out from
	// under it.
	jobTTLSeconds = int32(15 * 60)
)

// TokenAudience is what the Runner's projected token is scoped to, and
// the only audience the Server's TokenReview accepts.
//
// It is deliberately not the API server's own audience. A Pod's default
// token — the one at /var/run/secrets/kubernetes.io/serviceaccount —
// would be accepted by any component that merely checks "is this a valid
// token", and the Server could then replay it to the API server *as* the
// Runner's ServiceAccount. Scoping to an audience nothing else honours
// makes that impossible rather than merely discouraged.
const TokenAudience = "turnip.ivan.vc"

var jobTTLSecondsAfterFinished = ptr.To(jobTTLSeconds)

// OperationParams carries the per-invocation values a Job needs beyond what
// the matched Project already provides.
type OperationParams struct {
	OperationID string
	Operation   string
	RepoURL     string
	CommitSHA   string
	BaseRef     string
	ServerAddr  string
	ExtraArgs   []string
	PlanData    []byte
	// RunnerImage is turnip's own image, from the Server's
	// TURNIP_RUNNER_IMAGE config. It serves two roles depending on the
	// tool's strategy: under copyOut it is the main container's image,
	// and under runInImage it is the image the runner binary is copied
	// *out of*, while the vendor's image runs as the main container. One
	// field rather than two, because a Job never needs two turnip images.
	RunnerImage string
	// ServiceAccount is the Kubernetes ServiceAccount the Runner Pod runs
	// as — the identity cloud providers map to an IAM role (EKS Pod
	// Identity/IRSA) and that in-cluster API calls authenticate with.
	// Empty leaves it unset, so the namespace's default ServiceAccount
	// applies. The Server resolves this value (including whether a
	// Project may choose it at all) before calling BuildJob.
	ServiceAccount string
	// Submodules is the Submodule_Mode the clone uses, resolved by the
	// Server from its own default and the repository's override (and
	// whether that override is permitted) before calling BuildJob. Empty
	// means top-level, so a Job carrying no value still initialises
	// submodules rather than silently leaving an empty directory.
	Submodules string
}

// BuildJob constructs the Kubernetes Job that runs one Operation for one
// Project. It resolves the Project's requested tool version before
// constructing anything else, returning an error immediately — and building
// no part of the Job spec — on an unrecognized version (Requirement 8.4).
//
// The Job's shape depends on the tool's provisioning strategy: copyOut
// copies a binary out of the vendor image for turnip's Runner image to
// execute, while runInImage makes the vendor image the main container and
// hands it turnip's runner binary instead. Either way the repository is
// cloned by an initContainer, so the container that runs the tool needs
// nothing from its image but the tool.
func BuildJob(project config.Project, op OperationParams) (*batchv1.Job, error) {
	version, err := resolveVersion(project.Tool, project.ToolVersion)
	if err != nil {
		return nil, err
	}
	ti := toolImages[project.Tool]
	toolImageRef := fmt.Sprintf(ti.image, version)

	toolConfig, err := json.Marshal(project.With)
	if err != nil {
		return nil, fmt.Errorf("jobs: marshal tool config: %w", err)
	}
	extraArgs, err := json.Marshal(op.ExtraArgs)
	if err != nil {
		return nil, fmt.Errorf("jobs: marshal extra args: %w", err)
	}

	// baseEnv is what both the clone initContainer and the container
	// running the tool need. Two things are deliberately absent: the
	// GitHub token, which only the clone needs, and TURNIP_TOOLS_DIR,
	// which only means anything under copyOut.
	baseEnv := []corev1.EnvVar{
		{Name: "TURNIP_SERVER_ADDR", Value: op.ServerAddr},
		{Name: "TURNIP_OPERATION_ID", Value: op.OperationID},
		{Name: "TURNIP_PROJECT_NAME", Value: project.Name},
		{Name: "TURNIP_PROJECT_DIR", Value: project.Directory},
		{Name: "TURNIP_TOOL", Value: project.Tool},
		// The version this Job's tool image was built from. The Runner
		// records it in the execution transcript so a pull request read
		// months later says which binary produced its output — the usual
		// answer to "why did this change when I did not touch anything".
		//
		// It is the version turnip *requested*, which is the one that runs
		// while resolveVersion rejects a floating tag. If floating tags
		// are ever allowed, this is the value that has to start reporting
		// what resolved.
		{Name: "TURNIP_TOOL_VERSION", Value: version},
		{Name: "TURNIP_OPERATION", Value: op.Operation},
		{Name: "TURNIP_REPO_URL", Value: op.RepoURL},
		{Name: "TURNIP_COMMIT_SHA", Value: op.CommitSHA},
		{Name: "TURNIP_BASE_REF", Value: op.BaseRef},
		{Name: "TURNIP_TOOL_CONFIG", Value: string(toolConfig)},
		{Name: "TURNIP_EXTRA_ARGS", Value: string(extraArgs)},
		{Name: "TURNIP_PLAN_DATA", Value: base64.StdEncoding.EncodeToString(op.PlanData)},
		// TURNIP_WORKSPACE_DIR is the mounted workspace volume: where the
		// clone initContainer writes the repository, and where the tool
		// then finds it.
		{Name: "TURNIP_WORKSPACE_DIR", Value: workspaceMountPath},
		// Both containers that reach the Server need to prove who they
		// are, so the token path is base environment rather than
		// per-container: the clone reports its own failures over the same
		// authenticated stream the tool's container later uses.
		{Name: "TURNIP_TOKEN_FILE", Value: tokenMountPath + "/" + tokenFileName},
	}

	toolsMount := corev1.VolumeMount{Name: toolsVolumeName, MountPath: toolsMountPath}
	binMount := corev1.VolumeMount{Name: binVolumeName, MountPath: binMountPath}
	workspaceMount := corev1.VolumeMount{Name: workspaceVolumeName, MountPath: workspaceMountPath}
	// ReadOnly because nothing in the Pod has any business rewriting the
	// credential kubelet maintains there.
	tokenMount := corev1.VolumeMount{Name: tokenVolumeName, MountPath: tokenMountPath, ReadOnly: true}

	// The clone runs in turnip's own image, which has git — so the image
	// that runs the tool needs nothing but the tool, and can be a vendor
	// image turnip does not control.
	//
	// No GitHub credential is set on any container. The clone asks the
	// Server for one when git needs it, over the channel Slice 25
	// authenticates — so reading this Pod, or the etcd behind it, yields
	// nothing worth having (Slice 24, Requirement 2.1).
	cloneContainer := corev1.Container{
		Name:  "clone",
		Image: op.RunnerImage,
		Args:  []string{"clone"},
		// The submodule mode is set here for the same reason as the token:
		// only the clone reads it, so the container that runs the tool has
		// no business carrying it.
		Env: append(slices.Clone(baseEnv),
			corev1.EnvVar{Name: "TURNIP_CLONE_SUBMODULES", Value: op.Submodules},
		),
		VolumeMounts: []corev1.VolumeMount{workspaceMount, tokenMount},
	}

	var (
		initContainers []corev1.Container
		mainImage      string
		mainCommand    []string
		mainMounts     []corev1.VolumeMount
		volumes        []corev1.Volume
	)
	mainEnv := slices.Clone(baseEnv)

	switch ti.strategy {
	case runInImage:
		initContainers = []corev1.Container{
			{
				Name:         "copy-runner",
				Image:        op.RunnerImage,
				Command:      []string{"sh", "-c", fmt.Sprintf("cp /runner %s/runner", binMountPath)},
				VolumeMounts: []corev1.VolumeMount{binMount},
			},
			cloneContainer,
		}
		mainImage = toolImageRef
		// Overrides the vendor image's own entrypoint. The runner binary
		// is statically linked (CGO_ENABLED=0), so it executes in any
		// image regardless of libc.
		mainCommand = []string{binMountPath + "/runner"}
		mainMounts = []corev1.VolumeMount{binMount, workspaceMount, tokenMount}
		volumes = []corev1.Volume{emptyDirVolume(binVolumeName), emptyDirVolume(workspaceVolumeName), tokenVolume()}

		// No TURNIP_TOOLS_DIR: the tool is already on the vendor image's
		// own PATH. The Runner needs no branch for this — pathWithToolsDir
		// leaves PATH untouched when the variable is absent.

	default: // copyOut
		initContainers = []corev1.Container{
			{
				// Provisioning runs before the clone so an unpullable tool
				// version fails before turnip fetches a repository it is
				// about to throw away.
				Name:         "provision-" + project.Tool,
				Image:        toolImageRef,
				Command:      []string{"sh", "-c", fmt.Sprintf("cp %s %s/%s", ti.binaryPath, toolsMountPath, project.Tool)},
				VolumeMounts: []corev1.VolumeMount{toolsMount},
			},
			cloneContainer,
		}
		mainImage = op.RunnerImage
		mainMounts = []corev1.VolumeMount{toolsMount, workspaceMount, tokenMount}
		volumes = []corev1.Volume{emptyDirVolume(toolsVolumeName), emptyDirVolume(workspaceVolumeName), tokenVolume()}

		// TURNIP_TOOLS_DIR tells the Runner where the initContainer copied
		// the tool binary, so it can prepend that directory to its own
		// process's PATH at startup. This can't be done as a static PATH
		// override here instead: Kubernetes' $(VAR) env-value substitution
		// only resolves references to other variables declared in this
		// same list, never a running container's image-provided PATH, so
		// there is no way to express "prepend to whatever PATH already
		// is" from the Job spec alone.
		mainEnv = append(mainEnv, corev1.EnvVar{Name: "TURNIP_TOOLS_DIR", Value: toolsMountPath})
	}

	// A Project's own environment rides the Job spec rather than a
	// transport of its own, so the Runner needs no code for it — the
	// variables are simply present in the process it spawns. They come
	// last so a Project can never shadow one of turnip's, and they go on
	// the container running the tool rather than on the clone, which they
	// have nothing to do with.
	//
	// Kubernetes expands $(VAR) in env values against the variables
	// declared earlier in this same list, which would silently rewrite a
	// value that happens to contain $(...). Doubling every $ is the
	// documented escape, and an escaped reference is left alone whether
	// or not the name it mentions exists. Sorted so the spec is
	// deterministic and diffable.
	for _, name := range slices.Sorted(maps.Keys(project.Runner.Env)) {
		mainEnv = append(mainEnv, corev1.EnvVar{
			Name:  name,
			Value: strings.ReplaceAll(project.Runner.Env[name], "$", "$$"),
		})
	}

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
					InitContainers:     initContainers,
					Containers: []corev1.Container{
						{
							// Named "runner" under both strategies, so
							// `kubectl logs -c runner` works regardless of
							// which image it happens to be.
							Name:         "runner",
							Image:        mainImage,
							Command:      mainCommand,
							Env:          mainEnv,
							VolumeMounts: mainMounts,
						},
					},
					Volumes: volumes,
				},
			},
		},
	}

	return job, nil
}

// tokenVolume is the ServiceAccountToken projection the Runner presents
// to the Server. kubelet mints it against the Pod's own ServiceAccount,
// scoped to turnip's audience, and rotates it in place as it nears
// expiry.
//
// The projection carries the Pod's identity — name and uid — in the
// resulting token's claims, which is what lets the Server bind a caller
// to one Operation rather than merely recognising that it is some Runner.
func tokenVolume() corev1.Volume {
	return corev1.Volume{
		Name: tokenVolumeName,
		VolumeSource: corev1.VolumeSource{
			Projected: &corev1.ProjectedVolumeSource{
				Sources: []corev1.VolumeProjection{{
					ServiceAccountToken: &corev1.ServiceAccountTokenProjection{
						Audience:          TokenAudience,
						ExpirationSeconds: ptr.To(tokenExpirationSeconds),
						Path:              tokenFileName,
					},
				}},
			},
		},
	}
}

func emptyDirVolume(name string) corev1.Volume {
	return corev1.Volume{
		Name:         name,
		VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
	}
}
