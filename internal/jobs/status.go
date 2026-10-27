package jobs

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// JobStatus reports what's known about a Runner Job's Kubernetes state,
// used to diagnose why a Job never started (Requirement 8.4), and why a
// started Runner's Job ended without a result.
type JobStatus struct {
	JobFound   bool
	Active     int32
	Succeeded  int32
	Failed     int32
	PodPhase   corev1.PodPhase // "" if no Pod found yet
	PodReason  string          // e.g. "ImagePullBackOff", "" if none
	PodMessage string
	// ToolWaiting reports whether the container PodReason comes from is
	// the one running the tool's image (see toolImageID), rather than one
	// running turnip's own Runner image. Only then does an image-pull
	// failure concern the tool image.
	ToolWaiting bool
	// ImageID is the imageID Kubernetes reports for the container that
	// ran the tool's image: the CopyOut provisioning initContainer, or
	// the runner container under RunInImage. Empty until the runtime has
	// pulled it.
	ImageID string

	// Finished reports whether the Job carries a Failed or Complete
	// condition: its Pod has ended, so a Runner that has not reported by
	// now never will.
	Finished bool
	// FailedReason is the reason of the Job's Failed condition, such as
	// "DeadlineExceeded", or, before that condition is set, of its
	// FailureTarget condition (which the Job controller sets first while
	// it terminates the Pod); empty while the Job has not failed.
	FailedReason string
	// Deadline is the Job's activeDeadlineSeconds, zero when it has none,
	// so a deadline diagnostic names the deadline the Job actually had.
	Deadline time.Duration
	// TerminatedContainer and TerminatedReason name the first container
	// that terminated with a non-zero exit code, init containers first,
	// and the reason Kubernetes gave (such as "OOMKilled" or "Error").
	TerminatedContainer string
	TerminatedReason    string
	// Evicted reports whether the Pod was evicted.
	Evicted bool
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
	if d := job.Spec.ActiveDeadlineSeconds; d != nil {
		status.Deadline = time.Duration(*d) * time.Second
	}
	for _, c := range job.Status.Conditions {
		if c.Status != corev1.ConditionTrue {
			continue
		}
		switch c.Type {
		case batchv1.JobFailed:
			status.Finished = true
			status.FailedReason = c.Reason
		case batchv1.JobComplete:
			status.Finished = true
		case batchv1.JobFailureTarget:
			// Not Finished: the Pod may still be terminating. Its reason
			// still names why, for a tick that finds the Pod gone before
			// the Failed condition is set.
			status.FailedReason = cmp.Or(status.FailedReason, c.Reason)
		}
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
	status.ImageID = toolImageID(pod.Status)

	// Init-container statuses are checked before regular container
	// statuses: the tool-provisioning initContainer (Requirement 8.1) is
	// the more likely place for an ImagePullBackOff than the Runner's own,
	// already-published image.
	cs, ok := firstWaiting(pod.Status.InitContainerStatuses)
	if !ok {
		cs, ok = firstWaiting(pod.Status.ContainerStatuses)
	}
	if ok {
		status.PodReason, status.PodMessage = cs.State.Waiting.Reason, cs.State.Waiting.Message
		status.ToolWaiting = cs.Name == toolContainerName(pod.Status)
	}

	cs, ok = firstTerminated(pod.Status.InitContainerStatuses)
	if !ok {
		cs, ok = firstTerminated(pod.Status.ContainerStatuses)
	}
	if ok {
		status.TerminatedContainer, status.TerminatedReason = cs.Name, cs.State.Terminated.Reason
	}
	status.Evicted = pod.Status.Reason == "Evicted"

	return status, nil
}

func firstWaiting(statuses []corev1.ContainerStatus) (corev1.ContainerStatus, bool) {
	for _, cs := range statuses {
		if cs.State.Waiting != nil {
			return cs, true
		}
	}
	return corev1.ContainerStatus{}, false
}

// firstTerminated finds the first container that terminated with a
// non-zero exit code. One that exited 0 (the clone, once it has checked
// out) ended as it should and explains nothing.
func firstTerminated(statuses []corev1.ContainerStatus) (corev1.ContainerStatus, bool) {
	for _, cs := range statuses {
		if t := cs.State.Terminated; t != nil && t.ExitCode != 0 {
			return cs, true
		}
	}
	return corev1.ContainerStatus{}, false
}

// toolImageID finds the container that ran the tool's image. BuildJob
// gives a CopyOut Pod a provisioning initContainer, whose image is the
// tool's; without one, the Pod is RunInImage and the runner container
// runs the tool's image itself.
func toolImageID(status corev1.PodStatus) string {
	name := toolContainerName(status)
	for _, cs := range slices.Concat(status.InitContainerStatuses, status.ContainerStatuses) {
		if cs.Name == name {
			return cs.ImageID
		}
	}
	return ""
}

// toolContainerName names the container that runs the tool's image: the
// CopyOut provisioning initContainer when the Pod has one, and otherwise
// (RunInImage) the runner container.
func toolContainerName(status corev1.PodStatus) string {
	for _, cs := range status.InitContainerStatuses {
		if strings.HasPrefix(cs.Name, provisionContainerPrefix) {
			return cs.Name
		}
	}
	return runnerContainerName
}
