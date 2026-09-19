package orchestrator

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivanvc/turnip/internal/github"
)

// fakePRClient is a scriptable github.GitHubClient for pullrequest.go's
// tests: turnip.yaml lookups (root and .github fallback), modified
// files, and comment posting. PostComment can be reached from
// handlePlanTrigger's detached goroutine, so posted/getFileCalls are
// mutex-guarded — mirroring fakeCommentEventClient's same fix (comment_test.go).
type fakePRClient struct {
	github.GitHubClient
	files          map[string][]byte // path -> content; absent means ErrFileNotFound
	modifiedFiles  []string
	postCommentErr error

	mu           sync.Mutex
	posted       []string
	getFileCalls []string
}

func (f *fakePRClient) GetFile(ctx context.Context, owner, repo, path, ref string) ([]byte, error) {
	f.mu.Lock()
	f.getFileCalls = append(f.getFileCalls, path)
	f.mu.Unlock()
	data, ok := f.files[path]
	if !ok {
		return nil, fmt.Errorf("github: not found: %w", github.ErrFileNotFound)
	}
	return data, nil
}

func (f *fakePRClient) GetModifiedFiles(ctx context.Context, owner, repo string, prNumber int) ([]string, error) {
	return f.modifiedFiles, nil
}

func (f *fakePRClient) PostComment(ctx context.Context, owner, repo string, prNumber int, body string) (*github.PostedComment, error) {
	if f.postCommentErr != nil {
		return nil, f.postCommentErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.posted = append(f.posted, body)
	return &github.PostedComment{ID: 1, NodeID: "node-1"}, nil
}
func (f *fakePRClient) CreateCheckRun(ctx context.Context, owner, repo string, opts github.CheckRunOptions) (int64, error) {
	return 555, nil
}
func (f *fakePRClient) UpdateCheckRun(ctx context.Context, owner, repo string, checkRunID int64, opts github.CheckRunOptions) error {
	return nil
}
func (f *fakePRClient) GenerateInstallationToken(ctx context.Context) (string, error) {
	return "token", nil
}

func (f *fakePRClient) postedComments() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.posted...)
}

func (f *fakePRClient) getFileCallLog() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.getFileCalls...)
}

const validTurnipYAML = `
schemaVersion: v1alpha2
projects:
  - name: helm-a
    directory: a
    uses: helmfile
    whenModified:
      - "a/**"
`

func testPullRequestOrchestrator(t *testing.T) (*Orchestrator, *redis.Client) {
	t.Helper()
	client := newTestRedisClient(t)
	o := &Orchestrator{
		locks:   &fakeLockManager{},
		jobs:    &fakeJobCreator{t: t},
		plugins: testRegistry(),
		records: newRecordStore(client),
		redis:   client,
	}
	return o, client
}

func TestHandlePlanTrigger_MatchedProjectExecutesAndPosts(t *testing.T) {
	client := newTestRedisClient(t)
	fakeClient := &fakePRClient{
		files:         map[string][]byte{"turnip.yaml": []byte(validTurnipYAML)},
		modifiedFiles: []string{"a/main.tf"},
	}
	o := &Orchestrator{
		locks:              &fakeLockManager{},
		jobs:               &fakeJobCreator{t: t, redis: client, result: github.ProjectResult{Success: true, ProjectName: "helm-a"}},
		plugins:            testRegistry(),
		records:            newRecordStore(client),
		redis:              client,
		installationClient: func(id int64) github.GitHubClient { return fakeClient },
	}

	event := &github.WebhookEvent{
		Action:       "opened",
		Repository:   github.Repository{Owner: "owner", Name: "repo"},
		PullRequest:  &github.PullRequest{Number: 42, HeadSHA: "abc"},
		Installation: github.Installation{ID: 1},
	}
	require.NoError(t, o.HandlePullRequest(context.Background(), event))

	require.Eventually(t, func() bool { return len(fakeClient.postedComments()) >= 1 }, 2*time.Second, 10*time.Millisecond)
	assert.Contains(t, fakeClient.postedComments()[0], "helm-a")
}

