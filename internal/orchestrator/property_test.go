package orchestrator

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"pgregory.net/rapid"

	"github.com/ivanvc/turnip/internal/config"
	"github.com/ivanvc/turnip/internal/github"
	"github.com/ivanvc/turnip/internal/lock"
)

const identifierPattern = "[a-zA-Z][a-zA-Z0-9]{0,15}"

func genProject(t *rapid.T, tool, suffix string) config.Project {
	return config.Project{
		Name:      rapid.StringMatching(identifierPattern).Draw(t, "name") + suffix,
		Directory: rapid.StringMatching(identifierPattern).Draw(t, "directory"),
		Tool:      tool,
	}
}

func newPropertyRedisClient(t *rapid.T) *redis.Client {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr(), MaxRetries: -1})
	t.Cleanup(func() { _ = client.Close() })
	return client
}

// Feature: multi-iac-automation-platform, Property 5: PR Event Triggers Plan Operations
func TestProperty_PREventTriggersPlanOperations(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.IntRange(0, 5).Draw(t, "n")
		var matched []config.Project
		for i := range n {
			matched = append(matched, genProject(t, "helmfile", string(rune('a'+i))))
		}
		registry := testRegistry()

		targets := planTargetsFor(matched, registry, config.CloneSpec{})

		require.Len(t, targets, len(matched))
		for i, target := range targets {
			require.Equal(t, matched[i].Name, target.Project.Name)
			require.Equal(t, registry["helmfile"].GetPlanOperation(), target.Operation)
			require.Equal(t, "auto", target.TriggeredBy)
		}
	})
}

// Feature: multi-iac-automation-platform, Property 6: Runner Creation Per Triggered Project
func TestProperty_RunnerCreationPerTriggeredProject(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.IntRange(1, 4).Draw(t, "n")
		var targets []Target
		for i := range n {
			targets = append(targets, Target{
				Project:   genProject(t, "helmfile", string(rune('a'+i))),
				Operation: "diff",
			})
		}

		redisClient := newPropertyRedisClient(t)
		jobsClient := &fakeJobCreator{t: t, redis: redisClient, result: github.ProjectResult{Success: true}}
		o := &Orchestrator{
			locks:         &fakeLockManager{},
			jobs:          jobsClient,
			plugins:       testRegistry(),
			records:       newRecordStore(redisClient),
			redis:         redisClient,
			startTimeout:  5 * time.Minute,
			sweepInterval: 30 * time.Second,
		}
		client := &fakeExecuteClient{}

		results := o.executeTargets(context.Background(), client, testRepo, testPR, 1, targets)

		require.Len(t, results, n)
		require.Equal(t, n, jobsClient.createCount(), "exactly one Job creation attempt per triggered Project")
	})
}

// Feature: multi-iac-automation-platform, Property 9: Lock Acquisition Prevents Concurrent Operations
func TestProperty_LockAcquisitionPreventsConcurrentOperations(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		projectKey := rapid.StringMatching(identifierPattern).Draw(t, "projectKey")
		prA := rapid.IntRange(1, 100000).Draw(t, "prA")
		prB := rapid.IntRange(100001, 200000).Draw(t, "prB")

		locks := lock.NewRedisLockManager(newPropertyRedisClient(t))

		okA, err := locks.AcquireLock(context.Background(), projectKey, prA, "https://example.com/a", "alice")
		require.NoError(t, err)
		require.True(t, okA)

		okB, err := locks.AcquireLock(context.Background(), projectKey, prB, "https://example.com/b", "bob")
		require.NoError(t, err)
		require.False(t, okB)
	})
}

// genMatchableProject generates a Project whose whenModified pattern is
// derived from its own directory, so a generated path either matches it or
// does not.
//
// genProject is deliberately not reused: it carries no whenModified at
// all, and MatchProjects matches nothing without a pattern — so the
// equivalence below would compare empty against empty and pass while
// testing nothing.
func genMatchableProject(t *rapid.T, suffix string) config.Project {
	dir := rapid.StringMatching(identifierPattern).Draw(t, "dir") + suffix
	return config.Project{
		Name:         "p" + suffix,
		Directory:    dir,
		Tool:         "helmfile",
		WhenModified: []string{dir + "/**"},
	}
}

// Feature: project-selection, Property: bare plan equals autoplan selection
//
// Requirement 1.2 asks that a bare plan use the same matching the
// automatic plan uses rather than a parallel implementation of it. "It
// calls the same function" is a claim about code that only review can
// check, and review is what missed the two selection paths disagreeing in
// the first place. Stated as an equivalence it becomes falsifiable: any
// reimplementation that differs on any generated input fails here.
func TestProperty_BarePlanEqualsAutoplanSelection(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.IntRange(1, 5).Draw(t, "n")
		var projects []config.Project
		for i := range n {
			projects = append(projects, genMatchableProject(t, string(rune('a'+i))))
		}

		// Paths drawn from the Projects' own directories, so the matched
		// subset varies across runs instead of being all or nothing.
		var files []string
		for _, p := range projects {
			if rapid.Bool().Draw(t, "touch-"+p.Name) {
				files = append(files, p.Directory+"/main.tf")
			}
		}

		sel := testSelection(&config.Config{Projects: projects},
			github.NewAuthorizer(&fakeAuthClient{permission: "write"}))
		sel.prNumber = 42
		sel.modifiedSet = func(context.Context) ([]string, error) { return files, nil }

		targets, _, _, err := sel.resolve(context.Background(),
			&github.TriggerCommand{Tool: "helmfile", Operation: "diff"})

		want := config.MatchProjects(projects, files)

		if len(want) == 0 {
			// The other half of Requirement 1.3: an empty Modified_Set is
			// reported, never silently nothing.
			require.Error(t, err)
			require.ErrorIs(t, err, ErrNoModifiedProjects)
			return
		}

		require.NoError(t, err)
		require.Len(t, targets, len(want))
		for i, p := range want {
			require.Equal(t, p.Name, targets[i].Project.Name)
		}
	})
}

// Feature: multi-iac-automation-platform, Property 17: Comment Contains All Project Results
func TestProperty_ConsolidatedCommentContainsAllProjectResults(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.IntRange(1, 6).Draw(t, "n")
		var results []github.ProjectResult
		for i := range n {
			op := "diff"
			if i%2 == 1 {
				op = "apply"
			}
			results = append(results, github.ProjectResult{
				ProjectName: rapid.StringMatching(identifierPattern).Draw(t, "name") + string(rune('a'+i)),
				Tool:        "helmfile",
				Operation:   op,
				Success:     i%3 != 0,
			})
		}

		redisClient := newPropertyRedisClient(t)
		o := &Orchestrator{plugins: testRegistry(), records: newRecordStore(redisClient), redis: redisClient}
		fake := &fakeCommentClient{}

		o.postResults(context.Background(), fake, testRepo, 42, results)

		combined := ""
		for _, body := range fake.posted {
			combined += body
		}
		for _, r := range results {
			require.Contains(t, combined, r.ProjectName)
		}
	})
}
