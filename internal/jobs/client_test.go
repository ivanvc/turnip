package jobs

import (
	"context"
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stesting "k8s.io/client-go/testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"k8s.io/client-go/kubernetes/fake"
)

func TestClient_CreateCallsThroughAndReturnsCreatedJob(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	client := NewClient(clientset, "turnip")

	job := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "turnip-runner-abc"}}
	created, err := client.Create(context.Background(), job)
	require.NoError(t, err)
	assert.Equal(t, "turnip-runner-abc", created.Name)

	got, err := clientset.BatchV1().Jobs("turnip").Get(context.Background(), "turnip-runner-abc", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "turnip-runner-abc", got.Name)
}

func TestClient_DeletePassesBackgroundPropagationPolicy(t *testing.T) {
	clientset := fake.NewSimpleClientset(&batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "turnip-runner-abc", Namespace: "turnip"},
	})
	client := NewClient(clientset, "turnip")

	require.NoError(t, client.Delete(context.Background(), "turnip-runner-abc"))

	var deleteAction k8stesting.DeleteActionImpl
	found := false
	for _, action := range clientset.Actions() {
		if a, ok := action.(k8stesting.DeleteActionImpl); ok && a.GetName() == "turnip-runner-abc" {
			deleteAction = a
			found = true
		}
	}
	require.True(t, found, "expected a delete action for turnip-runner-abc")
	require.NotNil(t, deleteAction.DeleteOptions.PropagationPolicy)
	assert.Equal(t, metav1.DeletePropagationBackground, *deleteAction.DeleteOptions.PropagationPolicy)
}
