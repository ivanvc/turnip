package kubernetes

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/log"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8s "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/ivanvc/ares/internal/adapters/github"
)

// Client holds a wrapped Kubernetes client.
type Client struct {
	*k8s.Clientset
	config    *rest.Config
	namespace string
}

// LoadClient creates a new Client singleton.
func LoadClient(namespace string) *Client {
	cfg, err := ctrl.GetConfig()
	if err != nil {
		log.Fatal("Error loading kubeconfig", "error", err)
	}
	cs, err := k8s.NewForConfig(cfg)
	if err != nil {
		log.Fatal("Error initializing Kubernetes client", "error", err)
	}
	return &Client{cs, cfg, namespace}
}

func (c *Client) CreateJob(ic *github.IssueComment) error {
	if _, err := c.BatchV1().Jobs(c.namespace).Create(
		context.Background(),
		getJob(c.namespace, ic),
		metav1.CreateOptions{},
	); err != nil {
		return err
	}

	return nil
}

func getJob(namespace string, ic *github.IssueComment) *batchv1.Job {
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: fmt.Sprintf("ares-job-%s-", strings.ReplaceAll(strings.ToLower(ic.Comment.NodeID), "_", "-")),
			Namespace:    namespace,
			Labels:       map[string]string{},
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "ares-client",
							Image: "ivan/ares:latest",
							Args: []string{"/usr/local/go/bin/go",
								"run", "cmd/client/main.go",
								//fmt.Sprintf("git clone %s repo && cd repo && terraform init . && terraform plan", ic.Repository.CloneURL),
							},
							Env: []corev1.EnvVar{
								{
									Name:  "ARES_CLONE_URL",
									Value: ic.Repository.CloneURL,
								},
							},
						},
					},
					RestartPolicy: corev1.RestartPolicyNever,
				},
			},
		},
	}
}
