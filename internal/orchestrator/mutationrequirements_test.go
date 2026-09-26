package orchestrator

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivanvc/turnip/internal/github"
)

// fakeRequirementsClient answers the three questions the Requirement_Set
// asks GitHub, and records how often each was asked.
type fakeRequirementsClient struct {
	github.GitHubClient

	reviews    []github.Review
	reviewsErr error

	// permissions maps login to permission level; a login in permErrs
	// fails instead.
	permissions map[string]string
	permErrs    map[string]error
	permAsked   []string

	// mergeable is returned by successive GetPullRequest calls; past its
	// end the last value repeats.
	mergeable []*bool
	getErr    error
	getCalls  int

	reviewCalls int
}

func (f *fakeRequirementsClient) ListReviews(ctx context.Context, owner, repo string, prNumber int) ([]github.Review, error) {
	f.reviewCalls++
	return f.reviews, f.reviewsErr
}

func (f *fakeRequirementsClient) GetCollaboratorPermission(ctx context.Context, owner, repo, username string) (string, error) {
	f.permAsked = append(f.permAsked, username)
	if err, ok := f.permErrs[username]; ok {
		return "", err
	}
	if p, ok := f.permissions[username]; ok {
		return p, nil
	}
	return "read", nil
}

func (f *fakeRequirementsClient) GetPullRequest(ctx context.Context, owner, repo string, prNumber int) (*github.PullRequest, error) {
	f.getCalls++
	if f.getErr != nil {
		return nil, f.getErr
	}
	var m *bool
	if len(f.mergeable) > 0 {
		m = f.mergeable[min(f.getCalls, len(f.mergeable))-1]
	}
	return &github.PullRequest{Number: prNumber, Author: "author", Open: true, Mergeable: m}, nil
}

func boolPtr(b bool) *bool { return &b }

// newRequirementCheck builds a check over client and pr, with a sleep that
// records each wait instead of taking it.
func newRequirementCheck(client *fakeRequirementsClient, pr *github.PullRequest) (*mutationRequirementCheck, *[]time.Duration) {
	var waits []time.Duration
	return &mutationRequirementCheck{
		client:     client,
		authorizer: github.NewAuthorizer(client),
		owner:      "o",
		repo:       "r",
		pr:         pr,
		sleep: func(_ context.Context, d time.Duration) error {
			waits = append(waits, d)
			return nil
		},
	}, &waits
}

