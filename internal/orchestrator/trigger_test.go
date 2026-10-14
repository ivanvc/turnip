package orchestrator

import (
	"context"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/ivanvc/turnip/internal/config"
	"github.com/ivanvc/turnip/internal/github"
)

func newTestTriggerStore(t *testing.T) (*triggerStore, *redis.Client) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr(), MaxRetries: -1})
	t.Cleanup(func() { _ = client.Close() })
	return newTriggerStore(client), client
}

func testTriggerMeta() triggerMeta {
	return triggerMeta{
		Repo:           github.Repository{ID: 7, Owner: "owner", Name: "repo", URL: "https://example.com/owner/repo"},
		InstallationID: 99,
		PRNumber:       42,
		HeadSHA:        "abc",
		BaseRef:        "main",
		HeadRef:        "feature",
		Notices:        []string{"note one"},
		Commands: [][]Target{
			{{Project: config.Project{Name: "web", Directory: "web", Tool: "helmfile"}, Operation: "diff", ExtraArgs: []string{"--x"}, TriggeredBy: "alice"}},
			{{Project: config.Project{Name: "web", Directory: "web", Tool: "helmfile"}, Operation: "apply", TriggeredBy: "alice"}},
		},
	}
}

func testProjectResult(name string) github.ProjectResult {
	return github.ProjectResult{ProjectName: name, Tool: "helmfile", Operation: "diff", Success: true, Output: "out " + name}
}

func TestTriggerStore_RoundTrip(t *testing.T) {
	s, _ := newTestTriggerStore(t)
	ctx := context.Background()
	meta := testTriggerMeta()
	refused := []github.ProjectResult{{ProjectName: "api", Tool: "helmfile", Operation: "apply", Output: "refused"}}

	require.NoError(t, s.Create(ctx, "t1", meta, refused))
	st, err := s.Get(ctx, "t1")
	require.NoError(t, err)
	require.NotNil(t, st)
	assert.Equal(t, meta, st.Meta)
	assert.Equal(t, 0, st.Command)
	assert.Equal(t, refused, st.Results)
}

func TestTriggerStore_GetMissing(t *testing.T) {
	s, _ := newTestTriggerStore(t)
	st, err := s.Get(context.Background(), "nope")
	require.NoError(t, err)
	assert.Nil(t, st)
}

func TestTriggerStore_BeginCommandResetsExpiry(t *testing.T) {
	s, client := newTestTriggerStore(t)
	ctx := context.Background()
	require.NoError(t, s.Create(ctx, "t1", testTriggerMeta(), nil))

	require.NoError(t, client.Expire(ctx, triggerKey("t1"), time.Second).Err())
	require.NoError(t, s.BeginCommand(ctx, "t1", 1, []string{"web"}))

	ttl, err := client.TTL(ctx, triggerKey("t1")).Result()
	require.NoError(t, err)
	assert.Greater(t, ttl, triggerTTL-time.Minute)
	assert.LessOrEqual(t, ttl, triggerTTL)

	st, err := s.Get(ctx, "t1")
	require.NoError(t, err)
	assert.Equal(t, 1, st.Command)
}

func TestTriggerStore_CompleteReturnsPending(t *testing.T) {
	s, _ := newTestTriggerStore(t)
	ctx := context.Background()
	require.NoError(t, s.Create(ctx, "t1", testTriggerMeta(), nil))
	require.NoError(t, s.BeginCommand(ctx, "t1", 0, []string{"web", "api", "db"}))

	for i, name := range []string{"web", "api", "db"} {
		n, err := s.Complete(ctx, "t1", 0, name, uuid.NewString(), testProjectResult(name))
		require.NoError(t, err)
		assert.Equal(t, 2-i, n)
	}

	st, err := s.Get(ctx, "t1")
	require.NoError(t, err)
	require.Len(t, st.Results, 3)
	assert.Equal(t, "web", st.Results[0].ProjectName)
	assert.Equal(t, "db", st.Results[2].ProjectName)
}

// A completion tried again with its own token is given the answer it was
// given the first time, however late, so a try whose answer was lost still
// learns whether it finished the command. Another token is answered -1,
// and neither stores anything.
func TestTriggerStore_TheSameTokenIsAnsweredAgain(t *testing.T) {
	s, _ := newTestTriggerStore(t)
	ctx := context.Background()
	require.NoError(t, s.Create(ctx, "t1", testTriggerMeta(), nil))
	require.NoError(t, s.BeginCommand(ctx, "t1", 0, []string{"web", "api"}))

	n, err := s.Complete(ctx, "t1", 0, "api", "token-a", testProjectResult("api"))
	require.NoError(t, err)
	require.Equal(t, 1, n)
	n, err = s.Complete(ctx, "t1", 0, "web", "token-b", testProjectResult("web"))
	require.NoError(t, err)
	require.Equal(t, 0, n, "web finished the command")

	n, err = s.Complete(ctx, "t1", 0, "web", "token-b", testProjectResult("web"))
	require.NoError(t, err)
	assert.Equal(t, 0, n, "the same token is told again that it finished the command")
	n, err = s.Complete(ctx, "t1", 0, "api", "token-a", testProjectResult("api"))
	require.NoError(t, err)
	assert.Equal(t, 1, n, "and api's token what api was told")
	n, err = s.Complete(ctx, "t1", 0, "web", "token-c", testProjectResult("web"))
	require.NoError(t, err)
	assert.Equal(t, -1, n, "another completion of web is refused")

	st, err := s.Get(ctx, "t1")
	require.NoError(t, err)
	assert.Len(t, st.Results, 2, "no try stores a second result")
}

