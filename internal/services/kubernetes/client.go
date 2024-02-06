package kubernetes

import (
	"context"
	"encoding/json"
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

var (
	stringNormalizer = strings.NewReplacer("/", "-", "_", "-")
)

// Client holds a wrapped Kubernetes client.
type Client struct {
	*k8s.Clientset
	config         *rest.Config
	namespace      string
	githubToken    string
	serverName     string
	jobSecrets     string
	podAnnotations map[string]string
}

// LoadClient creates a new Client singleton.
func LoadClient(config *config.Config) *Client {
	cfg, err := ctrl.GetConfig()
	if err != nil {
		log.Fatal("error loading kubeconfig", "error", err)
	}
	cs, err := k8s.NewForConfig(cfg)
	if err != nil {
		log.Fatal("error initializing Kubernetes client", "error", err)
	}
	var podAnnotations map[string]string
	if err := json.Unmarshal([]byte(config.JobPodAnnotations), &podAnnotations); err != nil {
		log.Fatal("error unmarshaling pod annotations", "error", err)
	}
	return &Client{
		Clientset:      cs,
		config:         cfg,
		namespace:      config.Namespace,
		githubToken:    config.GitHubToken,
		serverName:     config.ServerName,
		jobSecrets:     config.JobSecretsName,
		podAnnotations: podAnnotations,
	}
}

func (c *Client) CreateJob(command, cloneURL, headRef, repoFullName, checkURL, checkName, commentsURL string, project *yaml.Project) error {
	if _, err := c.BatchV1().Jobs(c.namespace).Create(
		context.Background(),
		getJob(c.namespace, c.githubToken, c.serverName, c.jobSecrets, command, cloneURL, headRef, repoFullName, checkURL, checkName, commentsURL, project, c.podAnnotations),
		metav1.CreateOptions{},
	); err != nil {
		return err
	}

	return nil
}

func getJob(namespace, token, serverName, jobSecrets, command, cloneURL, headRef, repoFullName, checkURL, checkName, commentsURL string, project *yaml.Project, podAnnotations map[string]string) *batchv1.Job {
	projectYAML := marshalProjectYAML(project)
	generatedName := getGeneratedName(command, repoFullName, project)

	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: generatedName,
			Namespace:    namespace,
			Labels: map[string]string{
				"app":                    "turnip",
				"turnip.ivan.vc/repo":    stringNormalizer.Replace(repoFullName),
				"turnip.ivan.vc/command": command,
			},
			Annotations: podAnnotations,
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:            "turnip-client",
							Image:           "ivan/turnip:latest",
							ImagePullPolicy: corev1.PullAlways,
							Args:            []string{"/opt/turnip/bin/xl-15"},
							Env: []corev1.EnvVar{
								{
									Name:  "TURNIP_CLONE_URL",
									Value: cloneURL,
								},
								{
									Name:  "TURNIP_HEAD_REF",
									Value: headRef,
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
									Name:  "TURNIP_CHECK_NAME",
									Value: checkName,
								},
								{
									Name:  "TURNIP_PROJECT_YAML",
									Value: string(projectYAML),
								},
								{
									Name:  "TURNIP_SERVER_NAME",
									Value: serverName,
								},
								{
									Name:  "TURNIP_COMMENTS_URL",
									Value: commentsURL,
								},
							},
							EnvFrom: []corev1.EnvFromSource{
								{
									SecretRef: &corev1.SecretEnvSource{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: jobSecrets,
										},
									},
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
	nameTpl := fmt.Sprintf("turnip-%s-%s-",
		command,
		stringNormalizer.Replace(strings.ToLower(repoFullName)),
	)
	if project != nil {
		nameTpl = fmt.Sprintf("%s%s-", nameTpl, project.Dir)
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