func TestMutationRequirements_Approved(t *testing.T) {
	boom := errors.New("github: 502")
	cases := []struct {
		name        string
		reviews     []github.Review
		reviewsErr  error
		permissions map[string]string
		permErrs    map[string]error
		want        []unmetRequirement
		wantAsked   []string
	}{
		{
			name: "no reviews",
			want: []unmetRequirement{{MutationRequirementApproved, reasonUnmet}},
		},
		{
			name:        "the author's own approval does not count",
			reviews:     []github.Review{{Author: "author", State: "APPROVED"}},
			permissions: map[string]string{"author": "admin"},
			want:        []unmetRequirement{{MutationRequirementApproved, reasonUnmet}},
		},
		{
			name:        "the author's approval under a different case does not count",
			reviews:     []github.Review{{Author: "AUTHOR", State: "APPROVED"}},
			permissions: map[string]string{"AUTHOR": "admin"},
			want:        []unmetRequirement{{MutationRequirementApproved, reasonUnmet}},
		},
		{
			name: "an approval superseded by a request for changes does not count",
			reviews: []github.Review{
				{Author: "alice", State: "APPROVED"},
				{Author: "alice", State: "CHANGES_REQUESTED"},
			},
			permissions: map[string]string{"alice": "write"},
			want:        []unmetRequirement{{MutationRequirementApproved, reasonUnmet}},
		},
		{
			name: "a later comment leaves an approval standing",
			reviews: []github.Review{
				{Author: "alice", State: "APPROVED"},
				{Author: "alice", State: "COMMENTED"},
				{Author: "alice", State: "PENDING"},
			},
			permissions: map[string]string{"alice": "write"},
			wantAsked:   []string{"alice"},
		},
		{
			name: "an approval after changes were requested counts",
			reviews: []github.Review{
				{Author: "alice", State: "CHANGES_REQUESTED"},
				{Author: "Alice", State: "APPROVED"},
			},
			permissions: map[string]string{"Alice": "maintain"},
			wantAsked:   []string{"Alice"},
		},
		{
			name:        "a dismissed approval does not count",
			reviews:     []github.Review{{Author: "alice", State: "DISMISSED"}},
			permissions: map[string]string{"alice": "write"},
			want:        []unmetRequirement{{MutationRequirementApproved, reasonUnmet}},
		},
		{
			name: "an approval dismissed later does not count",
			reviews: []github.Review{
				{Author: "alice", State: "APPROVED"},
				{Author: "alice", State: "DISMISSED"},
			},
			permissions: map[string]string{"alice": "write"},
			want:        []unmetRequirement{{MutationRequirementApproved, reasonUnmet}},
		},
		{
			name:        "an approver without write permission does not count",
			reviews:     []github.Review{{Author: "stranger", State: "APPROVED"}},
			permissions: map[string]string{"stranger": "read"},
			want:        []unmetRequirement{{MutationRequirementApproved, reasonUnmet}},
			wantAsked:   []string{"stranger"},
		},
		{
			name: "one approver without write permission and one with",
			reviews: []github.Review{
				{Author: "stranger", State: "APPROVED"},
				{Author: "alice", State: "APPROVED"},
			},
			permissions: map[string]string{"stranger": "triage", "alice": "write"},
			wantAsked:   []string{"stranger", "alice"},
		},
		{
			name: "candidates are asked only until one has write permission",
			reviews: []github.Review{
				{Author: "alice", State: "APPROVED"},
				{Author: "bob", State: "APPROVED"},
			},
			permissions: map[string]string{"alice": "admin", "bob": "admin"},
			wantAsked:   []string{"alice"},
		},
		{
			name:       "a failed listing could not be checked",
			reviewsErr: boom,
			want:       []unmetRequirement{{MutationRequirementApproved, reasonCouldNotCheck}},
		},
		{
			name:      "a failed permission call could not be checked",
			reviews:   []github.Review{{Author: "alice", State: "APPROVED"}},
			permErrs:  map[string]error{"alice": boom},
			want:      []unmetRequirement{{MutationRequirementApproved, reasonCouldNotCheck}},
			wantAsked: []string{"alice"},
		},
		{
			name: "a failed permission call is settled by another approver",
			reviews: []github.Review{
				{Author: "alice", State: "APPROVED"},
				{Author: "bob", State: "APPROVED"},
			},
			permErrs:    map[string]error{"alice": boom},
			permissions: map[string]string{"bob": "write"},
			wantAsked:   []string{"alice", "bob"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := &fakeRequirementsClient{
				reviews:     tc.reviews,
				reviewsErr:  tc.reviewsErr,
				permissions: tc.permissions,
				permErrs:    tc.permErrs,
			}
			check, _ := newRequirementCheck(client, &github.PullRequest{Number: 7, Author: "author", Mergeable: boolPtr(false)})

			got := check.evaluate(context.Background(), []string{MutationRequirementApproved})

			assert.Equal(t, tc.want, got)
			assert.Equal(t, 1, client.reviewCalls)
			assert.Equal(t, tc.wantAsked, client.permAsked)
			assert.Zero(t, client.getCalls, "approved alone never reads the pull request")
		})
	}
}

func TestMutationRequirements_ApprovedFailureIsLogged(t *testing.T) {
	logs := captureLogs(t)
	client := &fakeRequirementsClient{reviewsErr: errors.New("github: 502")}
	check, _ := newRequirementCheck(client, &github.PullRequest{Number: 7, Author: "author"})

	check.evaluate(context.Background(), []string{MutationRequirementApproved})

	rec := findRecord(logs(), "listing reviews for a mutation requirement")
	require.NotNil(t, rec)
	assert.Equal(t, "ERROR", rec["level"])
	assert.Equal(t, "github: 502", rec["error"])
}

func TestMutationRequirements_Mergeable(t *testing.T) {
	cases := []struct {
		name      string
		initial   *bool
		reads     []*bool
		getErr    error
		want      []unmetRequirement
		wantReads int
	}{
		{
			name:    "true",
			initial: boolPtr(true),
		},
		{
			name:    "false is a merge conflict",
			initial: boolPtr(false),
			want:    []unmetRequirement{{MutationRequirementMergeable, reasonConflict}},
		},
		{
			name:      "nil then true within the retries",
			reads:     []*bool{nil, boolPtr(true)},
			wantReads: 2,
		},
		{
			name:      "nil then false within the retries",
			reads:     []*bool{boolPtr(false)},
			want:      []unmetRequirement{{MutationRequirementMergeable, reasonConflict}},
			wantReads: 1,
		},
		{
			name:      "nil throughout is not yet known",
			reads:     []*bool{nil},
			want:      []unmetRequirement{{MutationRequirementMergeable, reasonNotYetKnown}},
			wantReads: 3,
		},
		{
			name:      "a failed read could not be checked",
			getErr:    errors.New("github: 502"),
			want:      []unmetRequirement{{MutationRequirementMergeable, reasonCouldNotCheck}},
			wantReads: 1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := &fakeRequirementsClient{mergeable: tc.reads, getErr: tc.getErr}
			check, waits := newRequirementCheck(client, &github.PullRequest{Number: 7, Author: "author", Mergeable: tc.initial})

			got := check.evaluate(context.Background(), []string{MutationRequirementMergeable})

			assert.Equal(t, tc.want, got)
			assert.Equal(t, tc.wantReads, client.getCalls)
			// One wait before every read, each a second.
			wantWaits := make([]time.Duration, tc.wantReads)
			for i := range wantWaits {
				wantWaits[i] = time.Second
			}
			assert.Equal(t, wantWaits, append([]time.Duration{}, *waits...))
			assert.Zero(t, client.reviewCalls, "mergeable alone never lists reviews")
		})
	}
}

