package orchestrator

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivanvc/turnip/internal/config"
	"github.com/ivanvc/turnip/internal/github"
	pb "github.com/ivanvc/turnip/internal/grpc/turnip/v1"
	"github.com/ivanvc/turnip/internal/lock"
	"github.com/ivanvc/turnip/internal/rpc"
)

// ha_test.go validates Requirement 1.1-1.3 of the ha-validation slice
// (global Properties 34-36: a Server's Redis-backed state must behave
// identically no matter which instance touches it) against a REAL Redis,
// never miniredis — miniredis's single-threaded command loop serializes
// every operation, which could make these properties look true against
// the fake while a genuinely concurrent Redis exposes a race.

// realTestRedisAddr skips (not fails) when no real Redis is available,
// so a plain `go test ./...` still passes with no infrastructure handy.
func realTestRedisAddr(t *testing.T) string {
	t.Helper()
	addr := os.Getenv("TURNIP_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("TURNIP_TEST_REDIS_ADDR not set; skipping real-Redis HA test")
	}
	return addr
}

// haFakeClient is a github.GitHubClient shared across every simulated
// Server instance in a test — mirroring how real Server replicas all
// call the same real GitHub API. It covers both the pull_request
// plan-trigger path and the issue_comment apply-trigger path, and
// records posted comments per PR number so a test can inspect exactly
// one PR's outcome.
type haFakeClient struct {
	github.GitHubClient
	turnipYAML []byte
	pr         *github.PullRequest // returned by GetPullRequest for the apply path

	mu       sync.Mutex
	comments map[int][]string
}

func newHAFakeClient(turnipYAML []byte) *haFakeClient {
	return &haFakeClient{turnipYAML: turnipYAML, comments: make(map[int][]string)}
}

func (c *haFakeClient) GetFile(ctx context.Context, owner, repo, path, ref string) ([]byte, error) {
	if path != "turnip.yaml" {
		return nil, github.ErrFileNotFound
	}
	return c.turnipYAML, nil
}

func (c *haFakeClient) GetModifiedFiles(ctx context.Context, owner, repo string, prNumber int) ([]string, error) {
	return []string{"a/values.yaml"}, nil
}

func (c *haFakeClient) GetCollaboratorPermission(ctx context.Context, owner, repo, username string) (string, error) {
	return "write", nil
}

func (c *haFakeClient) GetPullRequest(ctx context.Context, owner, repo string, prNumber int) (*github.PullRequest, error) {
	return c.pr, nil
}

func (c *haFakeClient) PostComment(ctx context.Context, owner, repo string, prNumber int, body string) (*github.PostedComment, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.comments[prNumber] = append(c.comments[prNumber], body)
	return &github.PostedComment{ID: int64(len(c.comments[prNumber])), NodeID: fmt.Sprintf("node-%d-%d", prNumber, len(c.comments[prNumber]))}, nil
}

func (c *haFakeClient) CreateCheckRun(ctx context.Context, owner, repo string, opts github.CheckRunOptions) (int64, error) {
	return 1, nil
}

func (c *haFakeClient) UpdateCheckRun(ctx context.Context, owner, repo string, checkRunID int64, opts github.CheckRunOptions) error {
	return nil
}

func (c *haFakeClient) GenerateInstallationToken(ctx context.Context, _ github.TokenScope) (github.InstallationToken, error) {
	return github.InstallationToken{Token: "fake-token"}, nil
}

func (c *haFakeClient) commentsFor(prNumber int) []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.comments[prNumber]...)
}

const haTurnipYAML = `
schemaVersion: v1alpha3
projects:
  - name: helm-a
    directory: a
    uses: helmfile@v1.7.4
    whenModified:
      - "a/**"
`

