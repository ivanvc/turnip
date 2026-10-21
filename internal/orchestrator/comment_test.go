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
	modified   []string

	// collaborator overrides what IsCollaborator answers. Nil falls back
	// to whether a permission was configured, which is what every fixture
	// written before the gate asked this question meant by it — so those
	// fixtures need no change.
	//
	// It is settable because the two questions are independent: GitHub can
	// report an access level for an account that is not a collaborator,
	// and that combination is the one the gate used to get wrong.
	collaborator *bool

	// collaboratorErr and permissionErr make the matching lookup fail,
	// which is GitHub being unable to answer, as distinct from an answer
	// of "not a collaborator".
	collaboratorErr error
	permissionErr   error

	mu            sync.Mutex
	posted        []string
	modifiedCalls int
	fileCalls     []string

	// collaboratorCalls and permissionCalls count the questions asked of
	// GitHub, so a test can say how many each comment cost.
	collaboratorCalls int
	permissionCalls   int
}

func (f *fakeCommentEventClient) questionCounts() (collaborator, permission int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.collaboratorCalls, f.permissionCalls
}

// getFileCallLog is how a test witnesses that a refusal ran before the
// configuration was read: fetchConfig is the first thing past the guard,
// so an empty log is positive evidence rather than an inference.
func (f *fakeCommentEventClient) getFileCallLog() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.fileCalls...)
}

func (f *fakeCommentEventClient) postedComments() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.posted...)
}

// GetModifiedFiles counts its calls: a bare plan's file listing is
// memoized per event, and the count is how that is asserted rather than
// inferred.
func (f *fakeCommentEventClient) GetModifiedFiles(ctx context.Context, owner, repo string, prNumber int) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.modifiedCalls++
	return f.modified, nil
}

func (f *fakeCommentEventClient) modifiedFetches() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.modifiedCalls
}