func TestHandlePullRequest_DispatchesOnAction(t *testing.T) {
	o, client := testPullRequestOrchestrator(t)
	fakeClient := &fakePRClient{files: map[string][]byte{"turnip.yaml": []byte(validTurnipYAML)}}
	o.installationClient = func(id int64) github.GitHubClient { return fakeClient }
	_ = client

	event := &github.WebhookEvent{
		Action:       "labeled", // not opened/synchronize/closed
		Repository:   github.Repository{Owner: "owner", Name: "repo"},
		PullRequest:  &github.PullRequest{Number: 42, HeadSHA: "abc"},
		Installation: github.Installation{ID: 1},
	}
	require.NoError(t, o.HandlePullRequest(context.Background(), event))
	assert.Empty(t, fakeClient.postedComments())
}

// A draft is work its author marked unfinished, so turnip does not act on
// it of its own accord.
//
// The assertion is the *absence of side effects*, not that the call
// returned nil — a guard in the wrong place would also return nil. What
// proves the guard ran early is that turnip.yaml was never fetched:
// handlePlanTrigger's first act is that lookup, so an empty getFileCalls
// means nothing downstream of the guard executed (Requirement 1.4).
func TestHandlePullRequest_DraftIsNotPlannedAutomatically(t *testing.T) {
	for _, action := range []string{"opened", "synchronize"} {
		t.Run(action, func(t *testing.T) {
			o, _ := testPullRequestOrchestrator(t)
			fakeClient := &fakePRClient{
				files:         map[string][]byte{"turnip.yaml": []byte(validTurnipYAML)},
				modifiedFiles: []string{"a/main.tf"},
			}
			o.installationClient = func(id int64) github.GitHubClient { return fakeClient }

			event := &github.WebhookEvent{
				Action:       action,
				Repository:   github.Repository{Owner: "owner", Name: "repo"},
				PullRequest:  &github.PullRequest{Number: 42, HeadSHA: "abc", Draft: true},
				Installation: github.Installation{ID: 1},
			}
			require.NoError(t, o.HandlePullRequest(context.Background(), event))

			assert.Empty(t, fakeClient.postedComments(), "a skipped draft posts nothing")
			assert.Empty(t, fakeClient.getFileCallLog(),
				"a skipped draft must not even fetch turnip.yaml")
		})
	}
}

// Marking a pull request ready produces a plan. GitHub sends
// ready_for_review with the payload's draft field already false, so the
// same guard lets it through — no special case, no remembered state.
//
// Without this action handled, skipping drafts would be a trap: a draft
// marked ready would sit with no plan until someone pushed again.
func TestHandlePullRequest_ReadyForReviewPlans(t *testing.T) {
	client := newTestRedisClient(t)
	fakeClient := &fakePRClient{
		files:         map[string][]byte{"turnip.yaml": []byte(validTurnipYAML)},
		modifiedFiles: []string{"a/main.tf"},
	}
	o := &Orchestrator{
		locks:              &fakeLockManager{},
		jobs:               &fakeJobCreator{t: t, redis: client, result: github.ProjectResult{Success: true, ProjectName: "helm-a"}},
		plugins:            testRegistry(),
		records:            newRecordStore(client),
		redis:              client,
		installationClient: func(id int64) github.GitHubClient { return fakeClient },
	}

	event := &github.WebhookEvent{
		Action:       "ready_for_review",
		Repository:   github.Repository{Owner: "owner", Name: "repo"},
		PullRequest:  &github.PullRequest{Number: 42, HeadSHA: "abc", Draft: false},
		Installation: github.Installation{ID: 1},
	}
	require.NoError(t, o.HandlePullRequest(context.Background(), event))

	require.Eventually(t, func() bool { return len(fakeClient.postedComments()) >= 1 }, 2*time.Second, 10*time.Millisecond)
	assert.Contains(t, fakeClient.postedComments()[0], "helm-a")
}