// newHAOrchestrator builds one real *Orchestrator backed by a *redis.Client
// of its own dialing addr — a separate connection per instance, matching
// two independent Server processes sharing one real Redis backend rather
// than one shared *redis.Client object. It has no jobCreator wired yet;
// callers attach one (wireFakeJobCreator or wireGRPCJobCreator below)
// depending on whether the test needs a real HandleResult round-trip.
func newHAOrchestrator(t *testing.T, addr string, client github.GitHubClient) *Orchestrator {
	t.Helper()
	redisClient := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() { _ = redisClient.Close() })

	return &Orchestrator{
		locks:              lock.NewRedisLockManager(redisClient),
		plugins:            testRegistry(),
		records:            newRecordStore(redisClient),
		redis:              redisClient,
		installationClient: func(id int64) github.GitHubClient { return client },
		startTimeout:       5 * time.Minute,
		sweepInterval:      30 * time.Second,
	}
}

// wireFakeJobCreator attaches execute_test.go's fakeJobCreator, which
// publishes its canned result directly (bypassing HandleResult entirely)
// — fine when a test only cares about the immediate AcquireLock outcome
// and never needs StorePlan/ReleaseLock's side effects to have run.
func wireFakeJobCreator(t *testing.T, o *Orchestrator, result github.ProjectResult) {
	t.Helper()
	o.jobs = &fakeJobCreator{t: t, redis: o.redis, result: result}
}

// wireGRPCJobCreator attaches integration_test.go's grpcDrivingJobCreator,
// driving a real bufconn gRPC round-trip into this instance's own
// HandleLog/HandleResult — needed whenever a test's assertion depends on
// HandleResult's real side effects (StorePlan for a plan,
// ReleaseLock for an apply), not just the published ProjectResult.
func wireGRPCJobCreator(t *testing.T, o *Orchestrator, logLine string, result *pb.OperationResult) {
	t.Helper()
	client := newBufconnOperationClient(t, o)
	o.jobs = &grpcDrivingJobCreator{t: t, client: client, logLine: logLine, result: result}
}

func haPlanEvent(owner, repo string, prNumber int) *github.WebhookEvent {
	return &github.WebhookEvent{
		Action:     "opened",
		Repository: github.Repository{Owner: owner, Name: repo},
		// HeadRepo derived from the parameters rather than hardcoded:
		// these tests generate a fresh owner per run, so a literal
		// "owner" here would read as a fork and be refused.
		PullRequest:  &github.PullRequest{Number: prNumber, HeadSHA: "sha", HeadRepo: github.Repository{Owner: owner, Name: repo}},
		Installation: github.Installation{ID: 1},
	}
}

func haApplyEvent(owner, repo string, prNumber int, author string) *github.WebhookEvent {
	return &github.WebhookEvent{
		Action:       "created",
		Repository:   github.Repository{Owner: owner, Name: repo},
		PullRequest:  &github.PullRequest{Number: prNumber},
		Comment:      &github.Comment{Body: "/turnip apply helm-a", Author: author},
		Installation: github.Installation{ID: 1},
	}
}

