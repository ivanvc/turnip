package orchestrator

import (
	"fmt"

	"github.com/ivanvc/turnip/internal/config"
)

// ServiceAccountNotPermittedError reports a Project whose Effective_Runner
// takes a ServiceAccount from turnip.yaml while the Server has that
// override disabled. Source says which block set it, so the message can
// send the author to the block they have to edit, which need not be the
// Project's own.
type ServiceAccountNotPermittedError struct {
	Project        string
	ServiceAccount string
	Source         config.ServiceAccountSource
}

func (e *ServiceAccountNotPermittedError) Error() string {
	var what string
	if e.Source == config.ServiceAccountSourceTopLevel {
		what = fmt.Sprintf("Project %q inherits runner.serviceAccount %q from the top-level runner: block",
			e.Project, e.ServiceAccount)
	} else {
		what = fmt.Sprintf("Project %q requested runner.serviceAccount %q", e.Project, e.ServiceAccount)
	}
	return fmt.Sprintf(
		"%s, which this turnip deployment does not permit. "+
			"Add %q to TURNIP_ALLOWED_OVERRIDES on the Server to let turnip.yaml choose its own ServiceAccount.",
		what, overrideServiceAccount,
	)
}

// resolveServiceAccount decides which ServiceAccount a Project's Runner
// Job Pod runs as: the `runner.serviceAccount` of the Project's
// Effective_Runner (its own, or inherited from the top-level block) when
// the Server permits that override, otherwise the Server-wide default. The
// Project's Runner is already the Effective_Runner (config.Parse merges
// the top-level block in), so a value inherited from the top level is
// gated exactly like one the Project sets itself.
//
// The override is gated because turnip.yaml is read from the *pull
// request's own head commit* (configfetch.go), and a plan needs only
// collaborator access, not write access (target.go) — so without the gate
// anyone able to open a PR could pick any ServiceAccount in the Runner
// namespace and borrow its permissions. This mirrors Woodpecker CI's
// WOODPECKER_BACKEND_K8S_SERVICE_ACCOUNT_NAME_ALLOW_FROM_STEP, and
// Atlantis' warning that repo-defined workflows let anyone who can open a
// pull request run arbitrary code on the server.
//
// An empty return value means "set nothing", leaving Kubernetes to apply
// the namespace's own default ServiceAccount.
func resolveServiceAccount(project config.Project, defaultServiceAccount string, allowed map[string]bool) (string, error) {
	requested := project.Runner.ServiceAccount
	if requested == "" {
		return defaultServiceAccount, nil
	}
	if !allowed[overrideServiceAccount] {
		return "", &ServiceAccountNotPermittedError{
			Project:        project.Name,
			ServiceAccount: requested,
			Source:         project.ServiceAccountSource,
		}
	}
	return requested, nil
}
