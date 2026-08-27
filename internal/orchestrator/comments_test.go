package orchestrator

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivanvc/turnip/internal/github"
)

// fakeCommentClient is a minimal github.GitHubClient for comments.go's
// needs: PostComment and MinimizeComment.
type fakeCommentClient struct {
	github.GitHubClient
	posted         []string
	minimized      []string
	minimizeErr    error
	nextNodeIDSeq  int
	postCommentErr error
}

func (f *fakeCommentClient) PostComment(ctx context.Context, owner, repo string, prNumber int, body string) (*github.PostedComment, error) {
	if f.postCommentErr != nil {
		return nil, f.postCommentErr
	}
	f.posted = append(f.posted, body)
	f.nextNodeIDSeq++
	return &github.PostedComment{ID: int64(f.nextNodeIDSeq), NodeID: "node-" + string(rune('a'+f.nextNodeIDSeq-1))}, nil
}

func (f *fakeCommentClient) MinimizeComment(ctx context.Context, nodeID string) error {
	if f.minimizeErr != nil {
		return f.minimizeErr
	}
	f.minimized = append(f.minimized, nodeID)
	return nil
}

func testCommentsOrchestrator(t *testing.T, minimizeFlag bool) (*Orchestrator, *fakeCommentClient) {
	t.Helper()
	client := newTestRedisClient(t)
	o := &Orchestrator{
		plugins:                      testRegistry(),
		records:                      newRecordStore(client),
		redis:                        client,
		minimizeOutdatedPlanComments: minimizeFlag,
	}
	return o, &fakeCommentClient{}
}

func TestPostResults_PartitionsByKind(t *testing.T) {
	o, client := testCommentsOrchestrator(t, false)
	results := []github.ProjectResult{
		{ProjectName: "helm-a", Tool: "helmfile", Operation: "diff", Success: true, Output: "plan output"},
		{ProjectName: "helm-b", Tool: "helmfile", Operation: "apply", Success: true, Output: "apply output"},
	}

	o.postResults(context.Background(), client, testRepo, 42, results)

	require.Len(t, client.posted, 2)
	assert.Contains(t, client.posted[0], "plan output")
	assert.Contains(t, client.posted[1], "apply output")
}

func TestPostResults_EmptyGroupPostsNothing(t *testing.T) {
	o, client := testCommentsOrchestrator(t, false)
	results := []github.ProjectResult{
		{ProjectName: "helm-a", Tool: "helmfile", Operation: "diff", Success: true, Output: "plan output"},
	}

	o.postResults(context.Background(), client, testRepo, 42, results)

	require.Len(t, client.posted, 1)
	assert.Contains(t, client.posted[0], "plan output")
}

func TestPostResults_FlagDisabled_NeverMinimizesOrRecords(t *testing.T) {
	o, client := testCommentsOrchestrator(t, false)
	require.NoError(t, o.records.SetPlanCommentRecord(context.Background(), testRepo.Owner, testRepo.Name, 42, []string{"old-node"}))

	results := []github.ProjectResult{{ProjectName: "helm-a", Tool: "helmfile", Operation: "diff", Success: true}}
	o.postResults(context.Background(), client, testRepo, 42, results)

	assert.Empty(t, client.minimized)
	rec, err := o.records.GetPlanCommentRecord(context.Background(), testRepo.Owner, testRepo.Name, 42)
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Equal(t, []string{"old-node"}, rec.NodeIDs, "existing record must be left untouched when the flag is off")
}

func TestPostResults_FlagEnabled_ExistingRecord_MinimizesThenPostsFreshAndReplacesRecord(t *testing.T) {
	o, client := testCommentsOrchestrator(t, true)
	require.NoError(t, o.records.SetPlanCommentRecord(context.Background(), testRepo.Owner, testRepo.Name, 42, []string{"old-node-1", "old-node-2"}))

	results := []github.ProjectResult{{ProjectName: "helm-a", Tool: "helmfile", Operation: "diff", Success: true}}
	o.postResults(context.Background(), client, testRepo, 42, results)

	assert.ElementsMatch(t, []string{"old-node-1", "old-node-2"}, client.minimized)
	require.Len(t, client.posted, 1)

	rec, err := o.records.GetPlanCommentRecord(context.Background(), testRepo.Owner, testRepo.Name, 42)
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.NotEqual(t, []string{"old-node-1", "old-node-2"}, rec.NodeIDs)
}

func TestPostResults_FlagEnabled_NoRecord_SkipsMinimizeButStillPosts(t *testing.T) {
	o, client := testCommentsOrchestrator(t, true)

	results := []github.ProjectResult{{ProjectName: "helm-a", Tool: "helmfile", Operation: "diff", Success: true}}
	o.postResults(context.Background(), client, testRepo, 42, results)

	assert.Empty(t, client.minimized)
	require.Len(t, client.posted, 1)
}

func TestPostResults_ApplyGroup_NeverMinimizesOrRecords(t *testing.T) {
	o, client := testCommentsOrchestrator(t, true)

	results := []github.ProjectResult{{ProjectName: "helm-a", Tool: "helmfile", Operation: "apply", Success: true}}
	o.postResults(context.Background(), client, testRepo, 42, results)

	assert.Empty(t, client.minimized)
	rec, err := o.records.GetPlanCommentRecord(context.Background(), testRepo.Owner, testRepo.Name, 42)
	require.NoError(t, err)
	assert.Nil(t, rec)
}

func TestPostResults_MinimizeFailure_IsSoftAndPostingProceeds(t *testing.T) {
	o, client := testCommentsOrchestrator(t, true)
	client.minimizeErr = errors.New("minimize failed")
	require.NoError(t, o.records.SetPlanCommentRecord(context.Background(), testRepo.Owner, testRepo.Name, 42, []string{"old-node"}))

	results := []github.ProjectResult{{ProjectName: "helm-a", Tool: "helmfile", Operation: "diff", Success: true}}
	o.postResults(context.Background(), client, testRepo, 42, results)

	require.Len(t, client.posted, 1)
}