// TestHA_PlanAndApplyAcrossInstancesMatchSingleInstance validates
// Requirement 1.1 (Properties 34, 35): a Plan on one instance followed by
// an Apply on a *different* instance must succeed exactly as it would if
// one instance had handled both — instance B has no in-process state from
// instance A, only whatever the real Redis backend holds (the lock and
// the plan record StorePlan wrote to it).
func TestHA_PlanAndApplyAcrossInstancesMatchSingleInstance(t *testing.T) {
	addr := realTestRedisAddr(t)

	runPlanThenApply := func(t *testing.T, planInstance, applyInstance *Orchestrator) bool {
		t.Helper()
		owner := "owner-" + uuid.NewString()[:8]
		repo := "repo"
		prNumber := 1
		client := newHAFakeClient([]byte(haTurnipYAML))
		client.pr = &github.PullRequest{Number: prNumber, HeadSHA: "sha", Open: true, HeadRepo: github.Repository{Owner: owner, Name: repo}}
		planInstance.installationClient = func(id int64) github.GitHubClient { return client }
		applyInstance.installationClient = func(id int64) github.GitHubClient { return client }

		wireGRPCJobCreator(t, planInstance, "helmfile diff: 1 to add", &pb.OperationResult{
			Success: true, Output: "1 to add", PlanData: []byte("plan-bytes"),
		})
		require.NoError(t, planInstance.HandlePullRequest(context.Background(), haPlanEvent(owner, repo, prNumber)))
		require.Eventually(t, func() bool { return len(client.commentsFor(prNumber)) >= 1 }, 5*time.Second, 20*time.Millisecond)

		wireGRPCJobCreator(t, applyInstance, "helmfile apply: release upgraded", &pb.OperationResult{
			Success: true, Output: "applied successfully",
		})
		require.NoError(t, applyInstance.HandleIssueComment(context.Background(), haApplyEvent(owner, repo, prNumber, "alice")))
		require.Eventually(t, func() bool { return len(client.commentsFor(prNumber)) >= 2 }, 5*time.Second, 20*time.Millisecond)

		applyComment := client.commentsFor(prNumber)[1]
		return strings.Contains(applyComment, "applied successfully")
	}

	single := newHAOrchestrator(t, addr, nil)
	baseline := runPlanThenApply(t, single, single)

	instanceA := newHAOrchestrator(t, addr, nil)
	instanceB := newHAOrchestrator(t, addr, nil)
	splitAcrossInstances := runPlanThenApply(t, instanceA, instanceB)

	assert.True(t, baseline, "plan-then-apply on a single instance should succeed")
	assert.Equal(t, baseline, splitAcrossInstances, "splitting the same flow across two instances must not change the outcome")
}

// TestHA_ConcurrentLockAcquisitionAcrossInstancesExactlyOneWinner
// validates Requirement 1.2 (Property 36): of N concurrent plan triggers
// for the same Project Key fired across multiple simulated Server
// instances, exactly one acquires the lock and every other one is
// rejected as already locked.
func TestHA_ConcurrentLockAcquisitionAcrossInstancesExactlyOneWinner(t *testing.T) {
	addr := realTestRedisAddr(t)

	const numEvents = 10
	owner := "owner-" + uuid.NewString()[:8]
	repo := "repo"
	client := newHAFakeClient([]byte(haTurnipYAML))

	instances := make([]*Orchestrator, 2)
	for i := range instances {
		instances[i] = newHAOrchestrator(t, addr, client)
		// The winner's own Operation is never observed by this test (see
		// below), so a fakeJobCreator publishing directly is sufficient —
		// no real gRPC round-trip needed here.
		wireFakeJobCreator(t, instances[i], github.ProjectResult{Success: true, Output: "no changes"})
	}

	var wg sync.WaitGroup
	for i := 1; i <= numEvents; i++ {
		wg.Add(1)
		go func(prNumber int) {
			defer wg.Done()
			inst := instances[prNumber%len(instances)]
			assert.NoError(t, inst.HandlePullRequest(context.Background(), haPlanEvent(owner, repo, prNumber)))
		}(i)
	}
	wg.Wait()

	require.Eventually(t, func() bool {
		posted := 0
		for i := 1; i <= numEvents; i++ {
			if len(client.commentsFor(i)) >= 1 {
				posted++
			}
		}
		// The winner's own Operation never resolves within this test's
		// window (nothing ever calls HandleResult for it — see
		// wireFakeJobCreator's doc comment), so only the numEvents-1
		// rejections are expected to post.
		return posted >= numEvents-1
	}, 5*time.Second, 20*time.Millisecond)

	rejectedCount := 0
	for i := 1; i <= numEvents; i++ {
		comments := client.commentsFor(i)
		if len(comments) == 0 {
			continue
		}
		if strings.Contains(comments[0], "locked by PR #") {
			rejectedCount++
		}
	}
	assert.Equal(t, numEvents-1, rejectedCount, "exactly one PR should have acquired the lock; every other one should be rejected as already locked")
}

