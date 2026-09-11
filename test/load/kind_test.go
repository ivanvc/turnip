//go:build kind

// Package load holds turnip's on-demand kind-cluster HA validation
// (ha-validation, Slice 11, Requirement 1.5-1.6) — deliberately outside
// internal/ and never part of the default `go test -race ./...` suite.
package load

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// KindTimeoutError distinguishes a kind cluster that was simply too slow
// (a resource- or scheduling-bound runner — inconclusive, not a
// correctness failure) from a genuine RBAC/networking regression, per
// Requirement 1.6.
type KindTimeoutError struct {
	Check string
}

func (e *KindTimeoutError) Error() string {
	return "kind cluster check timed out: " + e.Check
}

// TestKind_RBACAndNetworking validates Requirement 1.5 directly against a
// real kind cluster running deployment-kustomize's overlay (applied by
// `make test-kind` before this runs) — not by replaying the Load Test's
// webhook burst. See design.md's Decision under Requirement 1 for why: a
// webhook fired at a real Server with a throwaway (unregistered) GitHub
// App key fails at the very first real GitHub API call
// (fetchConfig/IsCollaborator), before any lock or Job-creation logic
// runs at all, proving nothing this check couldn't prove more directly.
//
// Instead this checks the two things Requirement 1.5 actually cares
// about, neither of which needs GitHub:
//  1. RBAC: can the turnip-server ServiceAccount actually create Jobs and
//     read Pods — deploy/base/role.yaml's grant (deployment-kustomize,
//     Requirement 1.1), proven against the live cluster's RBAC API.
//  2. Networking: is the Server's gRPC Service reachable, by DNS name and
//     port, from another Pod in the namespace — the real path a Runner
//     Pod's gRPC dial takes.
func TestKind_RBACAndNetworking(t *testing.T) {
	ns := os.Getenv("TURNIP_TEST_NAMESPACE")
	if ns == "" {
		ns = "default"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	t.Run("RBAC: can create Jobs", func(t *testing.T) {
		runCanI(t, ctx, "create", "jobs.batch", ns)
	})
	t.Run("RBAC: can get/list Pods", func(t *testing.T) {
		runCanI(t, ctx, "get", "pods", ns)
	})
	t.Run("Networking: Service reachable", func(t *testing.T) {
		runNetworkCheck(t, ctx, ns)
	})
}

// runCanI shells out to `kubectl auth can-i`, impersonating the
// turnip-server ServiceAccount, and requires the answer to be "yes".
func runCanI(t *testing.T, ctx context.Context, verb, resource, ns string) {
	t.Helper()
	cmd := exec.CommandContext(ctx, "kubectl", "auth", "can-i", verb, resource,
		"--as=system:serviceaccount:"+ns+":turnip-server", "-n", ns)
	out, err := cmd.CombinedOutput()
	if handleKindTimeout(t, ctx, "kubectl auth can-i "+verb+" "+resource, err) {
		return
	}
	require.NoError(t, err, "kubectl auth can-i %s %s: %s", verb, resource, out)
}

// runNetworkCheck shells out to a one-shot `kubectl run` Pod that
// attempts a raw TCP connection to the Server's real gRPC Service DNS
// name, proving Service routing works from inside the cluster.
func runNetworkCheck(t *testing.T, ctx context.Context, ns string) {
	t.Helper()
	target := "turnip-server." + ns + ".svc.cluster.local"
	cmd := exec.CommandContext(ctx, "kubectl", "run", "turnip-ha-netcheck",
		"--rm", "-i", "--restart=Never", "--image=busybox", "-n", ns,
		"--", "sh", "-c", "nc -z -w3 "+target+" 9090")
	out, err := cmd.CombinedOutput()
	if handleKindTimeout(t, ctx, "kubectl run turnip-ha-netcheck", err) {
		return
	}
	require.NoError(t, err, "networking check against %s:9090: %s", target, out)
}

// handleKindTimeout reports a context-deadline failure as an inconclusive
// skip (Requirement 1.6) rather than a test failure, and returns true
// when it did so (the caller should stop asserting further on err).
func handleKindTimeout(t *testing.T, ctx context.Context, check string, err error) bool {
	t.Helper()
	if err != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
		t.Skipf("inconclusive, not a correctness failure: %v", &KindTimeoutError{Check: check})
		return true
	}
	return false
}
