package github

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeGitHubClient is a test-local GitHubClient recording calls and
// returning a scripted permission/error for GetCollaboratorPermission.
// Every other method panics if called — Authorizer must never need them.
type fakeGitHubClient struct {
	mu    sync.Mutex
	calls int

	permission string
	err        error

	// collaborator answers the 204/404 endpoint, independently of the
	// permission level above. The two are separate questions and a fake
	// that derived one from the other could not express the case this
	// slice exists to fix.
	collaborator    bool
	collaboratorErr error
	collabCalls     int
}

func (f *fakeGitHubClient) GetCollaboratorPermission(ctx context.Context, owner, repo, username string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.permission, f.err
}

func (f *fakeGitHubClient) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func (f *fakeGitHubClient) GenerateInstallationToken(ctx context.Context, _ TokenScope) (InstallationToken, error) {
	panic("not used by Authorizer")
}
func (f *fakeGitHubClient) GetFile(ctx context.Context, owner, repo, path, ref string) ([]byte, error) {
	panic("not used by Authorizer")
}
func (f *fakeGitHubClient) GetModifiedFiles(ctx context.Context, owner, repo string, prNumber int) ([]string, error) {
	panic("not used by Authorizer")
}
func (f *fakeGitHubClient) GetPullRequest(ctx context.Context, owner, repo string, prNumber int) (*PullRequest, error) {
	panic("not used by Authorizer")
}
func (f *fakeGitHubClient) CreateCheckRun(ctx context.Context, owner, repo string, opts CheckRunOptions) (int64, error) {
	panic("not used by Authorizer")
}
func (f *fakeGitHubClient) UpdateCheckRun(ctx context.Context, owner, repo string, checkRunID int64, opts CheckRunOptions) error {
	panic("not used by Authorizer")
}
func (f *fakeGitHubClient) PostComment(ctx context.Context, owner, repo string, prNumber int, body string) (*PostedComment, error) {
	panic("not used by Authorizer")
}
func (f *fakeGitHubClient) UpdateComment(ctx context.Context, owner, repo string, commentID int64, body string) error {
	panic("not used by Authorizer")
}
func (f *fakeGitHubClient) IsCollaborator(ctx context.Context, owner, repo, username string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.collabCalls++
	return f.collaborator, f.collaboratorErr
}

func (f *fakeGitHubClient) collaboratorCallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.collabCalls
}
func (f *fakeGitHubClient) MinimizeComment(ctx context.Context, nodeID string) error {
	panic("not used by Authorizer")
}

var _ GitHubClient = (*fakeGitHubClient)(nil)

func TestAuthorizer_IsCollaborator(t *testing.T) {
	fake := &fakeGitHubClient{permission: "read", collaborator: true}
	a := NewAuthorizer(fake)

	ok, err := a.IsCollaborator(context.Background(), "owner", "repo", "alice")
	require.NoError(t, err)
	assert.True(t, ok)
}

// A lookup that fails tells turnip nothing, so it must refuse and say so
// rather than answering the question it could not ask.
//
// GitHub requires write, maintain or admin to read collaborator
// information, so an App installation with narrower permissions lands
// here — and an error that became a quiet false would make that
// indistinguishable from a repository where nobody is a collaborator.
func TestAuthorizer_IsCollaborator_Error(t *testing.T) {
	fake := &fakeGitHubClient{collaboratorErr: errors.New("403 forbidden")}
	a := NewAuthorizer(fake)

	ok, err := a.IsCollaborator(context.Background(), "owner", "repo", "mallory")
	require.Error(t, err)
	assert.False(t, ok)
}

