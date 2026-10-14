package orchestrator

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivanvc/turnip/internal/config"
	"github.com/ivanvc/turnip/internal/github"
	"github.com/ivanvc/turnip/internal/lock"
	"github.com/ivanvc/turnip/internal/rpc"
)

// Task 5.1: a trigger across two replicas. Each replica is an Orchestrator
// of its own with its own GitHub client, sharing only Redis (the trigger
// store, the record store, the sweep's claims), the Lock manager (standing
// in for the Lock's Redis keys) and the Job creator (so Jobs can be
// counted). No fake Runner reports: the test delivers each result to the
// replica of its choosing, which is what a Runner connecting to whichever
// replica the Service picks does.

const twoReplicaYAML = `
schemaVersion: v1alpha3
projects:
  - name: web
    directory: web
    uses: helmfile@v1.7.4
    whenModified:
      - "web/**"
  - name: api
    directory: api
    uses: helmfile@v1.7.4
    whenModified:
      - "api/**"
`

func newReplica(t *testing.T, rc *redis.Client, locks lock.LockManager, jobsClient *fakeJobCreator, client github.GitHubClient) *Orchestrator {
	t.Helper()
	return &Orchestrator{
		locks:              locks,
		jobs:               jobsClient,
		plugins:            testRegistry(),
		catalog:            testCatalog(),
		records:            newRecordStore(rc),
		triggers:           newTriggerStore(rc),
		redis:              rc,
		installationClient: func(int64) github.GitHubClient { return client },
		startTimeout:       5 * time.Minute,
	}
}

func replicaClient() *fakeCommentEventClient {
	return &fakeCommentEventClient{
		permission: "write",
		files:      map[string][]byte{"turnip.yaml": []byte(twoReplicaYAML)},
		pr:         openPR(),
	}
}

// dispatchedOperations returns the project name to operation id of every
// Operation whose Job exists, according to Redis.
func dispatchedOperations(t *testing.T, o *Orchestrator) map[string]string {
	t.Helper()
	ctx := context.Background()
	keys, err := o.records.ScanOperationKeys(ctx)
	require.NoError(t, err)
	out := map[string]string{}
	for _, key := range keys {
		id := strings.TrimPrefix(key, "operation:")
		rec, err := o.records.get(ctx, id)
		require.NoError(t, err)
		if rec != nil && rec.JobName != "" {
			out[rec.Project.Name+":"+rec.Operation] = id
		}
	}
	return out
}

// awaitOperation waits until the Operation of project exists with its Job
// and returns its id.
func awaitOperation(t *testing.T, o *Orchestrator, project, operation string) string {
	t.Helper()
	var id string
	require.Eventually(t, func() bool {
		id = dispatchedOperations(t, o)[project+":"+operation]
		return id != ""
	}, 5*time.Second, time.Millisecond, "the %s of %s is dispatched", operation, project)
	return id
}

func awaitNoTrigger(t *testing.T, o *Orchestrator) {
	t.Helper()
	require.Eventually(t, func() bool { return len(triggerKeys(t, o)) == 0 },
		5*time.Second, time.Millisecond, "the trigger key is gone")
}

func deliver(t *testing.T, o *Orchestrator, operationID, output string) {
	t.Helper()
	require.NoError(t, o.HandleResult(context.Background(), operationID, rpc.OperationResult{Success: true, Output: output}))
}

// (a) The replica that received the comment is dropped after dispatching
// the diff. The other receives the diff's result, dispatches the apply,
// receives its result and posts one plan comment and one apply comment.
func TestTwoReplicas_ReceivingReplicaDroppedMidTrigger(t *testing.T) {
	rc := newTestRedisClient(t)
	locks := &fakeLockManager{}
	jobsClient := &fakeJobCreator{t: t}
	clientA, clientB := replicaClient(), replicaClient()
	a := newReplica(t, rc, locks, jobsClient, clientA)
	b := newReplica(t, rc, locks, jobsClient, clientB)

	require.NoError(t, a.HandleIssueComment(context.Background(), commentEvent("/turnip diff web\n/turnip apply web", "alice")))
	diffID := awaitOperation(t, a, "web", "diff")
	// Replica A is dropped here: nothing below uses it again.

	deliver(t, b, diffID, "diff output")
	applyID := awaitOperation(t, b, "web", "apply")
	assert.Empty(t, clientB.postedComments(), "nothing is posted before the last command finishes")
	deliver(t, b, applyID, "apply output")

	awaitNoTrigger(t, b)
	assert.Empty(t, clientA.postedComments())
	posted := clientB.postedComments()
	require.Len(t, posted, 2, "one plan comment and one apply comment, each once")
	assert.Contains(t, posted[0], "diff output")
	assert.Contains(t, posted[1], "apply output")
	assert.Equal(t, 2, jobsClient.createCount())
	assert.Empty(t, operationKeys(t, b))
}

