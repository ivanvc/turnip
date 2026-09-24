package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/ivanvc/turnip/internal/github"
)

// fakeRun is one check run as GitHub holds it.
type fakeRun struct {
	opts github.CheckRunOptions
	// updated orders every create and update across all runs, so a test can
	// ask which run branch protection would read: the most recently
	// updated of the name.
	updated int
}

// fakeChecksClient models the part of GitHub the publisher depends on.
// It refuses to move a completed run back to in progress, which GitHub
// does not document as possible, so the publisher's fallback is exercised
// rather than assumed.
type fakeChecksClient struct {
	github.GitHubClient

	mu    sync.Mutex
	runs  map[int64]*fakeRun
	seq   int
	delay func() // runs inside every call, before it takes effect
	// during runs once, inside the first call, to simulate another
	// instance's write landing while this one talks to GitHub.
	during    func()
	createErr error
}

func newFakeChecksClient() *fakeChecksClient {
	return &fakeChecksClient{runs: map[int64]*fakeRun{}}
}

func (f *fakeChecksClient) hooks() {
	if f.delay != nil {
		f.delay()
	}
	f.mu.Lock()
	during := f.during
	f.during = nil
	f.mu.Unlock()
	if during != nil {
		during()
	}
}

func (f *fakeChecksClient) CreateCheckRun(_ context.Context, _, _ string, opts github.CheckRunOptions) (int64, error) {
	f.hooks()
	if f.createErr != nil {
		return 0, f.createErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seq++
	id := int64(len(f.runs) + 1)
	f.runs[id] = &fakeRun{opts: opts, updated: f.seq}
	return id, nil
}

func (f *fakeChecksClient) UpdateCheckRun(_ context.Context, _, _ string, id int64, opts github.CheckRunOptions) error {
	f.hooks()
	f.mu.Lock()
	defer f.mu.Unlock()
	run, ok := f.runs[id]
	if !ok {
		return errors.New("404 not found")
	}
	if run.opts.Status == "completed" && opts.Status != "completed" {
		return errors.New("422 a completed check run cannot be reopened")
	}
	f.seq++
	opts.HeadSHA = run.opts.HeadSHA
	run.opts = opts
	run.updated = f.seq
	return nil
}

// current is the run of the aggregate's name that GitHub evaluates, or
// nil when none exists.
func (f *fakeChecksClient) current() *github.CheckRunOptions {
	f.mu.Lock()
	defer f.mu.Unlock()
	var latest *fakeRun
	for _, run := range f.runs {
		if run.opts.Name == aggregateCheckName && (latest == nil || run.updated > latest.updated) {
			latest = run
		}
	}
	if latest == nil {
		return nil
	}
	opts := latest.opts
	return &opts
}

func (f *fakeChecksClient) runCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.runs)
}

func testAggregateOrchestrator(t *testing.T) *Orchestrator {
	t.Helper()
	client := newTestRedisClient(t)
	return &Orchestrator{plugins: testRegistry(), records: newRecordStore(client), redis: client}
}

func record(t *testing.T, o *Orchestrator, client github.GitHubClient, project string, outcome Outcome) {
	t.Helper()
	require.NoError(t, o.recordOutcome(context.Background(), client, testRef, project, ProjectEntry{Outcome: outcome, Operation: "diff"}))
}

func TestPublishAggregate_AbsentUntilTheFirstApply(t *testing.T) {
	o := testAggregateOrchestrator(t)
	gh := newFakeChecksClient()

	record(t, o, gh, "web", OutcomeAwaitingApply)
	record(t, o, gh, "api", OutcomeAwaitingApply)
	assert.Nil(t, gh.current(), "a pull request under review shows GitHub's Expected, not a check")

	record(t, o, gh, "web", OutcomeApplied)
	cur := gh.current()
	require.NotNil(t, cur)
	assert.Equal(t, "in_progress", cur.Status)
	assert.Equal(t, "1/2 projects up to date", cur.Title)
	assert.Equal(t, testRef.HeadSHA, cur.HeadSHA)

	record(t, o, gh, "api", OutcomeApplied)
	cur = gh.current()
	assert.Equal(t, "success", cur.Conclusion)
	assert.Equal(t, 1, gh.runCount(), "kept current by updating in place")
}

// The walkthrough in the design: two Projects, applied on two instances.
// The second instance knows nothing about the first beyond what the first
// wrote to Redis, and still publishes the verdict of both.
func TestPublishAggregate_TwoInstancesOneVerdict(t *testing.T) {
	a := testAggregateOrchestrator(t)
	b := &Orchestrator{plugins: a.plugins, records: newRecordStore(a.redis), redis: a.redis}
	gh := newFakeChecksClient()

	record(t, a, gh, "web", OutcomeAwaitingApply)
	record(t, a, gh, "api", OutcomeAwaitingApply)
	record(t, a, gh, "web", OutcomeApplied)
	record(t, b, gh, "api", OutcomeApplied)

	assert.Equal(t, "success", gh.current().Conclusion)
	assert.Equal(t, "2/2 projects up to date", gh.current().Title)
}

