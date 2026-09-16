package orchestrator

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivanvc/turnip/internal/github"
	"github.com/ivanvc/turnip/internal/lock"
)

// fakeCommentEventClient is a scriptable github.GitHubClient covering
// everything HandleIssueComment's flow needs: collaborator/permission
// checks, turnip.yaml/PR lookups, and comment posting. PostComment is
// called both synchronously (reply comments) and from HandleIssueComment's
// detached goroutine (the actual plan/apply results), so posted needs a
// mutex — tests read it via require.Eventually from the main goroutine
// while that background goroutine may still be writing.
type fakeCommentEventClient struct {
	github.GitHubClient
	permission string
	files      map[string][]byte
	pr         *github.PullRequest

	mu     sync.Mutex
	posted []string
}

func (f *fakeCommentEventClient) postedComments() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.posted...)
}

func (f *fakeCommentEventClient) GetCollaboratorPermission(ctx context.Context, owner, repo, username string) (string, error) {
	// Authorizer.IsCollaborator only checks whether this call errors, not
	// the permission string's value — an empty permission simulates
	// GitHub's real behavior for a non-collaborator (the underlying API
	// call itself fails, e.g. 404), matching what a real non-collaborator
	// author would produce.
	if f.permission == "" {
		return "", errors.New("not a collaborator")
	}
	return f.permission, nil
}
func (f *fakeCommentEventClient) GetPullRequest(ctx context.Context, owner, repo string, prNumber int) (*github.PullRequest, error) {
	return f.pr, nil
}
func (f *fakeCommentEventClient) GetFile(ctx context.Context, owner, repo, path, ref string) ([]byte, error) {
	data, ok := f.files[path]
	if !ok {
		return nil, github.ErrFileNotFound
	}
	return data, nil
}
func (f *fakeCommentEventClient) PostComment(ctx context.Context, owner, repo string, prNumber int, body string) (*github.PostedComment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.posted = append(f.posted, body)
	return &github.PostedComment{ID: int64(len(f.posted)), NodeID: "node"}, nil
}
func (f *fakeCommentEventClient) CreateCheckRun(ctx context.Context, owner, repo string, opts github.CheckRunOptions) (int64, error) {
	return 555, nil
}
func (f *fakeCommentEventClient) UpdateCheckRun(ctx context.Context, owner, repo string, checkRunID int64, opts github.CheckRunOptions) error {
	return nil
}
func (f *fakeCommentEventClient) GenerateInstallationToken(ctx context.Context) (string, error) {
	return "token", nil
}

func testCommentOrchestrator(t *testing.T, locks lock.LockManager) *Orchestrator {
	t.Helper()
	client := newTestRedisClient(t)
	return &Orchestrator{
		locks:              locks,
		jobs:               &fakeJobCreator{t: t, redis: client, result: github.ProjectResult{Success: true}},
		plugins:            testRegistry(),
		records:            newRecordStore(client),
		redis:              client,
		installationClient: func(id int64) github.GitHubClient { return nil }, // executeOne doesn't reconstruct clients
	}
}

func commentEvent(body, author string) *github.WebhookEvent {
	return &github.WebhookEvent{
		Action:       "created",
		Repository:   github.Repository{Owner: "owner", Name: "repo"},
		PullRequest:  &github.PullRequest{Number: 42},
		Comment:      &github.Comment{Body: body, Author: author},
		Installation: github.Installation{ID: 1},
	}
}

func TestHandleIssueComment_NoTriggerIsSilentNoop(t *testing.T) {
	o := testCommentOrchestrator(t, &fakeLockManager{})
	client := &fakeCommentEventClient{permission: "write"}
	require.NoError(t, callHandleIssueComment(o, client, commentEvent("just chatting", "alice")))
	assert.Empty(t, client.posted)
}

