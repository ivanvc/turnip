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

	"github.com/ivanvc/turnip/internal/adapters/github"
	"github.com/ivanvc/turnip/internal/config"
)

// Client holds a wrapped Kubernetes client.
type Client struct {
	*k8s.Clientset
	config      *rest.Config
	namespace   string
	githubToken string
}

// LoadClient creates a new Client singleton.
func LoadClient(config *config.Config) *Client {
	cfg, err := ctrl.GetConfig()
	if err != nil {
		log.Fatal("Error loading kubeconfig", "error", err)
	}
	cs, err := k8s.NewForConfig(cfg)
	if err != nil {
		log.Fatal("Error initializing Kubernetes client", "error", err)
	}
	return &Client{cs, cfg, config.Namespace, config.GitHubToken}
}

func (c *Client) CreateJob(ic *github.IssueComment) error {
	if _, err := c.BatchV1().Jobs(c.namespace).Create(
		context.Background(),
		getJob(c.namespace, c.githubToken, ic),
		metav1.CreateOptions{},
	); err != nil {
		return err
	}

	return nil
}

func getJob(namespace, token string, ic *github.IssueComment) *batchv1.Job {
	payload, err := ic.ToJSON()
	if err != nil {
		log.Error("Error generating payload JSON", "error", err)
		return nil
	}
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: fmt.Sprintf("turnip-job-%s-", strings.ReplaceAll(strings.ToLower(ic.Comment.NodeID), "_", "-")),
			Namespace:    namespace,
			Labels:       map[string]string{},
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:            "turnip-client",
							Image:           "ivan/turnip:latest",
							ImagePullPolicy: corev1.PullAlways,
							Args: []string{"/usr/local/go/bin/go",
								"run", "cmd/client/main.go",
								//fmt.Sprintf("git clone %s repo && cd repo && terraform init . && terraform plan", ic.Repository.CloneURL),
							},
							Env: []corev1.EnvVar{
								{
									Name:  "TURNIP_PAYLOAD",
									Value: string(payload),
								},
								{
									Name:  "TURNIP_GITHUB_TOKEN",
									Value: token,
								},
								{
									Name:  "TURNIP_COMMAND",
									Value: "plan",
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
