package common

import (
	"github.com/ivanvc/turnip/internal/adapters/github"
	"github.com/ivanvc/turnip/internal/config"
	"github.com/ivanvc/turnip/internal/services/kubernetes"
)

type Common struct {
	*config.Config
	KubernetesClient *kubernetes.Client
	GitHubClient     *github.Client
}
