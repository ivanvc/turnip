package runnerauth

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	authv1 "k8s.io/api/authentication/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/ivanvc/turnip/internal/jobs"
	"github.com/ivanvc/turnip/internal/rpc"
)

const testNamespace = "turnip"

// review is what a fake cluster answers a TokenReview with.
type review struct {
	authenticated bool
	audiences     []string
	podUID        string
	// extraKey overrides where the uid is reported, so a test can model a
	// cluster that answers without the extra at all.
	omitPodUID bool
	errMessage string
}

// operationPod describes one Operation's Job and the single Pod
// Kubernetes created for it.
type operationPod struct {
	operationID string
	jobName     string
	podUID      string
}

// newAuthenticator builds an Authenticator over a fake cluster holding
// ops' Jobs and Pods, answering every TokenReview with r and recording
// the audiences it was asked about.
func newAuthenticator(t *testing.T, r review, ops ...operationPod) (*Authenticator, *[]string) {
	t.Helper()

	var objects []runtime.Object
	for _, op := range ops {
		objects = append(objects,
			&batchv1.Job{ObjectMeta: metav1.ObjectMeta{
				Name:      op.jobName,
				Namespace: testNamespace,
				Labels:    map[string]string{jobs.OperationIDLabel: op.operationID},
			}},
			&corev1.Pod{ObjectMeta: metav1.ObjectMeta{
				Name:      op.jobName + "-xyz",
				Namespace: testNamespace,
				UID:       types.UID(op.podUID),
				Labels:    map[string]string{"batch.kubernetes.io/job-name": op.jobName},
			}},
		)
	}

	clientset := fake.NewSimpleClientset(objects...)
	asked := &[]string{}
	clientset.PrependReactor("create", "tokenreviews", func(action k8stesting.Action) (bool, runtime.Object, error) {
		tr := action.(k8stesting.CreateAction).GetObject().(*authv1.TokenReview)
		*asked = append(*asked, tr.Spec.Audiences...)

		out := tr.DeepCopy()
		out.Status = authv1.TokenReviewStatus{
			Authenticated: r.authenticated,
			Audiences:     r.audiences,
			Error:         r.errMessage,
		}
		if !r.omitPodUID {
			out.Status.User.Extra = map[string]authv1.ExtraValue{
				podUIDExtra: {r.podUID},
			}
		}
		return true, out, nil
	})

	return New(clientset, testNamespace), asked
}

func acceptedReview(podUID string) review {
	return review{authenticated: true, audiences: []string{jobs.ReportAudience}, podUID: podUID}
}

func TestAuthenticate_RunnerMayWriteToItsOwnOperation(t *testing.T) {
	auth, asked := newAuthenticator(t, acceptedReview("pod-uid-1"),
		operationPod{operationID: "op-1", jobName: "turnip-runner-a", podUID: "pod-uid-1"},
	)

	identity, err := auth.Authenticate(context.Background(), "token", "op-1")
	require.NoError(t, err)
	assert.Equal(t, rpc.IdentityReport, identity)
	assert.Equal(t, []string{jobs.ReportAudience, jobs.CloneAudience}, *asked,
		"the review must name turnip's audiences, or a default API-server token would pass")
}

// The attack this slice exists to stop. The caller's token is entirely
// valid — it is a real Runner, with a real Pod, for a real Operation. It
// is simply naming somebody else's Operation, whose id travels in every
// Job's environment as TURNIP_OPERATION_ID.
//
// An implementation that authenticates without binding passes every other
// test here and fails only this one.
func TestAuthenticate_ValidRunnerCannotWriteToAnotherOperation(t *testing.T) {
	auth, _ := newAuthenticator(t, acceptedReview("pod-uid-1"),
		operationPod{operationID: "op-1", jobName: "turnip-runner-a", podUID: "pod-uid-1"},
		operationPod{operationID: "op-2", jobName: "turnip-runner-b", podUID: "pod-uid-2"},
	)

	_, err := auth.Authenticate(context.Background(), "token", "op-1")
	require.NoError(t, err, "precondition: this really is op-1's runner")

	_, err = auth.Authenticate(context.Background(), "token", "op-2")
	require.Error(t, err)
	require.ErrorIs(t, err, rpc.ErrNotBound)
	assert.NotErrorIs(t, err, rpc.ErrInvalidCredential,
		"turnip knows exactly who this is; the credential is not the problem")
}

