//go:build kind

package load

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/ivanvc/turnip/internal/config"
	"github.com/ivanvc/turnip/internal/jobs"
	"github.com/ivanvc/turnip/internal/provisioning"
)

const (
	secretsRepositoryID = int64(123456789)
	listedKey           = "lab-kubeconfig"
	listedValue         = "listed-key-content"
	unlistedKey         = "api-kubeconfig"
	unlistedValue       = "unlisted-key-content"
	missingKey          = "missing-kubeconfig"

	// secretsTestImage is turnip's Runner image, which `make test-kind`
	// builds and loads into the cluster: it is Alpine-based, so it has a
	// shell, and needs no pull.
	secretsTestImage = "ghcr.io/ivanvc/turnip-runner:dev"
	// secretsTestUID is a non-root user the image does not define, as a
	// vendor image's tool user would be to the Secret volume.
	secretsTestUID = int64(65532)

	// pendingWatch is how long the Pod listing a missing key is watched.
	pendingWatch = 60 * time.Second
)

// TestKind_RepositorySecretMount checks, against a real kubelet, the
// volume BuildJob emits for a Project's runner.secrets: a non-root
// process reads a listed key, an unlisted key of the same Secret is not
// there, and a listed key the Secret lacks keeps the Pod Pending with no
// container ever started (which the start timeout's hint relies on). It
// logs the waiting reason such a Pod reports, the answer to the
// repository-secrets design's open question.
//
// The Pods are small ones rather than BuildJob's whole Pod: they carry
// BuildJob's repository-secrets volume and the runner container's mount
// of it, plus one init container that mounts nothing, because every
// Runner Pod has a clone init container and init containers change the
// waiting reason the kubelet reports.
func TestKind_RepositorySecretMount(t *testing.T) {
	ns := os.Getenv("TURNIP_TEST_NAMESPACE")
	if ns == "" {
		ns = "default"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	secret := jobs.RepositorySecretName(secretsRepositoryID)
	require.Equal(t, "turnip-repository-123456789", secret)

	kubectl(t, ctx, "", "-n", ns, "delete", "secret", secret, "--ignore-not-found")
	kubectl(t, ctx, "", "-n", ns, "create", "secret", "generic", secret,
		"--from-literal="+listedKey+"="+listedValue,
		"--from-literal="+unlistedKey+"="+unlistedValue)
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = exec.CommandContext(cleanupCtx, "kubectl", "-n", ns, "delete", "secret", secret,
			"--ignore-not-found", "--wait=false").Run()
	})

	t.Run("a non-root process reads the listed key and only it", func(t *testing.T) {
		const name = "turnip-repository-secrets-listed"
		script := strings.Join([]string{
			"set -e",
			"d=/turnip/repository-secrets",
			`echo "UID=$(id -u)"`,
			`echo "CONTENT=$(cat "$d/` + listedKey + `")"`,
			`echo "ENTRIES=$(ls "$d" | tr '\n' ' ')"`,
			`if [ -e "$d/` + unlistedKey + `" ]; then echo "UNLISTED=present"; exit 1; fi`,
			`if touch "$d/probe" 2>/dev/null; then echo "WRITABLE=yes"; exit 1; fi`,
			`echo "UNLISTED=absent"`,
		}, "\n")
		createPod(t, ctx, ns, secretsPod(t, name, []string{listedKey}, script))

		pod := waitPodDone(t, ctx, ns, name)
		logs := kubectl(t, ctx, "", "-n", ns, "logs", name, "-c", "runner")
		t.Logf("Pod %s finished %s; runner output:\n%s", name, pod.Status.Phase, logs)
		require.Equal(t, corev1.PodSucceeded, pod.Status.Phase)

		assert.Contains(t, logs, "UID=65532")
		assert.Contains(t, logs, "CONTENT="+listedValue)
		assert.Equal(t, []string{listedKey}, logField(logs, "ENTRIES"),
			"only the listed key is a visible file in the directory")
		assert.Contains(t, logs, "UNLISTED=absent")
	})

	t.Run("a key the Secret lacks keeps the Pod Pending", func(t *testing.T) {
		const name = "turnip-repository-secrets-missing"
		createPod(t, ctx, ns, secretsPod(t, name, []string{missingKey}, "true"))

		var pod corev1.Pod
		deadline := time.Now().Add(pendingWatch)
		for {
			pod = getPod(t, ctx, ns, name)
			require.Equal(t, corev1.PodPending, pod.Status.Phase, "the Pod left Pending")
			for _, cs := range allStatuses(pod) {
				require.Nil(t, cs.State.Running, "container %s started", cs.Name)
				require.Nil(t, cs.State.Terminated, "container %s ran", cs.Name)
				require.Nil(t, cs.LastTerminationState.Terminated, "container %s ran before", cs.Name)
				require.Zero(t, cs.RestartCount, "container %s restarted", cs.Name)
			}
			if time.Now().After(deadline) {
				break
			}
			select {
			case <-ctx.Done():
				require.NoError(t, ctx.Err(), "watching Pod %s", name)
			case <-time.After(5 * time.Second):
			}
		}

		assert.NotEmpty(t, pod.Status.InitContainerStatuses, "the kubelet reports the init container")
		assert.NotEmpty(t, pod.Status.ContainerStatuses, "the kubelet reports the runner container")
		for _, cs := range pod.Status.InitContainerStatuses {
			t.Logf("init container %s: %s", cs.Name, describeState(cs.State))
		}
		for _, cs := range pod.Status.ContainerStatuses {
			t.Logf("container %s: %s", cs.Name, describeState(cs.State))
		}

		events := kubectl(t, ctx, "", "-n", ns, "get", "events",
			"--field-selector", "involvedObject.kind=Pod,involvedObject.name="+name+",reason=FailedMount",
			"-o", "jsonpath={range .items[*]}{.reason} (count {.count}): {.message}{\"\\n\"}{end}")
		t.Logf("FailedMount events for Pod %s:\n%s", name, events)
		assert.Contains(t, events, missingKey, "a FailedMount event names the missing key")
	})
}

