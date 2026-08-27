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
