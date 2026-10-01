package runnerauth_test

// The checkpoint for separate-clone-identity (tasks.md 3.1).
//
// The Job builder's tests prove which audience each container mounts; the
// authenticator's tests prove which identity each audience becomes; the
// rpc tests prove which identity each method accepts. Each can pass while
// the three disagree. This test joins them: it builds the real Runner
// Job, takes the audience each container actually mounts, mints a token
// for exactly that audience, and presents it to the real authenticated
// gRPC Server backed by the real runnerauth.Authenticator.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"path"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	authv1 "k8s.io/api/authentication/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/ivanvc/turnip/internal/config"
	pb "github.com/ivanvc/turnip/internal/grpc/turnip/v1"
	"github.com/ivanvc/turnip/internal/jobs"
	"github.com/ivanvc/turnip/internal/provisioning"
	"github.com/ivanvc/turnip/internal/rpc"
	"github.com/ivanvc/turnip/internal/runnerauth"
)

const (
	checkpointNamespace   = "turnip"
	checkpointOperationID = "op-checkpoint"
	checkpointJobName     = "turnip-runner-abcde"
	checkpointPodUID      = "pod-uid-checkpoint"

	cloneContainerName  = "clone"
	runnerContainerName = "runner"

	// podUIDExtra is where the API server reports which Pod a projected
	// token belongs to. Spelled out rather than shared with the package,
	// so this test pins the wire name.
	podUIDExtra = "authentication.kubernetes.io/pod-uid"

	// The fake cluster's tokens are "minted:<pod uid>:<audience>", so its
	// TokenReview can answer exactly as the API server would for a token
	// kubelet projected for that Pod and audience.
	mintedTokenPrefix = "minted:"
)

// provisioningShapes are the two Job shapes BuildJob produces, one per
// strategy, so the checkpoint holds for every tool rather than the one a
// fixture happened to pick.
var provisioningShapes = map[string]struct {
	tool    string
	version string
	spec    provisioning.Spec
}{
	"copy-out": {
		tool: "terraform", version: "1.9.5",
		spec: provisioning.Spec{Strategy: provisioning.CopyOut, Image: "hashicorp/terraform", BinaryPath: "/bin/terraform"},
	},
	"run-in-image": {
		tool: "helmfile", version: "v1.7.4",
		spec: provisioning.Spec{Strategy: provisioning.RunInImage, Image: "ghcr.io/helmfile/helmfile"},
	},
}

// buildOperationPod builds the real Runner Job for one provisioning shape
// and the Pod the Job controller would create from it: the Job's own
// template, with a uid and the job-name label the authenticator looks up.
func buildOperationPod(t *testing.T, tool, version string, spec provisioning.Spec) (*batchv1.Job, *corev1.Pod) {
	t.Helper()

	project := config.Project{
		Name:        "web",
		Directory:   "infra/web",
		Uses:        tool + "@" + version,
		Tool:        tool,
		ToolVersion: version,
	}
	job, err := jobs.BuildJob(project, jobs.OperationParams{
		OperationID:  checkpointOperationID,
		Operation:    "plan",
		RepoURL:      "https://github.com/acme/repo.git",
		CommitSHA:    "abc123",
		BaseRef:      "main",
		ServerAddr:   "server.turnip.svc:9443",
		RunnerImage:  "ghcr.io/ivanvc/turnip-runner:test",
		Provisioning: spec,
		ImageRef:     spec.Image + ":" + version,
	})
	require.NoError(t, err)

	// The API server fills in GenerateName; the fake clientset does not.
	job.Name = checkpointJobName
	job.Namespace = checkpointNamespace

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      checkpointJobName + "-xyz",
			Namespace: checkpointNamespace,
			UID:       types.UID(checkpointPodUID),
			Labels:    map[string]string{"batch.kubernetes.io/job-name": checkpointJobName},
		},
		Spec: *job.Spec.Template.Spec.DeepCopy(),
	}
	return job, pod
}

