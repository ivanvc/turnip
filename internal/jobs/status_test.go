package jobs

import (
	"context"
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_StatusJobNotFound(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	client := NewClient(clientset, "turnip")

	status, err := client.Status(context.Background(), "does-not-exist")
	require.NoError(t, err)
	assert.False(t, status.JobFound)
}

func TestClient_StatusJobFoundNoPodYet(t *testing.T) {
	clientset := fake.NewSimpleClientset(&batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "turnip-runner-abc", Namespace: "turnip"},
	})
	client := NewClient(clientset, "turnip")

	status, err := client.Status(context.Background(), "turnip-runner-abc")
	require.NoError(t, err)
	assert.True(t, status.JobFound)
	assert.Empty(t, status.PodPhase)
	assert.Empty(t, status.PodReason)
}

func TestClient_StatusInitContainerImagePullBackOff(t *testing.T) {
	clientset := fake.NewSimpleClientset(
		&batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "turnip-runner-abc", Namespace: "turnip"}},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "turnip-runner-abc-xyz",
				Namespace: "turnip",
				Labels:    map[string]string{"batch.kubernetes.io/job-name": "turnip-runner-abc"},
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodPending,
				InitContainerStatuses: []corev1.ContainerStatus{
					{State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{
						Reason: "ImagePullBackOff", Message: "back-off pulling image",
					}}},
				},
				// A later regular container also Waiting must not override
				// the init container's reason.
				ContainerStatuses: []corev1.ContainerStatus{
					{State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{
						Reason: "PodInitializing",
					}}},
				},
			},
		},
	)
	client := NewClient(clientset, "turnip")

	status, err := client.Status(context.Background(), "turnip-runner-abc")
	require.NoError(t, err)
	assert.True(t, status.JobFound)
	assert.Equal(t, corev1.PodPending, status.PodPhase)
	assert.Equal(t, "ImagePullBackOff", status.PodReason)
	assert.Equal(t, "back-off pulling image", status.PodMessage)
}

func TestClient_StatusPodRunningNoWaitingStatuses(t *testing.T) {
	clientset := fake.NewSimpleClientset(
		&batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "turnip-runner-abc", Namespace: "turnip"}},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "turnip-runner-abc-xyz",
				Namespace: "turnip",
				Labels:    map[string]string{"batch.kubernetes.io/job-name": "turnip-runner-abc"},
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{
					{State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}},
				},
			},
		},
	)
	client := NewClient(clientset, "turnip")

	status, err := client.Status(context.Background(), "turnip-runner-abc")
	require.NoError(t, err)
	assert.Equal(t, corev1.PodRunning, status.PodPhase)
	assert.Empty(t, status.PodReason)
}

// statusWithPod returns the Status of a Job whose one Pod reports status.
func statusWithPod(t *testing.T, podStatus corev1.PodStatus) *JobStatus {
	t.Helper()
	clientset := fake.NewSimpleClientset(
		&batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "turnip-runner-abc", Namespace: "turnip"}},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "turnip-runner-abc-xyz",
				Namespace: "turnip",
				Labels:    map[string]string{"batch.kubernetes.io/job-name": "turnip-runner-abc"},
			},
			Status: podStatus,
		},
	)
	status, err := NewClient(clientset, "turnip").Status(context.Background(), "turnip-runner-abc")
	require.NoError(t, err)
	return status
}

const (
	wantToolImageID   = "ghcr.io/helmfile/helmfile@sha256:1111111111111111111111111111111111111111111111111111111111111111"
	wantRunnerImageID = "ghcr.io/ivanvc/turnip-runner@sha256:2222222222222222222222222222222222222222222222222222222222222222"
)

// Under RunInImage the runner container runs the tool's image, so its
// imageID is the one reported; turnip's own initContainers are not.
func TestClient_StatusImageIDRunInImage(t *testing.T) {
	status := statusWithPod(t, corev1.PodStatus{
		Phase: corev1.PodSucceeded,
		InitContainerStatuses: []corev1.ContainerStatus{
			{Name: "copy-runner", ImageID: wantRunnerImageID},
			{Name: "clone", ImageID: wantRunnerImageID},
		},
		ContainerStatuses: []corev1.ContainerStatus{
			{Name: "runner", ImageID: wantToolImageID},
		},
	})
	assert.Equal(t, wantToolImageID, status.ImageID)
}