func TestAuthenticate_UnauthenticatedTokenIsRefused(t *testing.T) {
	auth, _ := newAuthenticator(t,
		review{authenticated: false, errMessage: "token expired"},
		operationPod{operationID: "op-1", jobName: "turnip-runner-a", podUID: "pod-uid-1"},
	)

	_, err := auth.Authenticate(context.Background(), "token", "op-1")
	require.Error(t, err)
	require.ErrorIs(t, err, rpc.ErrInvalidCredential)
	assert.Contains(t, err.Error(), "token expired", "the cluster's own reason is reported")
}

// A token the API server authenticates for some *other* audience is
// refused. Without this check the Pod's default ServiceAccount token
// would be accepted — and the Server could then replay it to the API
// server as the Runner's ServiceAccount, which is the mistake audience
// scoping exists to make impossible.
func TestAuthenticate_TokenForAnotherAudienceIsRefused(t *testing.T) {
	auth, _ := newAuthenticator(t,
		review{authenticated: true, audiences: []string{"https://kubernetes.default.svc"}, podUID: "pod-uid-1"},
		operationPod{operationID: "op-1", jobName: "turnip-runner-a", podUID: "pod-uid-1"},
	)

	_, err := auth.Authenticate(context.Background(), "token", "op-1")
	require.Error(t, err)
	require.ErrorIs(t, err, rpc.ErrInvalidCredential)
	assert.Contains(t, err.Error(), jobs.ReportAudience)
	assert.Contains(t, err.Error(), jobs.CloneAudience)
}

// A cluster that cannot say which Pod a token belongs to gets a refusal,
// not a relaxed check. Degrading to "any valid Runner token is fine"
// would silently restore exactly the gap this slice closed, on precisely
// the clusters least able to notice.
func TestAuthenticate_NoPodIdentityNoStream(t *testing.T) {
	auth, _ := newAuthenticator(t,
		review{authenticated: true, audiences: []string{jobs.ReportAudience}, omitPodUID: true},
		operationPod{operationID: "op-1", jobName: "turnip-runner-a", podUID: "pod-uid-1"},
	)

	_, err := auth.Authenticate(context.Background(), "token", "op-1")
	require.Error(t, err)
	require.ErrorIs(t, err, rpc.ErrInvalidCredential)
	assert.Contains(t, err.Error(), "v1.32", "the refusal says what the cluster is missing")
	assert.Contains(t, err.Error(), podUIDExtra)
}

func TestAuthenticate_UnresolvableOperationIsRefused(t *testing.T) {
	for name, operationID := range map[string]string{
		"no job carries this operation's label": "op-unknown",
	} {
		t.Run(name, func(t *testing.T) {
			auth, _ := newAuthenticator(t, acceptedReview("pod-uid-1"),
				operationPod{operationID: "op-1", jobName: "turnip-runner-a", podUID: "pod-uid-1"},
			)

			_, err := auth.Authenticate(context.Background(), "token", operationID)
			require.Error(t, err)
			assert.ErrorIs(t, err, rpc.ErrInvalidCredential)
		})
	}
}

// A Job whose Pod has not appeared yet — or has already been collected —
// cannot be bound to, because there is no uid to compare against.
func TestAuthenticate_JobWithoutAPodIsRefused(t *testing.T) {
	clientset := fake.NewSimpleClientset(&batchv1.Job{ObjectMeta: metav1.ObjectMeta{
		Name:      "turnip-runner-a",
		Namespace: testNamespace,
		Labels:    map[string]string{jobs.OperationIDLabel: "op-1"},
	}})
	clientset.PrependReactor("create", "tokenreviews", func(action k8stesting.Action) (bool, runtime.Object, error) {
		tr := action.(k8stesting.CreateAction).GetObject().(*authv1.TokenReview).DeepCopy()
		tr.Status = authv1.TokenReviewStatus{
			Authenticated: true,
			Audiences:     []string{jobs.ReportAudience},
			User:          authv1.UserInfo{Extra: map[string]authv1.ExtraValue{podUIDExtra: {"pod-uid-1"}}},
		}
		return true, tr, nil
	})

	_, err := New(clientset, testNamespace).Authenticate(context.Background(), "token", "op-1")
	require.Error(t, err)
	require.ErrorIs(t, err, rpc.ErrInvalidCredential)
}