func TestMutationRequirements_MergeableFailureIsLogged(t *testing.T) {
	logs := captureLogs(t)
	client := &fakeRequirementsClient{getErr: errors.New("github: 502")}
	check, _ := newRequirementCheck(client, &github.PullRequest{Number: 7, Author: "author"})

	check.evaluate(context.Background(), []string{MutationRequirementMergeable})

	rec := findRecord(logs(), "reading the pull request for a mutation requirement")
	require.NotNil(t, rec)
	assert.Equal(t, "ERROR", rec["level"])
	assert.Equal(t, "github: 502", rec["error"])
}

// A canceled wait stops the retries rather than reading on, and fails
// closed.
func TestMutationRequirements_MergeableWaitCanceled(t *testing.T) {
	client := &fakeRequirementsClient{mergeable: []*bool{boolPtr(true)}}
	check, _ := newRequirementCheck(client, &github.PullRequest{Number: 7, Author: "author"})
	check.sleep = nil
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	got := check.evaluate(ctx, []string{MutationRequirementMergeable})

	assert.Equal(t, []unmetRequirement{{MutationRequirementMergeable, reasonCouldNotCheck}}, got)
	assert.Zero(t, client.getCalls)
}

func TestMutationRequirements_EmptySetAsksNothing(t *testing.T) {
	client := &fakeRequirementsClient{}
	check, waits := newRequirementCheck(client, &github.PullRequest{Number: 7, Author: "author"})

	assert.Empty(t, check.evaluate(context.Background(), nil))
	assert.Zero(t, client.reviewCalls)
	assert.Zero(t, client.getCalls)
	assert.Empty(t, *waits)
}

// Both unmet are named together, in a fixed order whatever order the
// setting was written in.
func TestMutationRequirements_BothUnmet(t *testing.T) {
	client := &fakeRequirementsClient{}
	check, _ := newRequirementCheck(client, &github.PullRequest{Number: 7, Author: "author", Mergeable: boolPtr(false)})

	got := check.evaluate(context.Background(), []string{MutationRequirementMergeable, MutationRequirementApproved})

	assert.Equal(t, []unmetRequirement{
		{MutationRequirementApproved, reasonUnmet},
		{MutationRequirementMergeable, reasonConflict},
	}, got)
}

func TestMutationRequirementsReply(t *testing.T) {
	cases := []struct {
		name  string
		unmet []unmetRequirement
		want  string
	}{
		{
			name: "both, as in the design",
			unmet: []unmetRequirement{
				{MutationRequirementApproved, reasonUnmet},
				{MutationRequirementMergeable, reasonConflict},
			},
			want: "`/helmfile apply` was not run. This pull request must first:\n" +
				"- be approved by someone with write access other than its author\n" +
				"- have no merge conflicts (GitHub reports one)",
		},
		{
			name:  "mergeable not yet known",
			unmet: []unmetRequirement{{MutationRequirementMergeable, reasonNotYetKnown}},
			want: "`/helmfile apply` was not run. This pull request must first:\n" +
				"- have no merge conflicts (GitHub has not finished checking; try again in a moment)",
		},
		{
			name: "could not check either",
			unmet: []unmetRequirement{
				{MutationRequirementApproved, reasonCouldNotCheck},
				{MutationRequirementMergeable, reasonCouldNotCheck},
			},
			want: "`/helmfile apply` was not run. This pull request must first:\n" +
				"- be approved by someone with write access other than its author (turnip could not check this; try again)\n" +
				"- have no merge conflicts (turnip could not check this; try again)",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, mutationRequirementsReply("/helmfile apply", tc.unmet))
		})
	}
}