func TestTriggerStore_SecondCompletionStoresNothing(t *testing.T) {
	s, _ := newTestTriggerStore(t)
	ctx := context.Background()
	require.NoError(t, s.Create(ctx, "t1", testTriggerMeta(), nil))
	require.NoError(t, s.BeginCommand(ctx, "t1", 0, []string{"web", "api"}))

	n, err := s.Complete(ctx, "t1", 0, "web", uuid.NewString(), testProjectResult("web"))
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	n, err = s.Complete(ctx, "t1", 0, "web", uuid.NewString(), testProjectResult("web"))
	require.NoError(t, err)
	assert.Equal(t, -1, n)

	st, err := s.Get(ctx, "t1")
	require.NoError(t, err)
	assert.Len(t, st.Results, 1)
}

func TestTriggerStore_OneProjectInTwoCommands(t *testing.T) {
	s, client := newTestTriggerStore(t)
	ctx := context.Background()
	require.NoError(t, s.Create(ctx, "t1", testTriggerMeta(), nil))
	require.NoError(t, s.BeginCommand(ctx, "t1", 0, []string{"web"}))
	require.NoError(t, s.BeginCommand(ctx, "t1", 1, []string{"web"}))

	fields, err := client.HKeys(ctx, triggerKey("t1")).Result()
	require.NoError(t, err)
	assert.Contains(t, fields, "pending:0:web")
	assert.Contains(t, fields, "pending:1:web")

	n, err := s.Complete(ctx, "t1", 0, "web", uuid.NewString(), testProjectResult("web"))
	require.NoError(t, err)
	assert.Equal(t, 0, n)
	n, err = s.Complete(ctx, "t1", 1, "web", uuid.NewString(), testProjectResult("web"))
	require.NoError(t, err)
	assert.Equal(t, 0, n)
}

func TestTriggerStore_CompleteMissingTrigger(t *testing.T) {
	s, _ := newTestTriggerStore(t)
	n, err := s.Complete(context.Background(), "gone", 0, "web", uuid.NewString(), testProjectResult("web"))
	require.NoError(t, err)
	assert.Equal(t, -1, n)
}

func TestTriggerStore_Delete(t *testing.T) {
	s, _ := newTestTriggerStore(t)
	ctx := context.Background()
	require.NoError(t, s.Create(ctx, "t1", testTriggerMeta(), nil))
	require.NoError(t, s.Delete(ctx, "t1"))
	st, err := s.Get(ctx, "t1")
	require.NoError(t, err)
	assert.Nil(t, st)
}

// Feature: fix-results-comment-on-replica-loss, Property 1: Exactly one completion finishes a command
func TestProperty_ExactlyOneCompletionFinishesACommand(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		client := newPropertyRedisClient(t)
		s := newTriggerStore(client)
		ctx := context.Background()

		n := rapid.IntRange(1, 12).Draw(t, "n")
		names := make([]string, n)
		for i := range names {
			names[i] = "p" + strconv.Itoa(i)
		}
		order := rapid.Permutation(names).Draw(t, "order")

		require.NoError(t, s.Create(ctx, "t1", testTriggerMeta(), nil))
		require.NoError(t, s.BeginCommand(ctx, "t1", 0, names))

		returns := make([]int, n)
		var wg sync.WaitGroup
		for i, name := range order {
			wg.Add(1)
			go func() {
				defer wg.Done()
				got, err := s.Complete(ctx, "t1", 0, name, uuid.NewString(), testProjectResult(name))
				assert.NoError(t, err)
				returns[i] = got
			}()
		}
		wg.Wait()

		zeros := 0
		for _, got := range returns {
			if got == 0 {
				zeros++
			}
			assert.GreaterOrEqual(t, got, 0)
		}
		assert.Equal(t, 1, zeros)

		st, err := s.Get(ctx, "t1")
		require.NoError(t, err)
		require.Len(t, st.Results, n)
		seen := map[string]int{}
		for _, r := range st.Results {
			seen[r.ProjectName]++
		}
		for _, name := range names {
			assert.Equal(t, 1, seen[name], name)
		}
	})
}