func TestAuthorizer_HasWritePermission(t *testing.T) {
	tests := []struct {
		permission string
		want       bool
	}{
		{"none", false},
		{"read", false},
		{"triage", false},
		{"write", true},
		{"maintain", true},
		{"admin", true},
		{"something-unrecognized", false},
	}

	for _, tt := range tests {
		t.Run(tt.permission, func(t *testing.T) {
			fake := &fakeGitHubClient{permission: tt.permission}
			a := NewAuthorizer(fake)

			got, err := a.HasWritePermission(context.Background(), "owner", "repo", "alice")
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestAuthorizer_CachesWithinTTL(t *testing.T) {
	fake := &fakeGitHubClient{permission: "write", collaborator: true}
	a := NewAuthorizer(fake)
	now := time.Now()
	a.now = func() time.Time { return now }

	_, err := a.IsCollaborator(context.Background(), "owner", "repo", "alice")
	require.NoError(t, err)
	_, err = a.IsCollaborator(context.Background(), "owner", "repo", "alice")
	require.NoError(t, err)

	// Counted on the collaborator endpoint, which is what IsCollaborator
	// now asks. It no longer touches the permission endpoint at all.
	assert.Equal(t, 1, fake.collaboratorCallCount(), "second call should hit the cache")
	assert.Zero(t, fake.callCount(), "collaborator status does not cost a permission lookup")
}

func TestAuthorizer_RefetchesAfterTTLExpires(t *testing.T) {
	fake := &fakeGitHubClient{permission: "write", collaborator: true}
	a := NewAuthorizer(fake)
	now := time.Now()
	a.now = func() time.Time { return now }

	_, err := a.IsCollaborator(context.Background(), "owner", "repo", "alice")
	require.NoError(t, err)

	now = now.Add(authorizationCacheTTL + time.Second)

	_, err = a.IsCollaborator(context.Background(), "owner", "repo", "alice")
	require.NoError(t, err)

	assert.Equal(t, 2, fake.collaboratorCallCount(), "cache should have expired")
}

func TestAuthorizer_ConcurrentCallsForDifferentUsers(t *testing.T) {
	fake := &fakeGitHubClient{permission: "write"}
	a := NewAuthorizer(fake)

	const numUsers = 20
	var wg sync.WaitGroup
	wg.Add(numUsers)
	for i := range numUsers {
		go func(i int) {
			defer wg.Done()
			username := "user" + string(rune('a'+i))
			_, err := a.HasWritePermission(context.Background(), "owner", "repo", username)
			assert.NoErrorf(t, err, "HasWritePermission(%s)", username)
		}(i)
	}
	wg.Wait()
}

// The defect, stated as a fixture.
//
// GitHub answers two different questions here. Asked for a *permission
// level*, it returns a 200 carrying a string; asked whether an account is
// a *collaborator*, it answers 204 or 404. The gate inferred the second
// from the first succeeding, which admits every account GitHub will answer
// about at all.
//
// This is the only fixture that separates them: a successful permission
// lookup describing no useful access, alongside a definitive "not a
// collaborator". Against the unfixed Authorizer it reports true.
func TestAuthorizer_IsCollaborator_SuccessfulLookupIsNotCollaboration(t *testing.T) {
	for _, permission := range []string{"none", "read"} {
		t.Run("permission "+permission, func(t *testing.T) {
			fake := &fakeGitHubClient{permission: permission, collaborator: false}
			a := NewAuthorizer(fake)

			ok, err := a.IsCollaborator(context.Background(), "owner", "repo", "mallory")

			require.NoError(t, err, "GitHub answered; it just said no")
			assert.False(t, ok,
				"a permission lookup that succeeds is not evidence of collaboration")
		})
	}
}

// Requirement 4: two questions about one account cost at most one call
// each, and a repeat costs none.
func TestAuthorizer_BothAnswersAreCachedIndependently(t *testing.T) {
	fake := &fakeGitHubClient{permission: "write", collaborator: true}
	a := NewAuthorizer(fake)
	ctx := context.Background()

	for range 3 {
		ok, err := a.IsCollaborator(ctx, "owner", "repo", "alice")
		require.NoError(t, err)
		require.True(t, ok)

		write, err := a.HasWritePermission(ctx, "owner", "repo", "alice")
		require.NoError(t, err)
		require.True(t, write)
	}

	assert.Equal(t, 1, fake.collaboratorCallCount(), "collaborator status fetched once")
	assert.Equal(t, 1, fake.callCount(), "permission level fetched once")
}
