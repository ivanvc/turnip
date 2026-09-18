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
