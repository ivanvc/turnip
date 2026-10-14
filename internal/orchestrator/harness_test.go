package orchestrator

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/ivanvc/turnip/internal/github"
)

func newTestRedisClient(t *testing.T) *redis.Client {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr(), MaxRetries: -1})
	t.Cleanup(func() { _ = client.Close() })
	return client
}

// bindFakeRunner points o's fakeJobCreator at o, so the fake Runner of
// every Job it creates reports back through o.HandleResult.
func bindFakeRunner(o *Orchestrator) *Orchestrator {
	if fake, ok := o.jobs.(interface{ reportTo(*Orchestrator) }); ok {
		fake.reportTo(o)
	}
	return o
}

// heldProject is a pending field that no Project ever completes. A trigger
// holding it never finishes its command, so nothing is carried forward and
// nothing is posted: the results the finalizers and the dispatcher added
// stay in the trigger for a test to read.
const heldProject = "\x00held"

// holdTrigger creates a trigger and holds its first command open.
func holdTrigger(t require.TestingT, o *Orchestrator, triggerID string, meta triggerMeta) {
	ctx := context.Background()
	require.NoError(t, o.triggers.Create(ctx, triggerID, meta, nil))
	require.NoError(t, o.triggers.BeginCommand(ctx, triggerID, 0, []string{heldProject}))
}

// heldResults reads back the results added to a held trigger so far.
func heldResults(t require.TestingT, o *Orchestrator, triggerID string) []github.ProjectResult {
	st, err := o.triggers.Get(context.Background(), triggerID)
	require.NoError(t, err)
	require.NotNil(t, st, "the held trigger must still exist")
	return st.Results
}

// runCommand dispatches targets as one command of a trigger, the way a
// replica begins a command, and returns every Project's result in the
// order added: a refusal completed by the dispatcher, or what finalization
// added once the fake Runner (fakeJobCreator) reported through
// HandleResult. The trigger is held open, so no comment is posted.
//
// client becomes the installation's client, because both the dispatch and
// the finalization that follows build theirs from the installation id.
func (o *Orchestrator) runCommand(t require.TestingT, ctx context.Context, client github.GitHubClient, repo github.Repository, pr github.PullRequest, installationID int64, targets []Target) []github.ProjectResult {
	o.installationClient = func(int64) github.GitHubClient { return client }
	if o.triggers == nil {
		o.triggers = newTriggerStore(o.redis)
	}
	meta := triggerMeta{
		Repo:           repo,
		InstallationID: installationID,
		PRNumber:       pr.Number,
		HeadSHA:        pr.HeadSHA,
		BaseRef:        pr.BaseRef,
		HeadRef:        pr.HeadRef,
		Commands:       [][]Target{targets},
	}
	triggerID := uuid.NewString()
	holdTrigger(t, o, triggerID, meta)
	require.NoError(t, o.beginCommand(ctx, triggerID, meta, 0))

	var results []github.ProjectResult
	require.Eventually(t, func() bool {
		results = heldResults(t, o, triggerID)
		return len(results) == len(targets)
	}, 10*time.Second, time.Millisecond, "every Project of the command completes")
	// The fake Runners' goroutines have returned before the test reads
	// what they wrote to its fakes.
	if fake, ok := o.jobs.(interface{ waitForReports() }); ok {
		fake.waitForReports()
	}
	require.NoError(t, o.triggers.Delete(context.Background(), triggerID))
	return results
}

// dispatchOnly dispatches target and stops there: no fake Runner reports,
// so the Operation Record stays for a test to read. It returns
// executeOne's refusal, nil when the Job was created. The Operation names
// no trigger.
func (o *Orchestrator) dispatchOnly(ctx context.Context, client github.GitHubClient, repo github.Repository, pr github.PullRequest, target Target) *github.ProjectResult {
	if fake, ok := o.jobs.(interface{ reportTo(*Orchestrator) }); ok {
		fake.reportTo(nil)
	}
	return o.executeOne(ctx, client, repo, pr, 1, "", 0, target)
}

// runOne is runCommand for a single Target.
func (o *Orchestrator) runOne(t require.TestingT, ctx context.Context, client github.GitHubClient, repo github.Repository, pr github.PullRequest, installationID int64, target Target) github.ProjectResult {
	return o.runCommand(t, ctx, client, repo, pr, installationID, []Target{target})[0]
}

// finalizedResult returns the result that finalizing operationID adds to
// its trigger, which is what reaches the pull request comment. The
// Operation Record already exists; it is pointed at a held trigger, then
// finalize runs (HandleResult, or a sweep).
func finalizedResult(t require.TestingT, o *Orchestrator, operationID string, finalize func()) github.ProjectResult {
	ctx := context.Background()
	triggerID := "held-" + operationID
	holdTrigger(t, o, triggerID, triggerMeta{})
	require.NoError(t, o.records.mutate(ctx, operationID, func(rec *OperationRecord) {
		rec.TriggerID = triggerID
		rec.CommandIndex = 0
	}))
	rec, err := o.records.get(ctx, operationID)
	require.NoError(t, err)
	require.NoError(t, o.triggers.BeginCommand(ctx, triggerID, 0, []string{rec.Project.Name}))

	finalize()

	results := heldResults(t, o, triggerID)
	require.Len(t, results, 1, "finalizing adds the Project's result to its trigger, in the same call")
	return results[0]
}