func (f *fakeCommentEventClient) GetCollaboratorPermission(ctx context.Context, owner, repo, username string) (string, error) {
	f.mu.Lock()
	f.permissionCalls++
	f.mu.Unlock()
	if f.permissionErr != nil {
		return "", f.permissionErr
	}
	// An empty permission simulates an account with no access: GitHub
	// answers "none" for it, not an error.
	if f.permission == "" {
		return "none", nil
	}
	return f.permission, nil
}
func (f *fakeCommentEventClient) IsCollaborator(ctx context.Context, owner, repo, username string) (bool, error) {
	f.mu.Lock()
	f.collaboratorCalls++
	f.mu.Unlock()
	if f.collaboratorErr != nil {
		return false, f.collaboratorErr
	}
	if f.collaborator != nil {
		return *f.collaborator, nil
	}
	return f.permission != "", nil
}
func (f *fakeCommentEventClient) GetPullRequest(ctx context.Context, owner, repo string, prNumber int) (*github.PullRequest, error) {
	return f.pr, nil
}
func (f *fakeCommentEventClient) GetFile(ctx context.Context, owner, repo, path, ref string) ([]byte, error) {
	f.mu.Lock()
	f.fileCalls = append(f.fileCalls, path)
	f.mu.Unlock()
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
func (f *fakeCommentEventClient) GenerateInstallationToken(ctx context.Context, _ github.TokenScope) (github.InstallationToken, error) {
	return github.InstallationToken{Token: "token"}, nil
}

func testCommentOrchestrator(t *testing.T, locks lock.LockManager) *Orchestrator {
	t.Helper()
	client := newTestRedisClient(t)
	return bindFakeRunner(&Orchestrator{
		locks:    locks,
		jobs:     &fakeJobCreator{t: t, result: github.ProjectResult{Success: true}},
		plugins:  testRegistry(),
		catalog:  testCatalog(),
		records:  newRecordStore(client),
		triggers: newTriggerStore(client),
		redis:    client,
		// Replaced by callHandleIssueComment: the trigger's dispatch and
		// its finalization both build their client from the installation.
		installationClient: func(id int64) github.GitHubClient { return nil },
	})
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

// The flooding case, end to end: a repository being onboarded has no
// turnip.yaml yet, and someone comments a slash command meant for another
// bot. turnip must post nothing at all — not a config error, not a
// permission error, not a "that looked malformed" note. Reviewing a PR is
// impossible if every such comment draws a reply.
func TestHandleIssueComment_OtherBotsCommandPostsNothingWithoutConfig(t *testing.T) {
	o := testCommentOrchestrator(t, &fakeLockManager{})

	for _, body := range []string{
		"/jira create ATO-1",
		"/lgtm",
		"/cc @teammate",
		"looks good to me",
	} {
		client := &fakeCommentEventClient{
			permission: "write",
			files:      map[string][]byte{}, // no turnip.yaml anywhere
			pr:         &github.PullRequest{Number: 42, HeadSHA: "abc", Open: true, HeadRepo: github.Repository{Owner: "owner", Name: "repo"}},
		}

		require.NoError(t, callHandleIssueComment(o, client, commentEvent(body, "alice")))
		assert.Emptyf(t, client.postedComments(), "comment %q is not addressed to turnip", body)
	}
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
		pr:         &github.PullRequest{Number: 42, HeadSHA: "abc", Open: true, HeadRepo: github.Repository{Owner: "owner", Name: "repo"}},
	}

	require.NoError(t, callHandleIssueComment(o, client, commentEvent("/turnip diff", "alice")))

	require.Len(t, client.postedComments(), 1)
	// Asserted on the path rather than the phrasing: where to put the file
	// is the part of this message that has to be right, and it survives a
	// rewording that a substring like "not found" does not.
	assert.Contains(t, client.postedComments()[0], ".turnip/config.yaml")
}

// A Trigger Command on a pull request from another repository is refused
// whatever the commenter's permission level. The collaborator check
// authorizes the *trigger*; it says nothing about the *code*, so a
// trusted colleague commenting on a fork would otherwise run a stranger's
// changes.
func TestHandleIssueComment_ForeignPullRequestIsRefused(t *testing.T) {
	for _, tc := range []struct {
		name string
		head github.Repository
	}{
		{"fork under another owner", github.Repository{Owner: "contributor", Name: "repo"}},
		{"head repository deleted", github.Repository{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var acquired int
			locks := &fakeLockManager{
				acquireLockFunc: func(ctx context.Context, projectKey string, prNumber int, url, lockedBy string) (bool, error) {
					acquired++
					return true, nil
				},
			}
			o := testCommentOrchestrator(t, locks)
			client := &fakeCommentEventClient{
				permission: "write", // a trusted collaborator
				files:      map[string][]byte{"turnip.yaml": []byte(validTurnipYAML)},
				pr:         &github.PullRequest{Number: 42, HeadSHA: "abc", Open: true, HeadRepo: tc.head},
			}

			err := callHandleIssueComment(o, client, commentEvent("/turnip diff", "alice"))

			require.ErrorIs(t, err, github.ErrRefused)
			assert.Zero(t, acquired, "no Lock is acquired")
			assert.Empty(t, client.postedComments(),
				"the refusal is silent on the pull request")
		})
	}
}

// An ordinary same-repository pull request is unaffected — the assertion
// that would fail if HeadRepo were ever left unmapped, since IsForeign
// fails closed and would refuse everything.
func TestHandleIssueComment_SameRepositoryPullRequestStillRuns(t *testing.T) {
	o := testCommentOrchestrator(t, &fakeLockManager{})
	client := &fakeCommentEventClient{
		permission: "write",
		files:      map[string][]byte{"turnip.yaml": []byte(validTurnipYAML)},
		pr:         &github.PullRequest{Number: 42, HeadSHA: "abc", Open: true, HeadRepo: github.Repository{Owner: "owner", Name: "repo"}},
	}

	require.NoError(t, callHandleIssueComment(o, client, commentEvent("/turnip diff helm-a", "alice")))
}

func TestHandleIssueComment_NonCollaboratorRejectedBeforeAnyCommand(t *testing.T) {
	o := testCommentOrchestrator(t, &fakeLockManager{})
	client := &fakeCommentEventClient{permission: ""}
	require.NoError(t, callHandleIssueComment(o, client, commentEvent("/turnip plan", "mallory")))

	require.Equal(t, []string{"@mallory does not have permission to trigger operations on this repository."}, client.posted)
}

func TestHandleIssueComment_MalformedLineRepliedAlongsideWellFormedProcessed(t *testing.T) {
	locks := &fakeLockManager{}
	o := testCommentOrchestrator(t, locks)
	client := &fakeCommentEventClient{
		permission: "write",
		files:      map[string][]byte{"turnip.yaml": []byte(validTurnipYAML)},
		pr:         &github.PullRequest{Number: 42, HeadSHA: "abc", Open: true, HeadRepo: github.Repository{Owner: "owner", Name: "repo"}},
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
		pr:         &github.PullRequest{Number: 42, HeadSHA: "abc", Open: true, HeadRepo: github.Repository{Owner: "owner", Name: "repo"}},
	}

	require.NoError(t, callHandleIssueComment(o, client, commentEvent("/turnip diff helm-a\n/turnip apply helm-a", "alice")))

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(order) >= 2
	}, 2*time.Second, 10*time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	// The diff's Lock is acquired before anything asks whether the pull
	// request holds it: finalizing the diff does, and then the apply's
	// admission, which begins only once the diff's result is in.
	assert.Equal(t, []string{"acquire", "islocked"}, order[:2])
}