// haOperation puts an Operation Record for project into the shared Redis,
// as the instance that dispatched it would have, so a different instance
// can finalize it.
func haOperation(t *testing.T, o *Orchestrator, owner, project, operation string) string {
	t.Helper()
	id := uuid.NewString()
	require.NoError(t, o.records.Create(context.Background(), &OperationRecord{
		OperationID:   id,
		ProjectKey:    owner + "/repo/" + project,
		Project:       config.Project{Name: project, Tool: "helmfile"},
		Owner:         owner,
		Repo:          "repo",
		PRNumber:      1,
		HeadSHA:       "sha",
		Operation:     operation,
		StartDeadline: time.Now().Add(5 * time.Minute).Unix(),
		CreatedAt:     time.Now(),
	}))
	return id
}

// TestHA_AggregateCheckAcrossInstancesCarriesEveryResult validates
// aggregate-check-run's checkpoint: two Projects of one pull request, each
// finalized on a different instance at the same moment, end with one
// `turnip` verdict covering both. Neither instance has any in-process
// knowledge of the other's result — only the shared record.
func TestHA_AggregateCheckAcrossInstancesCarriesEveryResult(t *testing.T) {
	addr := realTestRedisAddr(t)

	run := func(t *testing.T, webSucceeds bool) *github.CheckRunOptions {
		t.Helper()
		gh := newFakeChecksClient()
		a := newHAOrchestrator(t, addr, gh)
		b := newHAOrchestrator(t, addr, gh)
		owner := "owner-" + uuid.NewString()[:8]
		ref := prRef{Owner: owner, Repo: "repo", PRNumber: 1, HeadSHA: "sha"}

		// Both Projects planned, as the automatic plan would leave them.
		for _, project := range []string{"helm-a", "helm-b"} {
			require.NoError(t, a.records.WriteOutcome(context.Background(), ref, project, ProjectEntry{Outcome: OutcomeAwaitingApply, Operation: "diff"}))
		}
		require.Nil(t, gh.current(), "plans under review publish nothing")

		opA := haOperation(t, a, owner, "helm-a", "apply")
		opB := haOperation(t, a, owner, "helm-b", "apply")
		var wg sync.WaitGroup
		wg.Go(func() {
			assert.NoError(t, a.HandleResult(context.Background(), opA, rpc.OperationResult{Success: webSucceeds}))
		})
		wg.Go(func() {
			assert.NoError(t, b.HandleResult(context.Background(), opB, rpc.OperationResult{Success: true}))
		})
		wg.Wait()
		return gh.current()
	}

	for i := range 20 {
		t.Run(fmt.Sprintf("both-succeed-%d", i), func(t *testing.T) {
			cur := run(t, true)
			require.NotNil(t, cur)
			assert.Equal(t, "success", cur.Conclusion)
			assert.Equal(t, "2/2 projects up to date", cur.Title)
		})
		t.Run(fmt.Sprintf("one-fails-%d", i), func(t *testing.T) {
			cur := run(t, false)
			require.NotNil(t, cur)
			assert.Equal(t, "failure", cur.Conclusion)
		})
	}
}

// A narrower re-plan after a failed apply must not turn the verdict green:
// the record keeps the failure it never looked at.
func TestHA_NarrowerReplanDoesNotHideAFailure(t *testing.T) {
	addr := realTestRedisAddr(t)
	gh := newFakeChecksClient()
	a := newHAOrchestrator(t, addr, gh)
	b := newHAOrchestrator(t, addr, gh)
	owner := "owner-" + uuid.NewString()[:8]

	require.NoError(t, a.HandleResult(context.Background(), haOperation(t, a, owner, "helm-a", "apply"), rpc.OperationResult{Success: false}))
	require.Equal(t, "failure", gh.current().Conclusion)

	require.NoError(t, b.HandleResult(context.Background(), haOperation(t, b, owner, "helm-b", "diff"), rpc.OperationResult{Success: true}))
	assert.Equal(t, "failure", gh.current().Conclusion)
}