// allContainers is every container in the Pod, init containers first.
func allContainers(pod *corev1.Pod) []corev1.Container {
	return append(slices.Clone(pod.Spec.InitContainers), pod.Spec.Containers...)
}

func containerNamed(t *testing.T, pod *corev1.Pod, name string) corev1.Container {
	t.Helper()
	for _, c := range allContainers(pod) {
		if c.Name == name {
			return c
		}
	}
	require.FailNow(t, "container not found", "no container named %q in the Pod", name)
	return corev1.Container{}
}

// projectedIdentity is one ServiceAccountToken projection a container can
// read: the file it lands at and the audience it was projected for.
type projectedIdentity struct {
	file     string
	audience string
}

// mountedIdentities returns every projected ServiceAccountToken the
// container mounts, read from the Pod spec: the mount says where, the
// Pod's volume says for which audience.
func mountedIdentities(pod *corev1.Pod, c corev1.Container) []projectedIdentity {
	volumes := make(map[string]corev1.Volume, len(pod.Spec.Volumes))
	for _, v := range pod.Spec.Volumes {
		volumes[v.Name] = v
	}

	var out []projectedIdentity
	for _, m := range c.VolumeMounts {
		v, ok := volumes[m.Name]
		if !ok || v.Projected == nil {
			continue
		}
		for _, src := range v.Projected.Sources {
			if src.ServiceAccountToken == nil {
				continue
			}
			out = append(out, projectedIdentity{
				file:     path.Join(m.MountPath, src.ServiceAccountToken.Path),
				audience: src.ServiceAccountToken.Audience,
			})
		}
	}
	return out
}

// identityOf returns the one Runner identity the container mounts, and
// checks it is the file the Runner in that container reads
// (TURNIP_TOKEN_FILE), so the audience asserted below is the one the
// Runner actually presents.
func identityOf(t *testing.T, pod *corev1.Pod, name string) projectedIdentity {
	t.Helper()
	c := containerNamed(t, pod, name)

	identities := mountedIdentities(pod, c)
	require.Len(t, identities, 1, "container %q mounts exactly one Runner identity", name)

	var tokenFile string
	for _, e := range c.Env {
		if e.Name == "TURNIP_TOKEN_FILE" {
			tokenFile = e.Value
		}
	}
	require.Equal(t, identities[0].file, tokenFile,
		"container %q's Runner reads the Runner identity it mounts", name)
	return identities[0]
}

// mint is the token kubelet would project for this Pod and audience.
func mint(podUID, audience string) string {
	return mintedTokenPrefix + podUID + ":" + audience
}

// newCluster is a fake cluster holding the Operation's Job and Pod, whose
// TokenReview answers as the API server does: a token is authenticated
// only for a requested audience it was minted for, the review echoes that
// audience, and the pod-uid extra names the Pod it was minted for.
func newCluster(t *testing.T, job *batchv1.Job, pod *corev1.Pod) *fake.Clientset {
	t.Helper()

	clientset := fake.NewSimpleClientset(job, pod)
	clientset.PrependReactor("create", "tokenreviews", func(action k8stesting.Action) (bool, runtime.Object, error) {
		tr := action.(k8stesting.CreateAction).GetObject().(*authv1.TokenReview)
		out := tr.DeepCopy()

		podUID, audience, ok := strings.Cut(strings.TrimPrefix(tr.Spec.Token, mintedTokenPrefix), ":")
		if !strings.HasPrefix(tr.Spec.Token, mintedTokenPrefix) || !ok {
			out.Status = authv1.TokenReviewStatus{Error: "token not recognized"}
			return true, out, nil
		}
		if !slices.Contains(tr.Spec.Audiences, audience) {
			out.Status = authv1.TokenReviewStatus{Error: "token audiences are invalid for the target audiences"}
			return true, out, nil
		}
		out.Status = authv1.TokenReviewStatus{
			Authenticated: true,
			Audiences:     []string{audience},
			User: authv1.UserInfo{
				Username: "system:serviceaccount:" + checkpointNamespace + ":default",
				Extra:    map[string]authv1.ExtraValue{podUIDExtra: {podUID}},
			},
		}
		return true, out, nil
	})
	return clientset
}

