//go:build kind

package load

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/ivanvc/turnip/internal/config"
	"github.com/ivanvc/turnip/internal/jobs"
	"github.com/ivanvc/turnip/internal/provisioning"
)

// TestKind_JobDeadline checks, against a real Job controller, that a Job
// BuildJob emits with a short deadline and a container that never
// finishes ends Failed with reason DeadlineExceeded, that its Pod carries
// the 60-second grace, and that jobs.Status reads the Job as the sweep's
// diagnostic needs ("Job <name>: stopped at its deadline of <deadline>").
//
// The Job is BuildJob's own, with the clone init container removed and
// the runner container's command replaced by a sleep, so the test needs
// no repository, Server or tool image.
func TestKind_JobDeadline(t *testing.T) {
	ns := os.Getenv("TURNIP_TEST_RUNNER_NAMESPACE")
	if ns == "" {
		ns = "turnip-runners"
	}
	const deadline = 5 * time.Second

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	restCfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, &clientcmd.ConfigOverrides{}).ClientConfig()
	require.NoError(t, err)
	clientset, err := kubernetes.NewForConfig(restCfg)
	require.NoError(t, err)
	client := jobs.NewClient(clientset, ns)

	job, err := jobs.BuildJob(config.Project{Name: "project", Directory: "project", Tool: "terraform"}, jobs.OperationParams{
		OperationID:  "op-deadline",
		Operation:    "plan",
		RepoURL:      "https://github.com/example/project.git",
		CommitSHA:    "abc123",
		BaseRef:      "main",
		ServerAddr:   "turnip-server:9090",
		RunnerImage:  secretsTestImage,
		Provisioning: provisioning.Spec{Strategy: provisioning.CopyOut, Image: "hashicorp/terraform", BinaryPath: "/bin/terraform"},
		ImageRef:     "hashicorp/terraform:1.9.0",
		Deadline:     deadline,
	})
	require.NoError(t, err)
	require.NotNil(t, job.Spec.ActiveDeadlineSeconds)
	require.EqualValues(t, 5, *job.Spec.ActiveDeadlineSeconds)

	spec := &job.Spec.Template.Spec
	spec.InitContainers = nil
	spec.Volumes = nil
	runner := &spec.Containers[0]
	runner.Image = secretsTestImage
	runner.ImagePullPolicy = corev1.PullIfNotPresent
	runner.Command = []string{"sleep", "3600"}
	runner.Args = nil
	runner.VolumeMounts = nil

	created, err := client.Create(ctx, job)
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = client.Delete(cleanupCtx, created.Name)
	})

	// Watch the Pod's spec while the Job runs: the controller deletes the
	// Pod once the deadline passes.
	var graceSeen *int64
	var final *batchv1.Job
	for final == nil {
		select {
		case <-ctx.Done():
			handleKindTimeout(t, ctx, "Job "+created.Name+" reaching its deadline", ctx.Err())
			require.NoError(t, ctx.Err())
		case <-time.After(time.Second):
		}
		pods, err := clientset.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{LabelSelector: "job-name=" + created.Name})
		require.NoError(t, err)
		for _, p := range pods.Items {
			graceSeen = p.Spec.TerminationGracePeriodSeconds
		}
		got, err := clientset.BatchV1().Jobs(ns).Get(ctx, created.Name, metav1.GetOptions{})
		require.NoError(t, err)
		for _, c := range got.Status.Conditions {
			if c.Type == batchv1.JobFailed && c.Status == corev1.ConditionTrue {
				final = got
				assert.Equal(t, "DeadlineExceeded", c.Reason)
			}
		}
	}

	require.NotNil(t, graceSeen, "no Pod was seen before the deadline stopped the Job")
	assert.EqualValues(t, 60, *graceSeen, "Pod's termination grace period")

	status, err := client.Status(ctx, created.Name)
	require.NoError(t, err)
	assert.True(t, status.JobFound)
	assert.Equal(t, "DeadlineExceeded", status.FailedReason)
	assert.Equal(t, deadline, status.Deadline)

	// The sweep renders these two fields as this line
	// (orchestrator.stoppedDiagnostic, unexported).
	assert.Equal(t, "Job "+created.Name+": stopped at its deadline of 5s",
		fmt.Sprintf("Job %s: stopped at its deadline of %s", created.Name, status.Deadline))
}
