package jobs

import (
	"context"

	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	batchv1client "k8s.io/client-go/kubernetes/typed/batch/v1"
	"k8s.io/utils/ptr"
)

// Client creates and deletes Runner Jobs in one Kubernetes namespace.
type Client struct {
	jobs batchv1client.JobInterface
}

// NewClient constructs a Client backed by clientset, scoped to namespace.
func NewClient(clientset kubernetes.Interface, namespace string) *Client {
	return &Client{jobs: clientset.BatchV1().Jobs(namespace)}
}

// Create submits job to the cluster and returns the created object
// (Requirement 7.1).
func (c *Client) Create(ctx context.Context, job *batchv1.Job) (*batchv1.Job, error) {
	return c.jobs.Create(ctx, job, metav1.CreateOptions{})
}

// Delete removes the named Job immediately, ahead of its TTL — the
// supplementary eager-cleanup path Requirement 9.2 describes, never the
// path anything else in this slice depends on for correctness. It passes
// PropagationPolicy explicitly rather than relying on the API server's
// default, so the Job's Pods are always removed alongside it (Requirement
// 9.3).
func (c *Client) Delete(ctx context.Context, name string) error {
	return c.jobs.Delete(ctx, name, metav1.DeleteOptions{
		PropagationPolicy: ptr.To(metav1.DeletePropagationBackground),
	})
}