// Under CopyOut the provisioning initContainer is what ran the tool's
// image; the runner container runs turnip's.
func TestClient_StatusImageIDCopyOut(t *testing.T) {
	status := statusWithPod(t, corev1.PodStatus{
		Phase: corev1.PodSucceeded,
		InitContainerStatuses: []corev1.ContainerStatus{
			{Name: "provision-terraform", ImageID: wantToolImageID},
			{Name: "clone", ImageID: wantRunnerImageID},
		},
		ContainerStatuses: []corev1.ContainerStatus{
			{Name: "runner", ImageID: wantRunnerImageID},
		},
	})
	assert.Equal(t, wantToolImageID, status.ImageID)
}

// A waiting initContainer still reports the tool's imageID alongside the
// reason, and an image not yet pulled reports none.
func TestClient_StatusImageIDWithWaitingInitContainer(t *testing.T) {
	status := statusWithPod(t, corev1.PodStatus{
		Phase: corev1.PodPending,
		InitContainerStatuses: []corev1.ContainerStatus{
			{Name: "provision-terraform", ImageID: wantToolImageID},
			{Name: "clone", State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}}},
		},
	})
	assert.Equal(t, "CrashLoopBackOff", status.PodReason)
	assert.Equal(t, wantToolImageID, status.ImageID)

	status = statusWithPod(t, corev1.PodStatus{
		Phase: corev1.PodPending,
		ContainerStatuses: []corev1.ContainerStatus{
			{Name: "runner", State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ErrImagePull"}}},
		},
	})
	assert.Empty(t, status.ImageID)
}

// ToolWaiting says whether the waiting container runs the tool's image, so
// a pull failure on turnip's own Runner image is not blamed on the tool.
func TestClient_StatusToolWaiting(t *testing.T) {
	pull := func(name string) corev1.ContainerStatus {
		return corev1.ContainerStatus{Name: name, State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ImagePullBackOff"}}}
	}
	pending := func(name string) corev1.ContainerStatus {
		return corev1.ContainerStatus{Name: name, State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "PodInitializing"}}}
	}
	tests := []struct {
		name string
		pod  corev1.PodStatus
		want bool
	}{
		{"CopyOut, provisioning initContainer", corev1.PodStatus{
			InitContainerStatuses: []corev1.ContainerStatus{pull("provision-helmfile"), pending("clone")},
			ContainerStatuses:     []corev1.ContainerStatus{pending("runner")},
		}, true},
		{"CopyOut, clone initContainer", corev1.PodStatus{
			InitContainerStatuses: []corev1.ContainerStatus{{Name: "provision-helmfile"}, pull("clone")},
			ContainerStatuses:     []corev1.ContainerStatus{pending("runner")},
		}, false},
		{"CopyOut, runner container", corev1.PodStatus{
			InitContainerStatuses: []corev1.ContainerStatus{{Name: "provision-helmfile"}, {Name: "clone"}},
			ContainerStatuses:     []corev1.ContainerStatus{pull("runner")},
		}, false},
		{"RunInImage, copy-runner initContainer", corev1.PodStatus{
			InitContainerStatuses: []corev1.ContainerStatus{pull("copy-runner"), pending("clone")},
			ContainerStatuses:     []corev1.ContainerStatus{pending("runner")},
		}, false},
		{"RunInImage, runner container", corev1.PodStatus{
			InitContainerStatuses: []corev1.ContainerStatus{{Name: "copy-runner"}, {Name: "clone"}},
			ContainerStatuses:     []corev1.ContainerStatus{pull("runner")},
		}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status := statusWithPod(t, tt.pod)
			assert.NotEmpty(t, status.PodReason)
			assert.Equal(t, tt.want, status.ToolWaiting)
		})
	}
}
