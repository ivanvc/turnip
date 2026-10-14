package orchestrator

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivanvc/turnip/internal/config"
	"github.com/ivanvc/turnip/internal/github"
)

// dispatchRecorder is a fake dispatch: it records each call and refuses
// the Projects named in refuse; the rest are left running.
type dispatchRecorder struct {
	mu     sync.Mutex
	calls  []string
	refuse map[string]bool
}

func (d *dispatchRecorder) dispatch(_ context.Context, _ github.GitHubClient, _ github.Repository, _ github.PullRequest, _ int64, _ string, command int, t Target) *github.ProjectResult {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls = append(d.calls, t.Operation+":"+t.Project.Name)
	if d.refuse[t.Project.Name] {
		return &github.ProjectResult{ProjectName: t.Project.Name, Tool: t.Project.Tool, Operation: t.Operation, Output: "refused"}
	}
	return nil
}

func carryTarget(name, op string) Target {
	return Target{Project: config.Project{Name: name, Directory: name, Tool: "helmfile"}, Operation: op, TriggeredBy: "alice"}
}

func carryOrchestrator(t *testing.T, d *dispatchRecorder) (*Orchestrator, *fakeCommentClient) {
	t.Helper()
	o, client := testCommentsOrchestrator(t, false)
	o.triggers = newTriggerStore(o.redis)
	o.installationClient = func(int64) github.GitHubClient { return client }
	o.dispatch = d.dispatch
	return o, client
}

func carryMeta(commands ...[]Target) triggerMeta {
	m := testTriggerMeta()
	m.Commands = commands
	return m
}

func TestBeginCommand_AllRefusedBeginsNextAtOnce(t *testing.T) {
	d := &dispatchRecorder{refuse: map[string]bool{"web": true, "api": true}}
	o, client := carryOrchestrator(t, d)
	ctx := context.Background()
	meta := carryMeta(
		[]Target{carryTarget("web", "diff"), carryTarget("api", "diff")},
		[]Target{carryTarget("db", "apply")},
	)
	require.NoError(t, o.triggers.Create(ctx, "t1", meta, nil))
	require.NoError(t, o.beginCommand(ctx, "t1", meta, 0))

	assert.ElementsMatch(t, []string{"diff:web", "diff:api", "apply:db"}, d.calls)
	assert.Equal(t, "apply:db", d.calls[len(d.calls)-1])
	assert.Empty(t, client.posted, "db is still running")

	st, err := o.triggers.Get(ctx, "t1")
	require.NoError(t, err)
	require.NotNil(t, st)
	assert.Equal(t, 1, st.Command)
	assert.Len(t, st.Results, 2)
}

func TestCompleteProject_LastOfLastPostsOnceAndDeletes(t *testing.T) {
	d := &dispatchRecorder{}
	o, client := carryOrchestrator(t, d)
	ctx := context.Background()
	meta := carryMeta(
		[]Target{carryTarget("web", "apply")},
		[]Target{carryTarget("web", "diff")},
	)
	// Order the apply command first so apply results are added before plan
	// results; the comment still splits plan before apply.
	meta.Notices = []string{"a notice"}
	require.NoError(t, o.triggers.Create(ctx, "t1", meta, nil))
	require.NoError(t, o.beginCommand(ctx, "t1", meta, 0))
	assert.Empty(t, client.posted)

	require.NoError(t, o.completeProject(ctx, "t1", 0, "web", github.ProjectResult{ProjectName: "web", Tool: "helmfile", Operation: "apply", Success: true, Output: "applied-out"}))
	assert.Empty(t, client.posted)
	assert.Equal(t, []string{"apply:web", "diff:web"}, d.calls)

	require.NoError(t, o.completeProject(ctx, "t1", 1, "web", github.ProjectResult{ProjectName: "web", Tool: "helmfile", Operation: "diff", Success: true, Output: "planned-out"}))
	require.Len(t, client.posted, 2)
	assert.Contains(t, client.posted[0], "planned-out")
	assert.Contains(t, client.posted[0], "a notice")
	assert.Contains(t, client.posted[1], "applied-out")
	assert.NotContains(t, client.posted[1], "a notice")

	st, err := o.triggers.Get(ctx, "t1")
	require.NoError(t, err)
	assert.Nil(t, st)
	n, err := o.redis.Exists(ctx, triggerKey("t1")).Result()
	require.NoError(t, err)
	assert.Zero(t, n)

	// A completion delivered again posts nothing more.
	require.NoError(t, o.completeProject(ctx, "t1", 1, "web", github.ProjectResult{ProjectName: "web"}))
	assert.Len(t, client.posted, 2)
}

func TestCompleteProject_NotLastPostsNothing(t *testing.T) {
	d := &dispatchRecorder{}
	o, client := carryOrchestrator(t, d)
	ctx := context.Background()
	meta := carryMeta([]Target{carryTarget("web", "diff"), carryTarget("api", "diff")})
	require.NoError(t, o.triggers.Create(ctx, "t1", meta, nil))
	require.NoError(t, o.beginCommand(ctx, "t1", meta, 0))

	require.NoError(t, o.completeProject(ctx, "t1", 0, "web", testProjectResult("web")))
	assert.Empty(t, client.posted)
	st, err := o.triggers.Get(ctx, "t1")
	require.NoError(t, err)
	assert.NotNil(t, st)
}

func TestCompleteProject_MissingOrEmptyTrigger(t *testing.T) {
	d := &dispatchRecorder{}
	o, client := carryOrchestrator(t, d)
	ctx := context.Background()

	require.NoError(t, o.completeProject(ctx, "gone", 0, "web", testProjectResult("web")))
	require.NoError(t, o.completeProject(ctx, "", 0, "web", testProjectResult("web")))
	assert.Empty(t, client.posted)
	assert.Empty(t, d.calls)
}
