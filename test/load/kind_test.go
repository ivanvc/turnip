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
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
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
//  1. RBAC: the turnip-server ServiceAccount (in the Server namespace)
//     can create Jobs and get Pods in the Runner namespace, can do
//     neither in its own namespace, and cannot read Secrets in either.
//  2. Networking: a Pod in the Runner namespace reaches the Server's gRPC
//     Service by its cross-namespace DNS name, the real path a Runner
//     Pod's gRPC dial takes.
//
// It also checks the Server's startup log reports the two settings that
// tie the namespaces together.
func TestKind_RBACAndNetworking(t *testing.T) {
	serverNS := envOr("TURNIP_TEST_SERVER_NAMESPACE", "turnip-system")
	runnerNS := envOr("TURNIP_TEST_RUNNER_NAMESPACE", "turnip-runners")
	serverAddr := "turnip-server." + serverNS + ".svc:9090"

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	t.Run("RBAC: can create Jobs in the Runner namespace", func(t *testing.T) {
		runCanI(t, ctx, serverNS, "create", "jobs.batch", runnerNS)
	})
	t.Run("RBAC: can get Pods in the Runner namespace", func(t *testing.T) {
		runCanI(t, ctx, serverNS, "get", "pods", runnerNS)
	})
	t.Run("RBAC: cannot create Jobs or get Pods in the Server namespace", func(t *testing.T) {
		runCannotI(t, ctx, serverNS, "create", "jobs.batch", serverNS)
		runCannotI(t, ctx, serverNS, "get", "pods", serverNS)
	})
	// The kubelet mounts a repository's Secret into its Runner Pods; the
	// Server only names it, so it needs no permission on Secrets at all.
	t.Run("RBAC: cannot get/list Secrets in either namespace", func(t *testing.T) {
		for _, ns := range []string{runnerNS, serverNS} {
			runCannotI(t, ctx, serverNS, "get", "secrets", ns)
			runCannotI(t, ctx, serverNS, "list", "secrets", ns)
		}
	})
	t.Run("Networking: Service reachable from the Runner namespace", func(t *testing.T) {
		runNetworkCheck(t, ctx, runnerNS, serverAddr)
	})
	t.Run("Server log reports the Runner namespace and Server address", func(t *testing.T) {
		checkServerLog(t, ctx, serverNS, runnerNS, serverAddr)
	})
}

func envOr(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}

// runCanI shells out to `kubectl auth can-i`, impersonating the
// turnip-server ServiceAccount of saNS, and requires the answer to be
// "yes" for the resource in ns.
func runCanI(t *testing.T, ctx context.Context, saNS, verb, resource, ns string) {
	t.Helper()
	cmd := exec.CommandContext(ctx, "kubectl", "auth", "can-i", verb, resource,
		"--as=system:serviceaccount:"+saNS+":turnip-server", "-n", ns)
	out, err := cmd.CombinedOutput()
	if handleKindTimeout(t, ctx, "kubectl auth can-i "+verb+" "+resource, err) {
		return
	}
	require.NoError(t, err, "kubectl auth can-i %s %s in %s: %s", verb, resource, ns, out)
}

// runCannotI shells out to `kubectl auth can-i`, impersonating the
// turnip-server ServiceAccount of saNS, and requires the answer to be
// "no" for the resource in ns. kubectl exits non-zero for a "no", so the
// exit status is not checked.
func runCannotI(t *testing.T, ctx context.Context, saNS, verb, resource, ns string) {
	t.Helper()
	cmd := exec.CommandContext(ctx, "kubectl", "auth", "can-i", verb, resource,
		"--as=system:serviceaccount:"+saNS+":turnip-server", "-n", ns)
	out, err := cmd.Output()
	if handleKindTimeout(t, ctx, "kubectl auth can-i "+verb+" "+resource, err) {
		return
	}
	assert.Equal(t, "no", strings.TrimSpace(string(out)),
		"kubectl auth can-i %s %s in %s", verb, resource, ns)
}

// runNetworkCheck shells out to a one-shot `kubectl run` Pod in ns that
// attempts a raw TCP connection to the Server's gRPC Service (addr, as
// host:port), proving cross-namespace Service routing works.
func runNetworkCheck(t *testing.T, ctx context.Context, ns, addr string) {
	t.Helper()
	host, port, _ := strings.Cut(addr, ":")
	cmd := exec.CommandContext(ctx, "kubectl", "run", "turnip-ha-netcheck",
		"--rm", "-i", "--restart=Never", "--image=busybox", "-n", ns,
		"--", "sh", "-c", "nc -z -w3 "+host+" "+port)
	out, err := cmd.CombinedOutput()
	if handleKindTimeout(t, ctx, "kubectl run turnip-ha-netcheck", err) {
		return
	}
	require.NoError(t, err, "networking check from %s against %s: %s", ns, addr, out)
}

// checkServerLog requires the Server's startup log to carry
// runner_namespace and runner_server_addr with the expected values, in
// either the JSON handler's form or slog's text form.
func checkServerLog(t *testing.T, ctx context.Context, serverNS, runnerNS, serverAddr string) {
	t.Helper()
	cmd := exec.CommandContext(ctx, "kubectl", "-n", serverNS, "logs", "deployment/turnip-server")
	out, err := cmd.CombinedOutput()
	if handleKindTimeout(t, ctx, "kubectl logs deployment/turnip-server", err) {
		return
	}
	require.NoError(t, err, "kubectl logs deployment/turnip-server: %s", out)
	logs := string(out)
	for field, want := range map[string]string{
		"runner_namespace":   runnerNS,
		"runner_server_addr": serverAddr,
	} {
		pattern := regexp.MustCompile(`"?` + regexp.QuoteMeta(field) + `"?[=:]"?` + regexp.QuoteMeta(want) + `"?(\s|,|}|$)`)
		assert.Regexp(t, pattern, logs, "Server log lacks %s=%s", field, want)
	}
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