// secretsPod builds a Pod carrying the repository-secrets volume and the
// runner container's mount of it exactly as BuildJob emits them for a
// Project listing keys, running script as a non-root user after an init
// container that mounts nothing.
func secretsPod(t *testing.T, name string, keys []string, script string) *corev1.Pod {
	t.Helper()
	project := config.Project{
		Name:      "lab",
		Directory: "infra/lab",
		Tool:      "terraform",
		Runner:    config.RunnerSpec{Secrets: config.SecretKeys(keys)},
	}
	job, err := jobs.BuildJob(project, jobs.OperationParams{
		OperationID:  "op-kind",
		Operation:    "plan",
		RepoURL:      "https://github.com/example/project.git",
		CommitSHA:    "abc123",
		BaseRef:      "main",
		ServerAddr:   "turnip-server:9090",
		RunnerImage:  secretsTestImage,
		Provisioning: provisioning.Spec{Strategy: provisioning.CopyOut, Image: "hashicorp/terraform", BinaryPath: "/bin/terraform"},
		ImageRef:     "hashicorp/terraform:1.9.0",
		RepositoryID: secretsRepositoryID,
	})
	require.NoError(t, err)

	spec := job.Spec.Template.Spec
	var volume *corev1.Volume
	for i := range spec.Volumes {
		if spec.Volumes[i].Name == "repository-secrets" {
			volume = &spec.Volumes[i]
		}
	}
	require.NotNil(t, volume, "BuildJob emitted no repository-secrets volume")

	var mount *corev1.VolumeMount
	for _, c := range spec.Containers {
		if c.Name != "runner" {
			continue
		}
		for i := range c.VolumeMounts {
			if c.VolumeMounts[i].Name == "repository-secrets" {
				mount = &c.VolumeMounts[i]
			}
		}
	}
	require.NotNil(t, mount, "BuildJob's runner container does not mount repository-secrets")

	return &corev1.Pod{
		TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Pod"},
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyNever,
			SecurityContext: &corev1.PodSecurityContext{
				RunAsNonRoot: ptr.To(true),
				RunAsUser:    ptr.To(secretsTestUID),
				RunAsGroup:   ptr.To(secretsTestUID),
			},
			InitContainers: []corev1.Container{{
				Name:            "clone",
				Image:           secretsTestImage,
				ImagePullPolicy: corev1.PullIfNotPresent,
				Command:         []string{"true"},
			}},
			Containers: []corev1.Container{{
				Name:            "runner",
				Image:           secretsTestImage,
				ImagePullPolicy: corev1.PullIfNotPresent,
				Command:         []string{"sh", "-c", script},
				VolumeMounts:    []corev1.VolumeMount{*mount},
			}},
			Volumes: []corev1.Volume{*volume},
		},
	}
}

