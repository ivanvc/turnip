package orchestrator

import (
	"fmt"

	"github.com/ivanvc/turnip/internal/config"
)

// serviceAccountConfigKey is the turnip.yaml `config` key a Project uses
// to request a specific Kubernetes ServiceAccount for its Runner Job.
const serviceAccountConfigKey = "serviceAccount"

// ServiceAccountNotPermittedError reports a Project that requested a
// ServiceAccount while the Server has that override disabled.
type ServiceAccountNotPermittedError struct {
	Project        string
	ServiceAccount string
}

func (e *ServiceAccountNotPermittedError) Error() string {
	return fmt.Sprintf(
		"Project %q requested serviceAccount %q, which this turnip deployment does not permit. "+
			"Set TURNIP_RUNNER_SERVICE_ACCOUNT_ALLOW_FROM_CONFIG=true on the Server to let turnip.yaml choose its own ServiceAccount.",
		e.Project, e.ServiceAccount,
	)
}

// resolveServiceAccount decides which ServiceAccount a Project's Runner
// Job Pod runs as: the Project's own `config.serviceAccount` when the
// Server permits that override, otherwise the Server-wide default.
//
// The override is gated because turnip.yaml is read from the *pull
// request's own head commit* (configfetch.go), and a plan needs only
// collaborator access, not write access (target.go) — so without the gate
// anyone able to open a PR could pick any ServiceAccount in the Runner
// namespace and borrow its permissions. This mirrors Woodpecker CI's
// WOODPECKER_BACKEND_K8S_SERVICE_ACCOUNT_NAME_ALLOW_FROM_STEP, which is
// default-false for the same reason.
//
// An empty return value means "set nothing", leaving Kubernetes to apply
// the namespace's own default ServiceAccount.
func resolveServiceAccount(project config.Project, defaultServiceAccount string, allowFromConfig bool) (string, error) {
	requested := project.Config[serviceAccountConfigKey]
	if requested == "" {
		return defaultServiceAccount, nil
	}
	if !allowFromConfig {
		return "", &ServiceAccountNotPermittedError{Project: project.Name, ServiceAccount: requested}
	}
	return requested, nil
}