// The one regression this slice can introduce, and it is silent: hoisting
// the draft guard above the switch would skip "closed" too, so a draft's
// Locks would never be released — for exactly the pull requests most
// likely to be abandoned rather than closed cleanly.
func TestHandlePullRequest_ClosedDraftStillReleasesLocks(t *testing.T) {
	var released []string
	locks := &fakeLockManager{
		isLockedByPRFunc: func(ctx context.Context, projectKey string, prNumber int) (bool, error) {
			return true, nil
		},
		releaseLockFunc: func(ctx context.Context, projectKey string, prNumber int) error {
			released = append(released, projectKey)
			return nil
		},
	}
	client := newTestRedisClient(t)
	fakeClient := &fakePRClient{files: map[string][]byte{"turnip.yaml": []byte(validTurnipYAML)}}
	o := &Orchestrator{
		locks:              locks,
		plugins:            testRegistry(),
		records:            newRecordStore(client),
		redis:              client,
		installationClient: func(id int64) github.GitHubClient { return fakeClient },
	}

	event := &github.WebhookEvent{
		Action:       "closed",
		Repository:   github.Repository{Owner: "owner", Name: "repo"},
		PullRequest:  &github.PullRequest{Number: 42, HeadSHA: "abc", Draft: true},
		Installation: github.Installation{ID: 1},
	}
	require.NoError(t, o.HandlePullRequest(context.Background(), event))

	assert.NotEmpty(t, released,
		"closing a draft must release its Locks — the draft guard must not reach this arm")
}

func TestHandlePlanTrigger_ZeroMatchedProjectsTakesNoAction(t *testing.T) {
	o, _ := testPullRequestOrchestrator(t)
	client := &fakePRClient{
		files:         map[string][]byte{"turnip.yaml": []byte(validTurnipYAML)},
		modifiedFiles: []string{"unrelated/file.txt"},
	}

	event := &github.WebhookEvent{
		Repository:  github.Repository{Owner: "owner", Name: "repo"},
		PullRequest: &github.PullRequest{Number: 42, HeadSHA: "abc"},
	}
	require.NoError(t, o.handlePlanTrigger(context.Background(), client, event))

	time.Sleep(50 * time.Millisecond) // nothing should be posted, even async
	assert.Empty(t, client.postedComments())
}

// The dedicated directory is tried first and the repository root is the
// fallback — that order is deliberate, so a repository migrating to
// `.turnip/` takes effect by adding the new file rather than needing a
// second step to remove the old one.
func TestHandlePlanTrigger_RootFileIsTheFallback(t *testing.T) {
	o, _ := testPullRequestOrchestrator(t)
	client := &fakePRClient{
		files:         map[string][]byte{"turnip.yaml": []byte(validTurnipYAML)},
		modifiedFiles: []string{"unrelated/file.txt"},
	}

	event := &github.WebhookEvent{
		Repository:  github.Repository{Owner: "owner", Name: "repo"},
		PullRequest: &github.PullRequest{Number: 42, HeadSHA: "abc"},
	}
	require.NoError(t, o.handlePlanTrigger(context.Background(), client, event))

	require.Len(t, client.getFileCallLog(), 2)
	assert.Equal(t, ".turnip/config.yaml", client.getFileCallLog()[0])
	assert.Equal(t, "turnip.yaml", client.getFileCallLog()[1])
}

// The dedicated location wins outright when both exist; the two are never
// merged.
func TestHandlePlanTrigger_DedicatedDirectoryWinsOverRoot(t *testing.T) {
	o, _ := testPullRequestOrchestrator(t)
	client := &fakePRClient{
		files: map[string][]byte{
			".turnip/config.yaml": []byte(validTurnipYAML),
			"turnip.yaml":         []byte("schemaVersion: v1alpha2\nprojects: []\n"),
		},
		modifiedFiles: []string{"unrelated/file.txt"},
	}

	event := &github.WebhookEvent{
		Repository:  github.Repository{Owner: "owner", Name: "repo"},
		PullRequest: &github.PullRequest{Number: 42, HeadSHA: "abc"},
	}
	require.NoError(t, o.handlePlanTrigger(context.Background(), client, event))

	assert.Equal(t, []string{".turnip/config.yaml"}, client.getFileCallLog(),
		"the root file is never fetched once the dedicated one is found")
}

// A repository with no turnip.yaml hasn't opted into turnip, and this
// handler runs on every PR open and every push to one — commenting would
// put "turnip.yaml was not found" on every pull request in the
// repository. An explicit Trigger Comment still reports it; see
// comment.go.
func TestHandlePlanTrigger_MissingConfigPostsNothing(t *testing.T) {
	o, _ := testPullRequestOrchestrator(t)
	client := &fakePRClient{files: map[string][]byte{}}

	event := &github.WebhookEvent{
		Repository:  github.Repository{Owner: "owner", Name: "repo"},
		PullRequest: &github.PullRequest{Number: 42, HeadSHA: "abc"},
	}
	require.NoError(t, o.handlePlanTrigger(context.Background(), client, event))

	time.Sleep(50 * time.Millisecond) // nothing should be posted, even async
	assert.Empty(t, client.postedComments())
	assert.Equal(t, []string{".turnip/config.yaml", "turnip.yaml"}, client.getFileCallLog(),
		"both locations are checked before giving up, and no more than both — this runs on every PR in every installed repository")
}

