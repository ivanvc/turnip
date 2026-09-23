package orchestrator

import (
	"context"
	"fmt"

	"github.com/ivanvc/turnip/internal/github"
	"github.com/ivanvc/turnip/internal/rpc"
)

// CloneCredential mints the GitHub credential for one Operation's clone,
// at the moment its Runner asks for it.
//
// operationID comes from what the gRPC interceptor authenticated, never
// from the request — see rpc.FetchCloneCredential. Everything else is
// read from the Operation's own record, so a caller cannot influence the
// installation, the repository or the scope.
//
// Minting here rather than at dispatch is Requirement 2.4: the credential
// stops being live through queueing, scheduling, image pulls and tool
// provisioning, and is instead live for the clone that uses it.
func (o *Orchestrator) CloneCredential(ctx context.Context, operationID string) (rpc.CloneCredential, error) {
	rec, err := o.records.get(ctx, operationID)
	if err != nil {
		return rpc.CloneCredential{}, fmt.Errorf("orchestrator: reading operation %s: %w", operationID, err)
	}
	if rec == nil {
		// The Operation is gone — swept, or its record expired. There is
		// nothing to mint a credential for, and minting one anyway would
		// hand out a token with no Operation accountable for it.
		return rpc.CloneCredential{}, fmt.Errorf("orchestrator: no record for operation %s", operationID)
	}

	client := o.installationClient(rec.InstallationID)
	token, err := client.GenerateInstallationToken(ctx, github.TokenScope{Repositories: rec.TokenRepositories})
	if err != nil {
		return rpc.CloneCredential{}, fmt.Errorf("orchestrator: minting clone credential for %s: %w", operationID, err)
	}

	return rpc.CloneCredential{Token: token.Token, ExpiresAt: token.ExpiresAt}, nil
}