func TestHandleIssueComment_UnlockNeverCreatesJobOrRecord(t *testing.T) {
	locks := &fakeLockManager{listLocksFunc: listHeld(map[string]int{"helm-a": 42})}
	o := testCommentOrchestrator(t, locks)
	client := &fakeCommentEventClient{
		permission: "write",
		files:      map[string][]byte{"turnip.yaml": []byte(validTurnipYAML)},
		pr:         &github.PullRequest{Number: 42, HeadSHA: "abc", Open: true, HeadRepo: github.Repository{Owner: "owner", Name: "repo"}},
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
// Three Projects, so "one reply rather than one refusal per Project" is
// demonstrated rather than merely asserted on a set of one.
const multiProjectTurnipYAML = `
schemaVersion: v1alpha3
projects:
  - name: helm-a
    directory: a
    uses: helmfile@v1.7.4
    whenModified:
      - "a/**"
  - name: helm-b
    directory: b
    uses: helmfile@v1.7.4
    whenModified:
      - "b/**"
  - name: helm-c
    directory: c
    uses: helmfile@v1.7.4
    whenModified:
      - "c/**"
`

// The memoized seam, end to end: three bare plans in one comment ask the
// same unchanging question, and must cost one file listing rather than
// three.
func TestHandleIssueComment_BarePlansFetchModifiedFilesOnce(t *testing.T) {
	o := testCommentOrchestrator(t, &fakeLockManager{})
	client := &fakeCommentEventClient{
		permission: "write",
		files:      map[string][]byte{"turnip.yaml": []byte(multiProjectTurnipYAML)},
		pr:         &github.PullRequest{Number: 42, HeadSHA: "abc", Open: true, HeadRepo: github.Repository{Owner: "owner", Name: "repo"}},
		modified:   []string{"a/main.tf"},
	}

	require.NoError(t, callHandleIssueComment(o, client,
		commentEvent("/turnip diff\n/turnip diff\n/turnip diff", "alice")))

	assert.Equal(t, 1, client.modifiedFetches(), "the listing is fetched once per event, not once per command")
}

// A trigger that names its Projects never consults the Modified_Set, so it
// must not pay for the call at all.
func TestHandleIssueComment_NamedProjectsFetchNoModifiedFiles(t *testing.T) {
	o := testCommentOrchestrator(t, &fakeLockManager{})
	client := &fakeCommentEventClient{
		permission: "write",
		files:      map[string][]byte{"turnip.yaml": []byte(multiProjectTurnipYAML)},
		pr:         &github.PullRequest{Number: 42, HeadSHA: "abc", Open: true, HeadRepo: github.Repository{Owner: "owner", Name: "repo"}},
		modified:   []string{"a/main.tf"},
	}

	require.NoError(t, callHandleIssueComment(o, client, commentEvent("/turnip diff helm-a", "alice")))

	assert.Equal(t, 0, client.modifiedFetches())
}

// The regression this slice exists to prevent, counted rather than read:
// a bare apply against three configured Projects holding no plan produces
// one reply, where it used to produce one refusal per Project.
func TestHandleIssueComment_BareApplyWithNoPlansRepliesOnce(t *testing.T) {
	// fakeLockManager's default GetLockStatus reports a Lock held by PR 99
	// with no plan recorded, so this pull request holds nothing to apply.
	o := testCommentOrchestrator(t, &fakeLockManager{})
	client := &fakeCommentEventClient{
		permission: "write",
		files:      map[string][]byte{"turnip.yaml": []byte(multiProjectTurnipYAML)},
		pr:         &github.PullRequest{Number: 42, HeadSHA: "abc", Open: true, HeadRepo: github.Repository{Owner: "owner", Name: "repo"}},
	}

	require.NoError(t, callHandleIssueComment(o, client, commentEvent("/turnip apply", "alice")))

	posted := client.postedComments()
	require.Len(t, posted, 1, "one answer for the command, not one refusal per configured project")
	assert.Contains(t, posted[0], "No project has a plan from this pull request")
}

func callHandleIssueComment(o *Orchestrator, client github.GitHubClient, event *github.WebhookEvent) error {
	o.installationClient = func(id int64) github.GitHubClient { return client }
	return o.HandleIssueComment(context.Background(), event)
}

// The same defect, at the level that matters: a helper returning false
// proves nothing if the caller ignores it.
//
// The fixture is a *successful* permission lookup describing access,
// alongside GitHub saying the account is not a collaborator. That is the
// combination the gate used to read as authorization.
func TestHandleIssueComment_SuccessfulPermissionLookupIsNotAuthorization(t *testing.T) {
	notCollaborator := false
	o := testCommentOrchestrator(t, &fakeLockManager{})
	client := &fakeCommentEventClient{
		permission:   "read",
		collaborator: &notCollaborator,
		files:        map[string][]byte{"turnip.yaml": []byte(validTurnipYAML)},
		pr:           openPR(),
	}

	require.NoError(t, callHandleIssueComment(o, client, commentEvent("/turnip diff", "mallory")))

	require.Equal(t, []string{"@mallory does not have permission to trigger operations on this repository."}, client.postedComments())
	assert.Empty(t, client.getFileCallLog(),
		"a refused trigger must not read the repository's configuration")
}

const couldNotCheck = "@alice: turnip could not check your permission on this repository, so nothing was run. Comment again to retry."

func TestHandleIssueComment_CollaboratorLookupErrorIsNotARefusal(t *testing.T) {
	o := testCommentOrchestrator(t, &fakeLockManager{})
	client := &fakeCommentEventClient{
		permission:      "write",
		collaboratorErr: errors.New("rate limited"),
		files:           map[string][]byte{"turnip.yaml": []byte(validTurnipYAML)},
		pr:              openPR(),
	}

	require.NoError(t, callHandleIssueComment(o, client, commentEvent("/turnip diff", "alice")))

	require.Equal(t, []string{couldNotCheck}, client.postedComments())
	assert.Empty(t, client.getFileCallLog())
}

func TestHandleIssueComment_PermissionLookupErrorForUnlistedAuthorIsNotARefusal(t *testing.T) {
	notCollaborator := false
	o := testCommentOrchestrator(t, &fakeLockManager{})
	client := &fakeCommentEventClient{
		collaborator:  &notCollaborator,
		permissionErr: errors.New("unreachable"),
		files:         map[string][]byte{"turnip.yaml": []byte(validTurnipYAML)},
		pr:            openPR(),
	}

	require.NoError(t, callHandleIssueComment(o, client, commentEvent("/turnip diff", "alice")))

	require.Equal(t, []string{couldNotCheck}, client.postedComments())
	assert.Empty(t, client.getFileCallLog())
}

func TestHandleIssueComment_PermissionLookupErrorForApplyIsNotARefusal(t *testing.T) {
	listed := true
	o := testCommentOrchestrator(t, &fakeLockManager{})
	jobsClient, ok := o.jobs.(*fakeJobCreator)
	require.True(t, ok)
	// The collaborator answer lists the author, so the gate asks nothing
	// else and only the apply's write check meets the error.
	client := &fakeCommentEventClient{
		collaborator:  &listed,
		permissionErr: errors.New("unreachable"),
		files:         map[string][]byte{"turnip.yaml": []byte(validTurnipYAML)},
		pr:            openPR(),
	}

	require.NoError(t, callHandleIssueComment(o, client, commentEvent("/turnip apply helm-a", "alice")))

	posted := client.postedComments()
	require.Len(t, posted, 1)
	assert.Contains(t, posted[0], `could not check write permission to run "apply"; comment again to retry`)
	assert.NotContains(t, posted[0], "write permission is required")
	assert.Zero(t, jobsClient.createCount())
}

func TestHandleIssueComment_PermissionLookupErrorForUnlockIsNotARefusal(t *testing.T) {
	listed := true
	o := testCommentOrchestrator(t, &fakeLockManager{})
	client := &fakeCommentEventClient{
		collaborator:  &listed,
		permissionErr: errors.New("unreachable"),
		files:         map[string][]byte{"turnip.yaml": []byte(validTurnipYAML)},
		pr:            openPR(),
	}

	require.NoError(t, callHandleIssueComment(o, client, commentEvent("/turnip unlock", "alice")))

	require.Equal(t, []string{"@alice: turnip could not check your write permission, so nothing was unlocked. Comment again to retry."}, client.postedComments())
}

func TestHandleIssueComment_UnlockRefusalTextUnchanged(t *testing.T) {
	o := testCommentOrchestrator(t, &fakeLockManager{})
	client := &fakeCommentEventClient{
		permission: "read",
		files:      map[string][]byte{"turnip.yaml": []byte(validTurnipYAML)},
		pr:         openPR(),
	}

	require.NoError(t, callHandleIssueComment(o, client, commentEvent("/turnip unlock", "alice")))

	require.Equal(t, []string{"@alice does not have write permission required to unlock."}, client.postedComments())
}