// An invalid turnip.yaml is the opposite case: that repository *has*
// opted in, so breaking its config must stay visible rather than being
// silently skipped on every push.
func TestHandlePlanTrigger_InvalidConfigStillPostsComment(t *testing.T) {
	o, _ := testPullRequestOrchestrator(t)
	client := &fakePRClient{files: map[string][]byte{
		"turnip.yaml": []byte("schemaVersion: v1alpha2\nprojects:\n  - name: broken\n"), // no directory/uses
	}}

	event := &github.WebhookEvent{
		Repository:  github.Repository{Owner: "owner", Name: "repo"},
		PullRequest: &github.PullRequest{Number: 42, HeadSHA: "abc"},
	}
	require.NoError(t, o.handlePlanTrigger(context.Background(), client, event))

	require.Len(t, client.postedComments(), 1)
	assert.Contains(t, client.postedComments()[0], "invalid")
}

func TestHandlePRClosed_ReleasesOnlyLocksHeldByThisPR(t *testing.T) {
	var released []string
	locks := &fakeLockManager{
		isLockedByPRFunc: func(ctx context.Context, projectKey string, prNumber int) (bool, error) {
			return projectKey == "owner/repo/helm-a", nil
		},
		releaseLockFunc: func(ctx context.Context, projectKey string, prNumber int) error {
			released = append(released, projectKey)
			return nil
		},
	}
	client := newTestRedisClient(t)
	o := &Orchestrator{locks: locks, records: newRecordStore(client), redis: client}

	prClient := &fakePRClient{files: map[string][]byte{"turnip.yaml": []byte(validTurnipYAML)}}
	event := &github.WebhookEvent{
		Repository:  github.Repository{Owner: "owner", Name: "repo"},
		PullRequest: &github.PullRequest{Number: 42, HeadSHA: "abc"},
	}
	require.NoError(t, o.handlePRClosed(context.Background(), prClient, event))

	assert.Equal(t, []string{"owner/repo/helm-a"}, released)
	require.Len(t, prClient.postedComments(), 1)
	assert.Contains(t, prClient.postedComments()[0], "helm-a")
}

func TestHandlePRClosed_NoLocksHeldPostsNoComment(t *testing.T) {
	locks := &fakeLockManager{
		isLockedByPRFunc: func(ctx context.Context, projectKey string, prNumber int) (bool, error) {
			return false, nil
		},
	}
	client := newTestRedisClient(t)
	o := &Orchestrator{locks: locks, records: newRecordStore(client), redis: client}

	prClient := &fakePRClient{files: map[string][]byte{"turnip.yaml": []byte(validTurnipYAML)}}
	event := &github.WebhookEvent{
		Repository:  github.Repository{Owner: "owner", Name: "repo"},
		PullRequest: &github.PullRequest{Number: 42, HeadSHA: "abc"},
	}
	require.NoError(t, o.handlePRClosed(context.Background(), prClient, event))

	assert.Empty(t, prClient.postedComments())
}

func TestHandlePRClosed_AlwaysDeletesPlanCommentRecord(t *testing.T) {
	locks := &fakeLockManager{isLockedByPRFunc: func(ctx context.Context, projectKey string, prNumber int) (bool, error) { return false, nil }}
	client := newTestRedisClient(t)
	o := &Orchestrator{locks: locks, records: newRecordStore(client), redis: client}
	require.NoError(t, o.records.SetPlanCommentRecord(context.Background(), "owner", "repo", 42, []string{"node-1"}))

	prClient := &fakePRClient{files: map[string][]byte{"turnip.yaml": []byte(validTurnipYAML)}}
	event := &github.WebhookEvent{
		Repository:  github.Repository{Owner: "owner", Name: "repo"},
		PullRequest: &github.PullRequest{Number: 42, HeadSHA: "abc"},
	}
	require.NoError(t, o.handlePRClosed(context.Background(), prClient, event))

	rec, err := o.records.GetPlanCommentRecord(context.Background(), "owner", "repo", 42)
	require.NoError(t, err)
	assert.Nil(t, rec)
}