// checkpointHandler stands in for the orchestrator: it mints a GitHub
// credential when asked and records every result reported.
type checkpointHandler struct {
	mu            sync.Mutex
	credentialFor []string
	results       []rpc.OperationResult
	resultFor     []string
}

func (h *checkpointHandler) HandleLog(context.Context, string, rpc.LogLine) error { return nil }

func (h *checkpointHandler) HandleResult(_ context.Context, operationID string, result rpc.OperationResult) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.resultFor = append(h.resultFor, operationID)
	h.results = append(h.results, result)
	return nil
}

func (h *checkpointHandler) CloneCredential(_ context.Context, operationID string) (rpc.CloneCredential, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.credentialFor = append(h.credentialFor, operationID)
	return rpc.CloneCredential{Token: "ghs_for_" + operationID}, nil
}

func (h *checkpointHandler) snapshot() (credentialFor, resultFor []string, results []rpc.OperationResult) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.credentialFor), slices.Clone(h.resultFor), slices.Clone(h.results)
}

// serve runs the real Server (its interceptors and operationServer) with
// the real Authenticator over the fake cluster, on an in-process
// listener, and returns a client.
func serve(t *testing.T, clientset *fake.Clientset, handler rpc.OperationHandler) pb.OperationServiceClient {
	t.Helper()

	lis := bufconn.Listen(1024 * 1024)
	server := rpc.NewServer(handler, rpc.WithAuthenticator(runnerauth.New(clientset, checkpointNamespace)))
	go func() { _ = server.Serve(lis) }()
	t.Cleanup(server.Stop)

	conn, err := grpc.NewClient("passthrough:///bufconn",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return pb.NewOperationServiceClient(conn)
}

// presenting is the metadata a Runner attaches when it presents token for
// the Operation.
func presenting(token string) context.Context {
	return metadata.AppendToOutgoingContext(context.Background(),
		rpc.AuthorizationKey, rpc.BearerPrefix+token,
		rpc.OperationIDKey, checkpointOperationID,
	)
}

// report opens the result stream and sends one result.
func report(ctx context.Context, client pb.OperationServiceClient, result *pb.OperationResult) error {
	stream, err := client.ExecuteOperation(ctx)
	if err != nil {
		return err
	}
	if err := stream.Send(&pb.ExecuteOperationRequest{
		Payload: &pb.ExecuteOperationRequest_Result{Result: result},
	}); err != nil {
		return err
	}
	_, err = stream.CloseAndRecv()
	return err
}

// syncBuffer is a bytes.Buffer safe to write from the Server's goroutines
// while the test reads it.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

// captureLogRecords points slog's default at a buffer for the rest of the
// test and returns a function decoding what was written. Safe because no
// test in this package runs in parallel.
func captureLogRecords(t *testing.T) func() []map[string]any {
	t.Helper()
	buf := &syncBuffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	return func() []map[string]any {
		buf.mu.Lock()
		raw := bytes.Clone(buf.buf.Bytes())
		buf.mu.Unlock()

		var records []map[string]any
		sc := bufio.NewScanner(bytes.NewReader(raw))
		for sc.Scan() {
			var rec map[string]any
			require.NoError(t, json.Unmarshal(sc.Bytes(), &rec))
			records = append(records, rec)
		}
		return records
	}
}

// Only the clone init container and the runner container talk to the
// Server, so only they hold a Runner identity. A provisioning init
// container runs a vendor image and must mount neither.
func TestCheckpoint_OnlyCloneAndRunnerMountARunnerIdentity(t *testing.T) {
	for shape, s := range provisioningShapes {
		t.Run(shape, func(t *testing.T) {
			_, pod := buildOperationPod(t, s.tool, s.version, s.spec)

			for _, c := range allContainers(pod) {
				identities := mountedIdentities(pod, c)
				switch c.Name {
				case cloneContainerName, runnerContainerName:
					assert.Len(t, identities, 1, "container %q mounts exactly one Runner identity", c.Name)
				default:
					assert.Empty(t, identities, "container %q must mount no Runner identity", c.Name)
				}
			}

			assert.Equal(t, jobs.CloneAudience, identityOf(t, pod, cloneContainerName).audience)
			assert.Equal(t, jobs.ReportAudience, identityOf(t, pod, runnerContainerName).audience)
		})
	}
}

// The tool runs in the runner container, so whatever identity that
// container mounts, the repository's own code can present. Presented to
// the real Server, it must not yield a GitHub credential, and must still
// report the Operation's result.
func TestCheckpoint_RunnerContainerIdentityCannotFetchAGitHubCredential(t *testing.T) {
	for shape, s := range provisioningShapes {
		t.Run(shape, func(t *testing.T) {
			logs := captureLogRecords(t)
			job, pod := buildOperationPod(t, s.tool, s.version, s.spec)
			handler := &checkpointHandler{}
			client := serve(t, newCluster(t, job, pod), handler)

			runnerIdentity := mint(string(pod.UID), identityOf(t, pod, runnerContainerName).audience)

			resp, err := client.FetchCloneCredential(presenting(runnerIdentity), &pb.FetchCloneCredentialRequest{})
			require.Error(t, err)
			assert.Equal(t, codes.PermissionDenied, status.Code(err))
			assert.Empty(t, resp.GetToken())

			credentialFor, _, _ := handler.snapshot()
			assert.Empty(t, credentialFor, "no GitHub credential is minted for the runner container's identity")

			var refusal map[string]any
			for _, rec := range logs() {
				if rec["msg"] == "refusing rpc: credential fetch requires the clone identity" {
					refusal = rec
				}
			}
			require.NotNil(t, refusal, "the refused fetch is logged")
			assert.Equal(t, "ERROR", refusal["level"])

			require.NoError(t, report(presenting(runnerIdentity), client, &pb.OperationResult{Success: true, Output: "planned"}),
				"the runner container's identity may report the result")
			_, resultFor, results := handler.snapshot()
			assert.Equal(t, []string{checkpointOperationID}, resultFor)
			require.Len(t, results, 1)
			assert.True(t, results[0].Success)
		})
	}
}

// The clone container's identity is the one that fetches the GitHub
// credential for git, and it reports a clone that failed.
func TestCheckpoint_CloneContainerIdentityFetchesACredentialAndReports(t *testing.T) {
	for shape, s := range provisioningShapes {
		t.Run(shape, func(t *testing.T) {
			job, pod := buildOperationPod(t, s.tool, s.version, s.spec)
			handler := &checkpointHandler{}
			client := serve(t, newCluster(t, job, pod), handler)

			cloneIdentity := mint(string(pod.UID), identityOf(t, pod, cloneContainerName).audience)

			resp, err := client.FetchCloneCredential(presenting(cloneIdentity), &pb.FetchCloneCredentialRequest{})
			require.NoError(t, err)
			assert.Equal(t, "ghs_for_"+checkpointOperationID, resp.GetToken())

			credentialFor, _, _ := handler.snapshot()
			assert.Equal(t, []string{checkpointOperationID}, credentialFor)

			require.NoError(t, report(presenting(cloneIdentity), client, &pb.OperationResult{
				Success:         false,
				ErrorMessage:    "clone failed",
				FailureCategory: pb.FailureCategory_FAILURE_CATEGORY_CLONE_FAILED,
			}), "the clone container's identity may report a failed clone")
			_, resultFor, results := handler.snapshot()
			assert.Equal(t, []string{checkpointOperationID}, resultFor)
			require.Len(t, results, 1)
			assert.False(t, results[0].Success)
			assert.Equal(t, rpc.FailureCloneFailed, results[0].FailureCategory)
		})
	}
}
