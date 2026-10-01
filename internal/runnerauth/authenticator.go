package runnerauth

import (
	"context"
	"fmt"

	authv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	authv1client "k8s.io/client-go/kubernetes/typed/authentication/v1"
	batchv1client "k8s.io/client-go/kubernetes/typed/batch/v1"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"

	"github.com/ivanvc/turnip/internal/jobs"
	"github.com/ivanvc/turnip/internal/rpc"
)

// podUIDExtra is where the API server reports the uid of the Pod a
// projected ServiceAccount token was minted for.
//
// It is the reason this slice requires Kubernetes v1.32. The extra exists
// from v1.29 but sits behind the ServiceAccountTokenPodNodeInfo feature
// gate until v1.32, and a gate turnip cannot detect from a TokenReview
// response is not a version it can claim to support — an older cluster
// simply returns no extras, which is indistinguishable from a token that
// never carried them.
const podUIDExtra = "authentication.kubernetes.io/pod-uid"

// Authenticator implements rpc.Authenticator against a real cluster.
type Authenticator struct {
	reviews authv1client.TokenReviewInterface
	jobs    batchv1client.JobInterface
	pods    corev1client.PodInterface
}

// New constructs an Authenticator. TokenReviews are cluster-scoped, which
// is why the Server needs a ClusterRole for them while Jobs and Pods stay
// namespaced.
func New(clientset kubernetes.Interface, namespace string) *Authenticator {
	return &Authenticator{
		reviews: clientset.AuthenticationV1().TokenReviews(),
		jobs:    clientset.BatchV1().Jobs(namespace),
		pods:    clientset.CoreV1().Pods(namespace),
	}
}

// Authenticate returns nil when token belongs to the Pod running
// operationID's Job, and otherwise an error wrapping one of
// rpc.ErrInvalidCredential or rpc.ErrNotBound.
//
// The two are distinct on purpose. Not recognizing a caller is ordinary —
// an expired token, a Job whose Pod is gone. Recognizing a caller and
// finding it belongs to a different Operation is the forgery, and the
// only case where turnip knows something is wrong rather than merely
// unresolvable.
//
// On success it also returns which Runner identity the token is:
// rpc.IdentityClone when the review reported jobs.CloneAudience, else
// rpc.IdentityReport. Pod-uid binding is the same for both. On failure
// the identity is always rpc.IdentityNone.
func (a *Authenticator) Authenticate(ctx context.Context, token, operationID string) (rpc.Identity, error) {
	callerUID, identity, err := a.reviewToken(ctx, token)
	if err != nil {
		return rpc.IdentityNone, err
	}

	expectedUID, err := a.podUIDForOperation(ctx, operationID)
	if err != nil {
		return rpc.IdentityNone, err
	}

	// uids, not names. A Pod name can be reused after deletion — a Job
	// recreated under the same generated name would produce a Pod whose
	// name matches a previous Operation's, and a name comparison would
	// admit it. A uid is unique for the life of the cluster.
	if callerUID != expectedUID {
		return rpc.IdentityNone, fmt.Errorf("%w: token belongs to pod %s, operation %s runs on pod %s",
			rpc.ErrNotBound, callerUID, operationID, expectedUID)
	}
	return identity, nil
}

// reviewToken validates token against turnip's two audiences and returns
// the uid of the Pod it was minted for and the Runner identity its
// audience makes it.
func (a *Authenticator) reviewToken(ctx context.Context, token string) (types.UID, rpc.Identity, error) {
	review, err := a.reviews.Create(ctx, &authv1.TokenReview{
		Spec: authv1.TokenReviewSpec{
			Token: token,
			// Naming the audience is what makes this a check rather than a
			// formality. Without it the API server validates against its
			// own audience, so a Pod's default ServiceAccount token would
			// pass — and the Server could then replay that token to the API
			// server as the Runner's ServiceAccount.
			//
			// Both identities are named so one review serves either
			// container; the API server answers with the subset the token
			// is actually valid for, and that answer alone decides the
			// identity below.
			Audiences: []string{jobs.ReportAudience, jobs.CloneAudience},
		},
	}, metav1.CreateOptions{})
	if err != nil {
		return "", rpc.IdentityNone, fmt.Errorf("%w: token review failed: %v", rpc.ErrInvalidCredential, err)
	}

	if !review.Status.Authenticated {
		return "", rpc.IdentityNone, fmt.Errorf("%w: %s", rpc.ErrInvalidCredential, reviewError(review))
	}

	// The API server echoes back which of the requested audiences the
	// token is actually valid for. An authenticated response with neither
	// of ours means it authenticated for something else. The clone
	// identity is granted only on the review's word that the token is
	// valid for jobs.CloneAudience: never by default, never by elimination.
	var identity rpc.Identity
	switch {
	case containsAudience(review.Status.Audiences, jobs.CloneAudience):
		identity = rpc.IdentityClone
	case containsAudience(review.Status.Audiences, jobs.ReportAudience):
		identity = rpc.IdentityReport
	default:
		return "", rpc.IdentityNone, fmt.Errorf("%w: token is not scoped to %s or %s",
			rpc.ErrInvalidCredential, jobs.ReportAudience, jobs.CloneAudience)
	}

	uids := review.Status.User.Extra[podUIDExtra]
	if len(uids) != 1 || uids[0] == "" {
		return "", rpc.IdentityNone, fmt.Errorf("%w: the cluster reported no %s; turnip requires Kubernetes v1.32 or later",
			rpc.ErrInvalidCredential, podUIDExtra)
	}
	return types.UID(uids[0]), identity, nil
}

// podUIDForOperation resolves the uid of the one Pod running
// operationID's Job.
func (a *Authenticator) podUIDForOperation(ctx context.Context, operationID string) (types.UID, error) {
	jobList, err := a.jobs.List(ctx, metav1.ListOptions{
		LabelSelector: jobs.OperationIDLabel + "=" + operationID,
	})
	if err != nil {
		return "", fmt.Errorf("%w: listing jobs for operation %s: %v", rpc.ErrInvalidCredential, operationID, err)
	}
	if len(jobList.Items) != 1 {
		return "", fmt.Errorf("%w: expected one job for operation %s, found %d",
			rpc.ErrInvalidCredential, operationID, len(jobList.Items))
	}

	podList, err := a.pods.List(ctx, metav1.ListOptions{
		LabelSelector: "batch.kubernetes.io/job-name=" + jobList.Items[0].Name,
	})
	if err != nil {
		return "", fmt.Errorf("%w: listing pods for operation %s: %v", rpc.ErrInvalidCredential, operationID, err)
	}
	// BackoffLimit: 0 (internal/jobs.BuildJob) guarantees at most one Pod
	// per Job, so more than one here means that invariant broke rather
	// than that a choice is needed.
	if len(podList.Items) != 1 {
		return "", fmt.Errorf("%w: expected one pod for operation %s, found %d",
			rpc.ErrInvalidCredential, operationID, len(podList.Items))
	}

	return podList.Items[0].UID, nil
}

func containsAudience(audiences []string, want string) bool {
	for _, a := range audiences {
		if a == want {
			return true
		}
	}
	return false
}

// reviewError renders why a review came back unauthenticated. The API
// server usually populates Status.Error; when it does not, saying so
// beats an empty message.
func reviewError(review *authv1.TokenReview) string {
	if review.Status.Error != "" {
		return review.Status.Error
	}
	return "token not authenticated"
}
