package jobs

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// JobStatus reports what's known about a Runner Job's Kubernetes state,
// used to diagnose why a Job never started (Requirement 8.4).
type JobStatus struct {
	JobFound   bool
	Active     int32
	Succeeded  int32
	Failed     int32
	PodPhase   corev1.PodPhase // "" if no Pod found yet
	PodReason  string          // e.g. "ImagePullBackOff", "" if none
	PodMessage string
}

// Status looks up jobName's Job and, if found, its Pod's status. A
// not-found Job is not an error — it's reported as JobStatus{JobFound:
// false}, since "the Job doesn't exist" is itself diagnostic information
// for a caller reporting a start timeout.
func (c *Client) Status(ctx context.Context, jobName string) (*JobStatus, error) {
	job, err := c.jobs.Get(ctx, jobName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return &JobStatus{JobFound: false}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("jobs: getting status for %q: %w", jobName, err)
	}

	status := &JobStatus{
		JobFound:  true,
		Active:    job.Status.Active,
		Succeeded: job.Status.Succeeded,
		Failed:    job.Status.Failed,
	}

	// "job-name" is the Job controller's long-standing label, superseded
	// for namespacing purposes (not compatibility) by
	// "batch.kubernetes.io/job-name" in Kubernetes 1.27 — this platform
	// targets only currently-supported clusters, so the qualified form is
	// used outright rather than for any old-version compatibility reason.
	pods, err := c.pods.List(ctx, metav1.ListOptions{
		LabelSelector: "batch.kubernetes.io/job-name=" + jobName,
	})
	if err != nil {
		return nil, fmt.Errorf("jobs: listing pods for %q: %w", jobName, err)
	}
	if len(pods.Items) == 0 {
		return status, nil
	}

	// BackoffLimit: 0 (build.go) guarantees at most one Pod per Job.
	pod := pods.Items[0]
	status.PodPhase = pod.Status.Phase

	// Init-container statuses are checked before regular container
	// statuses: the tool-provisioning initContainer (Requirement 8.1) is
	// the more likely place for an ImagePullBackOff than the Runner's own,
	// already-published image.
	if reason, message, ok := firstWaiting(pod.Status.InitContainerStatuses); ok {
		status.PodReason, status.PodMessage = reason, message
		return status, nil
	}
	if reason, message, ok := firstWaiting(pod.Status.ContainerStatuses); ok {
		status.PodReason, status.PodMessage = reason, message
	}

	return status, nil
}

func firstWaiting(statuses []corev1.ContainerStatus) (reason, message string, ok bool) {
	for _, cs := range statuses {
		if cs.State.Waiting != nil {
			return cs.State.Waiting.Reason, cs.State.Waiting.Message, true
		}
	}
	return "", "", false
}