// (b) Every replica replaced: the one that receives the only result of a
// trigger never dispatched anything, and posts the comment.
func TestTwoReplicas_ReplicaThatDispatchedNothingPostsTheComment(t *testing.T) {
	rc := newTestRedisClient(t)
	locks := &fakeLockManager{}
	jobsClient := &fakeJobCreator{t: t}
	clientA, clientB := replicaClient(), replicaClient()
	a := newReplica(t, rc, locks, jobsClient, clientA)
	b := newReplica(t, rc, locks, jobsClient, clientB)

	require.NoError(t, a.HandleIssueComment(context.Background(), commentEvent("/turnip diff web", "alice")))
	id := awaitOperation(t, a, "web", "diff")

	deliver(t, b, id, "only result")

	awaitNoTrigger(t, b)
	assert.Empty(t, clientA.postedComments())
	posted := clientB.postedComments()
	require.Len(t, posted, 1)
	assert.Contains(t, posted[0], "only result")
}

// (c) The last two Projects of a command are finalized on the two replicas
// at the same moment: the next command is begun once, every time.
func TestTwoReplicas_ConcurrentLastCompletionsBeginNextCommandOnce(t *testing.T) {
	rc := newTestRedisClient(t)
	locks := &fakeLockManager{}
	jobsClient := &fakeJobCreator{t: t}
	clientA, clientB := replicaClient(), replicaClient()
	a := newReplica(t, rc, locks, jobsClient, clientA)
	b := newReplica(t, rc, locks, jobsClient, clientB)
	ctx := context.Background()

	target := func(name, op string) Target {
		return Target{Project: config.Project{Name: name, Directory: name, Tool: "helmfile"}, Operation: op, TriggeredBy: "alice"}
	}
	for i := range 100 {
		web, api, db := fmt.Sprintf("web-%d", i), fmt.Sprintf("api-%d", i), fmt.Sprintf("db-%d", i)
		meta := testTriggerMeta()
		meta.PRNumber = 1000 + i
		meta.Notices = nil
		meta.Commands = [][]Target{{target(web, "diff"), target(api, "diff")}, {target(db, "diff")}}
		triggerID := fmt.Sprintf("race-%d", i)
		require.NoError(t, a.triggers.Create(ctx, triggerID, meta, nil))
		require.NoError(t, a.beginCommand(ctx, triggerID, meta, 0))
		require.Equal(t, 3*i+2, jobsClient.createCount(), "setup: the first command's two Jobs")

		webID := awaitOperation(t, a, web, "diff")
		apiID := awaitOperation(t, a, api, "diff")

		var wg sync.WaitGroup
		start := make(chan struct{})
		for o, id := range map[*Orchestrator]string{a: webID, b: apiID} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				assert.NoError(t, o.HandleResult(ctx, id, rpc.OperationResult{Success: true, Output: "out"}))
			}()
		}
		close(start)
		wg.Wait()

		assert.Equal(t, 3*(i+1), jobsClient.createCount(), "the next command is begun exactly once (iteration %d)", i)
		dbID := awaitOperation(t, a, db, "diff")
		deliver(t, b, dbID, "out")
		awaitNoTrigger(t, a)
	}
	assert.Equal(t, 100, len(clientA.postedComments())+len(clientB.postedComments()), "one comment per trigger")
}

// (d) A first command refused whole at its Locks completes at once from
// the dispatching replica, and the second is dispatched without waiting.
func TestTwoReplicas_FirstCommandRefusedAtItsLocksBeginsTheSecond(t *testing.T) {
	rc := newTestRedisClient(t)
	locks := &fakeLockManager{acquireForPlanFunc: func(_ context.Context, projectKey string, _ int, _, _ string) (bool, lock.Transition, error) {
		if strings.HasSuffix(projectKey, "/web") {
			return false, lock.Transition{}, nil
		}
		return true, lock.Transition{To: lock.StatePlanning}, nil
	}}
	jobsClient := &fakeJobCreator{t: t}
	clientA, clientB := replicaClient(), replicaClient()
	a := newReplica(t, rc, locks, jobsClient, clientA)
	b := newReplica(t, rc, locks, jobsClient, clientB)

	require.NoError(t, a.HandleIssueComment(context.Background(), commentEvent("/turnip diff web\n/turnip diff api", "alice")))
	id := awaitOperation(t, a, "api", "diff")
	assert.Equal(t, 1, jobsClient.createCount(), "only the second command has a Job")

	deliver(t, b, id, "api output")

	awaitNoTrigger(t, b)
	posted := clientB.postedComments()
	require.Len(t, posted, 1)
	assert.Contains(t, posted[0], "api output")
	assert.Contains(t, posted[0], "web")
	assert.Empty(t, clientA.postedComments())
}