// createPod applies pod and deletes it when the test ends.
func createPod(t *testing.T, ctx context.Context, ns string, pod *corev1.Pod) {
	t.Helper()
	manifest, err := json.Marshal(pod)
	require.NoError(t, err)
	kubectl(t, ctx, "", "-n", ns, "delete", "pod", pod.Name, "--ignore-not-found")
	kubectl(t, ctx, string(manifest), "-n", ns, "create", "-f", "-")
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = exec.CommandContext(cleanupCtx, "kubectl", "-n", ns, "delete", "pod", pod.Name,
			"--ignore-not-found", "--wait=false").Run()
	})
}

// waitPodDone polls the Pod until it has Succeeded or Failed.
func waitPodDone(t *testing.T, ctx context.Context, ns, name string) corev1.Pod {
	t.Helper()
	for {
		pod := getPod(t, ctx, ns, name)
		if pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
			return pod
		}
		select {
		case <-ctx.Done():
			for _, cs := range allStatuses(pod) {
				t.Logf("container %s: %s", cs.Name, describeState(cs.State))
			}
			handleKindTimeout(t, ctx, "Pod "+name+" finishing", ctx.Err())
			require.NoError(t, ctx.Err(), "waiting for Pod %s", name)
		case <-time.After(2 * time.Second):
		}
	}
}

func getPod(t *testing.T, ctx context.Context, ns, name string) corev1.Pod {
	t.Helper()
	out := kubectl(t, ctx, "", "-n", ns, "get", "pod", name, "-o", "json")
	var pod corev1.Pod
	require.NoError(t, json.Unmarshal([]byte(out), &pod))
	return pod
}

func allStatuses(pod corev1.Pod) []corev1.ContainerStatus {
	return append(append([]corev1.ContainerStatus{}, pod.Status.InitContainerStatuses...),
		pod.Status.ContainerStatuses...)
}

func describeState(s corev1.ContainerState) string {
	switch {
	case s.Waiting != nil:
		return "waiting, reason " + s.Waiting.Reason + ", message " + s.Waiting.Message
	case s.Running != nil:
		return "running"
	case s.Terminated != nil:
		return "terminated, reason " + s.Terminated.Reason
	default:
		return "no state"
	}
}

// logField returns the space-separated words after "<field>=" on the
// line that starts with it.
func logField(logs, field string) []string {
	for line := range strings.SplitSeq(logs, "\n") {
		if value, ok := strings.CutPrefix(line, field+"="); ok {
			return strings.Fields(value)
		}
	}
	return nil
}

// kubectl runs kubectl with stdin and requires it to succeed, returning
// its standard output.
func kubectl(t *testing.T, ctx context.Context, stdin string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(ctx, "kubectl", args...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if handleKindTimeout(t, ctx, "kubectl "+strings.Join(args, " "), err) {
		return ""
	}
	require.NoError(t, err, "kubectl %s: %s", strings.Join(args, " "), stderr.String())
	return string(out)
}