// Comparing names rather than uids would admit a Pod whose name was
// reused after deletion. Here two Operations' Pods share a name and
// differ only by uid — a name comparison passes, a uid comparison does
// not.
func TestAuthenticate_ComparesUIDsNotNames(t *testing.T) {
	clientset := fake.NewSimpleClientset(
		&batchv1.Job{ObjectMeta: metav1.ObjectMeta{
			Name: "job-a", Namespace: testNamespace,
			Labels: map[string]string{jobs.OperationIDLabel: "op-1"},
		}},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{
			Name: "turnip-runner-reused", Namespace: testNamespace, UID: "uid-old",
			Labels: map[string]string{"batch.kubernetes.io/job-name": "job-a"},
		}},
	)
	clientset.PrependReactor("create", "tokenreviews", func(action k8stesting.Action) (bool, runtime.Object, error) {
		tr := action.(k8stesting.CreateAction).GetObject().(*authv1.TokenReview).DeepCopy()
		tr.Status = authv1.TokenReviewStatus{
			Authenticated: true,
			Audiences:     []string{jobs.ReportAudience},
			// Same Pod name as op-1's, different uid: the caller is a
			// later Pod that happens to have inherited the name.
			User: authv1.UserInfo{
				Username: "system:serviceaccount:turnip:default",
				Extra: map[string]authv1.ExtraValue{
					podUIDExtra:                             {"uid-new"},
					"authentication.kubernetes.io/pod-name": {"turnip-runner-reused"},
				},
			},
		}
		return true, tr, nil
	})

	_, err := New(clientset, testNamespace).Authenticate(context.Background(), "token", "op-1")
	require.Error(t, err)
	assert.ErrorIs(t, err, rpc.ErrNotBound)
}

// Nothing about the ServiceAccount enters the decision, which is what
// keeps Slice 13's runner.serviceAccount override orthogonal: a Project
// may choose the account its Pod runs as without changing whether it can
// authenticate.
func TestAuthenticate_ServiceAccountIsNotPartOfTheDecision(t *testing.T) {
	for _, username := range []string{
		"system:serviceaccount:turnip:default",
		"system:serviceaccount:turnip:project-chosen",
	} {
		clientset := fake.NewSimpleClientset(
			&batchv1.Job{ObjectMeta: metav1.ObjectMeta{
				Name: "job-a", Namespace: testNamespace,
				Labels: map[string]string{jobs.OperationIDLabel: "op-1"},
			}},
			&corev1.Pod{ObjectMeta: metav1.ObjectMeta{
				Name: "pod-a", Namespace: testNamespace, UID: "pod-uid-1",
				Labels: map[string]string{"batch.kubernetes.io/job-name": "job-a"},
			}},
		)
		clientset.PrependReactor("create", "tokenreviews", func(action k8stesting.Action) (bool, runtime.Object, error) {
			tr := action.(k8stesting.CreateAction).GetObject().(*authv1.TokenReview).DeepCopy()
			tr.Status = authv1.TokenReviewStatus{
				Authenticated: true,
				Audiences:     []string{jobs.ReportAudience},
				User: authv1.UserInfo{
					Username: username,
					Extra:    map[string]authv1.ExtraValue{podUIDExtra: {"pod-uid-1"}},
				},
			}
			return true, tr, nil
		})

		_, err := New(clientset, testNamespace).Authenticate(context.Background(), "token", "op-1")
		assert.NoError(t, err, "account %q", username)
	}
}

