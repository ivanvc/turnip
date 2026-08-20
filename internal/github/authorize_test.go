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

func (f *fakeGitHubClient) GenerateInstallationToken(ctx context.Context) (string, error) {
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
func (f *fakeGitHubClient) PostComment(ctx context.Context, owner, repo string, prNumber int, body string) (int64, error) {
	panic("not used by Authorizer")
}
func (f *fakeGitHubClient) UpdateComment(ctx context.Context, owner, repo string, commentID int64, body string) error {
	panic("not used by Authorizer")
}
func (f *fakeGitHubClient) IsCollaborator(ctx context.Context, owner, repo, username string) (bool, error) {
	panic("not used by Authorizer")
}

var _ GitHubClient = (*fakeGitHubClient)(nil)

func TestAuthorizer_IsCollaborator(t *testing.T) {
	fake := &fakeGitHubClient{permission: "read"}
	a := NewAuthorizer(fake)

	ok, err := a.IsCollaborator(context.Background(), "owner", "repo", "alice")
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestAuthorizer_IsCollaborator_Error(t *testing.T) {
	fake := &fakeGitHubClient{err: errors.New("404 not a collaborator")}
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
	fake := &fakeGitHubClient{permission: "write"}
	a := NewAuthorizer(fake)
	now := time.Now()
	a.now = func() time.Time { return now }

	_, err := a.IsCollaborator(context.Background(), "owner", "repo", "alice")
	require.NoError(t, err)
	_, err = a.IsCollaborator(context.Background(), "owner", "repo", "alice")
	require.NoError(t, err)

	assert.Equal(t, 1, fake.callCount(), "second call should hit the cache")
}

func TestAuthorizer_RefetchesAfterTTLExpires(t *testing.T) {
	fake := &fakeGitHubClient{permission: "write"}
	a := NewAuthorizer(fake)
	now := time.Now()
	a.now = func() time.Time { return now }

	_, err := a.IsCollaborator(context.Background(), "owner", "repo", "alice")
	require.NoError(t, err)

	now = now.Add(authorizationCacheTTL + time.Second)

	_, err = a.IsCollaborator(context.Background(), "owner", "repo", "alice")
	require.NoError(t, err)

	assert.Equal(t, 2, fake.callCount(), "cache should have expired")
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
