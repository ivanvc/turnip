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

	"github.com/ivanvc/turnip/internal/config"
	"github.com/ivanvc/turnip/internal/yaml"
)

// Client holds a wrapped Kubernetes client.
type Client struct {
	*k8s.Clientset
	config      *rest.Config
	namespace   string
	githubToken string
	serverName  string
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
	return &Client{cs, cfg, config.Namespace, config.GitHubToken, config.ServerName}
}

func (c *Client) CreateJob(command, cloneURL, baseRef, repoFullName, checkURL string, project *yaml.Project) error {
	if _, err := c.BatchV1().Jobs(c.namespace).Create(
		context.Background(),
		getJob(c.namespace, c.githubToken, c.serverName, command, cloneURL, baseRef, repoFullName, checkURL, project),
		metav1.CreateOptions{},
	); err != nil {
		return err
	}

	return nil
}

func getJob(namespace, token, serverName, command, cloneURL, baseRef, repoFullName, checkURL string, project *yaml.Project) *batchv1.Job {
	projectYAML := marshalProjectYAML(project)
	generatedName := getGeneratedName(command, repoFullName, project)

	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: generatedName,
			Namespace:    namespace,
			Labels: map[string]string{
				"app":                    "turnip",
				"turnip.ivan.vc/repo":    repoFullName,
				"turnip.ivan.vc/command": command,
			},
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
							},
							Env: []corev1.EnvVar{
								{
									Name:  "TURNIP_CLONE_URL",
									Value: cloneURL,
								},
								{
									Name:  "TURNIP_BASE_REF",
									Value: baseRef,
								},
								{
									Name:  "TURNIP_COMMAND",
									Value: command,
								},
								{
									Name:  "TURNIP_CHECK_URL",
									Value: checkURL,
								},
								{
									Name:  "TURNIP_PROJECT_YAML",
									Value: string(projectYAML),
								},
								{
									Name:  "TURNIP_SERVER_NAME",
									Value: serverName,
								},
								// TODO: Move to a secret
								{
									Name:  "TURNIP_GITHUB_TOKEN",
									Value: token,
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

func getGeneratedName(command, repoFullName string, project *yaml.Project) string {
	r := strings.NewReplacer("/", "-", "_", "-")
	nameTpl := fmt.Sprintf("turnip-%s-%s-",
		command,
		r.Replace(strings.ToLower(repoFullName)),
	)
	if project != nil {
		nameTpl = fmt.Sprintf("%s-%s-", nameTpl, project.Dir)
	}
	if len(nameTpl) > 47 {
		nameTpl = fmt.Sprintf("%s-", strings.TrimSuffix(nameTpl[:47], "-"))
	}
	return nameTpl
}

func marshalProjectYAML(project *yaml.Project) []byte {
	result, err := project.ToYAML()
	if err != nil {
		log.Error("error marshaling project YAML", "error", err)
	}
	return result
}