// A write that lands while a publish is in flight is caught by the
// in-flight publisher's re-read, even if the writer never publishes.
func TestPublishAggregate_RepublishesWhenTheRecordMovedMidPublish(t *testing.T) {
	o := testAggregateOrchestrator(t)
	gh := newFakeChecksClient()
	ctx := context.Background()

	require.NoError(t, o.records.WriteOutcome(ctx, testRef, "api", ProjectEntry{Outcome: OutcomeAwaitingApply}))
	gh.during = func() {
		assert.NoError(t, o.records.WriteOutcome(ctx, testRef, "api", ProjectEntry{Outcome: OutcomeApplied}))
	}
	record(t, o, gh, "web", OutcomeApplied)

	assert.Equal(t, "success", gh.current().Conclusion, "the verdict of the write that landed mid-publish")
}

// A completed run is never reopened: leaving a completed state creates a
// new run, which is then the most recently updated.
func TestPublishAggregate_LeavingCompletedCreatesANewRun(t *testing.T) {
	o := testAggregateOrchestrator(t)
	gh := newFakeChecksClient()

	record(t, o, gh, "web", OutcomeApplyFailed)
	require.Equal(t, "failure", gh.current().Conclusion)

	record(t, o, gh, "web", OutcomeAwaitingApply) // re-planned on the same commit
	assert.Equal(t, "in_progress", gh.current().Status)
	assert.Equal(t, 2, gh.runCount())

	record(t, o, gh, "web", OutcomeApplied)
	assert.Equal(t, "success", gh.current().Conclusion)
	assert.Equal(t, 2, gh.runCount(), "an open run is updated in place")
}

// A stale completed hint is caught by the update failing.
func TestPublishAggregate_StaleHintFallsBackToCreating(t *testing.T) {
	o := testAggregateOrchestrator(t)
	gh := newFakeChecksClient()
	ctx := context.Background()

	record(t, o, gh, "web", OutcomeApplied)
	require.Equal(t, "success", gh.current().Conclusion)
	require.NoError(t, o.records.SetAggregateCheckDone(ctx, testRef, false))

	record(t, o, gh, "api", OutcomeAwaitingApply)
	assert.Equal(t, "in_progress", gh.current().Status)
}

func TestPublishAggregate_ErrorsAreReturnedNotSwallowed(t *testing.T) {
	o := testAggregateOrchestrator(t)
	gh := newFakeChecksClient()
	gh.createErr = errors.New("boom")

	err := o.recordOutcome(context.Background(), gh, testRef, "web", ProjectEntry{Outcome: OutcomeApplied})
	require.ErrorContains(t, err, "boom")

	st, err := o.records.ReadPRStatus(context.Background(), testRef)
	require.NoError(t, err)
	assert.Equal(t, OutcomeApplied, st.Projects["web"].Outcome, "the Outcome is recorded even when publishing fails")
}

// Feature: aggregate-check-run, Property 1: Published verdict converges
//
// Several instances write Outcomes for one commit concurrently, each
// publishing after its writes, with GitHub calls taking arbitrary time.
// Once all have finished, the run GitHub evaluates carries the verdict of
// the final record.
func TestProperty_PublishedVerdictConverges(t *testing.T) {
	outcomes := []Outcome{
		OutcomeAwaitingApply, OutcomeNothingToApply, OutcomeNotPlanned,
		OutcomeApplied, OutcomeApplyFailed,
	}
	redisClient := newTestRedisClient(t)
	rapid.Check(t, func(t *rapid.T) {
		require.NoError(t, redisClient.FlushAll(context.Background()).Err())
		gh := newFakeChecksClient()
		delays := rapid.SliceOfN(rapid.IntRange(0, 300), 1, 50).Draw(t, "delays")
		var next int
		var delayMu sync.Mutex
		gh.delay = func() {
			delayMu.Lock()
			d := delays[next%len(delays)]
			next++
			delayMu.Unlock()
			time.Sleep(time.Duration(d) * time.Microsecond)
		}

		instances := rapid.IntRange(2, 3).Draw(t, "instances")
		type write struct {
			project string
			outcome Outcome
		}
		plans := make([][]write, instances)
		for i := range plans {
			n := rapid.IntRange(1, 4).Draw(t, fmt.Sprintf("writes-%d", i))
			for range n {
				plans[i] = append(plans[i], write{
					project: rapid.SampledFrom([]string{"web", "api", "db"}).Draw(t, "project"),
					outcome: rapid.SampledFrom(outcomes).Draw(t, "outcome"),
				})
			}
		}

		var wg sync.WaitGroup
		for _, plan := range plans {
			o := &Orchestrator{plugins: testRegistry(), records: newRecordStore(redisClient), redis: redisClient}
			wg.Go(func() {
				for _, w := range plan {
					assert.NoError(t, o.recordOutcome(context.Background(), gh, testRef, w.project, ProjectEntry{Outcome: w.outcome, Operation: "diff"}))
				}
			})
		}
		wg.Wait()

		st, err := newRecordStore(redisClient).ReadPRStatus(context.Background(), testRef)
		require.NoError(t, err)
		want := verdictFor(st)
		cur := gh.current()
		if !shouldPublish(st, want) {
			assert.Nil(t, cur)
			return
		}
		require.NotNil(t, cur)
		assert.Equal(t, want.Status, cur.Status)
		assert.Equal(t, want.Conclusion, cur.Conclusion)
		assert.Equal(t, want.Title, cur.Title)
	})
}