// The mirror of TestHandlePlanTrigger_MissingConfigPostsNothing: an
// automatic plan stays silent when a repository has no turnip.yaml, but
// an explicit Trigger Command must still say so — here a human asked
// turnip to do something, so silence would be the confusing answer.
func TestHandleIssueComment_MissingConfigStillPostsComment(t *testing.T) {
	o := testCommentOrchestrator(t, &fakeLockManager{})
	client := &fakeCommentEventClient{
		permission: "write",
		files:      map[string][]byte{},
		pr:         &github.PullRequest{Number: 42, HeadSHA: "abc"},
	}

	require.NoError(t, callHandleIssueComment(o, client, commentEvent("/turnip diff", "alice")))

	require.Len(t, client.postedComments(), 1)
	assert.Contains(t, client.postedComments()[0], "not found")
}

func TestHandleIssueComment_NonCollaboratorRejectedBeforeAnyCommand(t *testing.T) {
	o := testCommentOrchestrator(t, &fakeLockManager{})
	client := &fakeCommentEventClient{permission: ""}
	require.NoError(t, callHandleIssueComment(o, client, commentEvent("/turnip plan", "mallory")))

	require.Len(t, client.posted, 1)
	assert.Contains(t, client.posted[0], "does not have permission")
}

func TestHandleIssueComment_MalformedLineRepliedAlongsideWellFormedProcessed(t *testing.T) {
	locks := &fakeLockManager{}
	o := testCommentOrchestrator(t, locks)
	client := &fakeCommentEventClient{
		permission: "write",
		files:      map[string][]byte{"turnip.yaml": []byte(validTurnipYAML)},
		pr:         &github.PullRequest{Number: 42, HeadSHA: "abc"},
	}

	require.NoError(t, callHandleIssueComment(o, client, commentEvent("/turnip\n/turnip plan", "alice")))

	require.Eventually(t, func() bool { return len(client.postedComments()) >= 2 }, 2*time.Second, 10*time.Millisecond)
	found := false
	for _, body := range client.postedComments() {
		if strings.Contains(body, "couldn't be parsed") {
			found = true
		}
	}
	assert.True(t, found, "expected a reply about the malformed line")
}

func TestHandleIssueComment_SequentialCommandsOrdered(t *testing.T) {
	var mu sync.Mutex
	var order []string
	locks := &fakeLockManager{
		acquireLockFunc: func(ctx context.Context, projectKey string, prNumber int, url, lockedBy string) (bool, error) {
			mu.Lock()
			order = append(order, "acquire")
			mu.Unlock()
			return true, nil
		},
		isLockedByPRFunc: func(ctx context.Context, projectKey string, prNumber int) (bool, error) {
			mu.Lock()
			order = append(order, "islocked")
			mu.Unlock()
			return true, nil
		},
	}
	o := testCommentOrchestrator(t, locks)
	client := &fakeCommentEventClient{
		permission: "write",
		files:      map[string][]byte{"turnip.yaml": []byte(validTurnipYAML)},
		pr:         &github.PullRequest{Number: 42, HeadSHA: "abc"},
	}

	require.NoError(t, callHandleIssueComment(o, client, commentEvent("/turnip diff helm-a\n/turnip apply helm-a", "alice")))

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(order) >= 2
	}, 2*time.Second, 10*time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []string{"acquire", "islocked"}, order)
}

func TestHandleIssueComment_UnlockNeverCreatesJobOrRecord(t *testing.T) {
	locks := &fakeLockManager{}
	o := testCommentOrchestrator(t, locks)
	client := &fakeCommentEventClient{
		permission: "write",
		files:      map[string][]byte{"turnip.yaml": []byte(validTurnipYAML)},
		pr:         &github.PullRequest{Number: 42, HeadSHA: "abc"},
	}

	require.NoError(t, callHandleIssueComment(o, client, commentEvent("/turnip unlock", "alice")))

	keys, err := o.records.ScanOperationKeys(context.Background())
	require.NoError(t, err)
	assert.Empty(t, keys)
	require.Len(t, client.posted, 1)
	assert.Contains(t, client.posted[0], "Unlocked")
}

// callHandleIssueComment installs a fake installationClient returning
// client before calling HandleIssueComment, since the real method always
// reconstructs its GitHubClient via o.installationClient rather than
// accepting one directly.
func callHandleIssueComment(o *Orchestrator, client github.GitHubClient, event *github.WebhookEvent) error {
	o.installationClient = func(id int64) github.GitHubClient { return client }
	return o.HandleIssueComment(context.Background(), event)
}