// The Runner identity comes only from the audiences the API server
// reports the token valid for. The clone identity, the one that may fetch
// a GitHub credential, is never granted unless the review reported
// jobs.CloneAudience; a token valid for both is the clone identity, since
// the review vouched for that audience.
func TestAuthenticate_IdentityIsTheReviewedAudience(t *testing.T) {
	for name, tc := range map[string]struct {
		audiences []string
		want      rpc.Identity
	}{
		"report audience only": {[]string{jobs.ReportAudience}, rpc.IdentityReport},
		"clone audience only":  {[]string{jobs.CloneAudience}, rpc.IdentityClone},
		"both audiences":       {[]string{jobs.ReportAudience, jobs.CloneAudience}, rpc.IdentityClone},
		"both, clone first":    {[]string{jobs.CloneAudience, jobs.ReportAudience}, rpc.IdentityClone},
	} {
		t.Run(name, func(t *testing.T) {
			auth, _ := newAuthenticator(t,
				review{authenticated: true, audiences: tc.audiences, podUID: "pod-uid-1"},
				operationPod{operationID: "op-1", jobName: "turnip-runner-a", podUID: "pod-uid-1"},
			)

			identity, err := auth.Authenticate(context.Background(), "token", "op-1")
			require.NoError(t, err)
			assert.Equal(t, tc.want, identity)
		})
	}
}

// A review reporting neither turnip audience is refused as an unknown
// caller, whatever else it reports, and yields no identity.
func TestAuthenticate_NeitherAudienceIsRefused(t *testing.T) {
	for name, audiences := range map[string][]string{
		"no audiences reported":    nil,
		"another audience":         {"https://kubernetes.default.svc"},
		"a lookalike of clone":     {jobs.CloneAudience + "/"},
		"a lookalike of report":    {"https://" + jobs.ReportAudience},
		"report audience's parent": {"ivan.vc"},
	} {
		t.Run(name, func(t *testing.T) {
			auth, _ := newAuthenticator(t,
				review{authenticated: true, audiences: audiences, podUID: "pod-uid-1"},
				operationPod{operationID: "op-1", jobName: "turnip-runner-a", podUID: "pod-uid-1"},
			)

			identity, err := auth.Authenticate(context.Background(), "token", "op-1")
			require.Error(t, err)
			require.ErrorIs(t, err, rpc.ErrInvalidCredential)
			assert.Equal(t, rpc.IdentityNone, identity)
		})
	}
}

// Pod-uid binding applies to the clone identity exactly as to the report
// identity: a valid clone token for another Operation's Pod is the
// forgery, and yields no identity.
func TestAuthenticate_CloneIdentityIsBoundToItsOperation(t *testing.T) {
	auth, _ := newAuthenticator(t,
		review{authenticated: true, audiences: []string{jobs.CloneAudience}, podUID: "pod-uid-1"},
		operationPod{operationID: "op-1", jobName: "turnip-runner-a", podUID: "pod-uid-1"},
		operationPod{operationID: "op-2", jobName: "turnip-runner-b", podUID: "pod-uid-2"},
	)

	identity, err := auth.Authenticate(context.Background(), "token", "op-2")
	require.Error(t, err)
	require.ErrorIs(t, err, rpc.ErrNotBound)
	assert.Equal(t, rpc.IdentityNone, identity)
}

// Every failure returns IdentityNone, so a caller that ignored the error
// still could not mistake a refused token for either identity.
func TestAuthenticate_FailureYieldsNoIdentity(t *testing.T) {
	for name, r := range map[string]review{
		"unauthenticated": {authenticated: false, audiences: []string{jobs.CloneAudience}, errMessage: "expired"},
		"no pod uid":      {authenticated: true, audiences: []string{jobs.CloneAudience}, omitPodUID: true},
	} {
		t.Run(name, func(t *testing.T) {
			auth, _ := newAuthenticator(t, r,
				operationPod{operationID: "op-1", jobName: "turnip-runner-a", podUID: "pod-uid-1"},
			)

			identity, err := auth.Authenticate(context.Background(), "token", "op-1")
			require.ErrorIs(t, err, rpc.ErrInvalidCredential)
			assert.Equal(t, rpc.IdentityNone, identity)
		})
	}
}
