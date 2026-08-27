package orchestrator

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
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

func TestWaitForDone_ReceivesPublishedResult(t *testing.T) {
	client := newTestRedisClient(t)
	ctx := context.Background()

	resultCh := make(chan github.ProjectResult, 1)
	errCh := make(chan error, 1)
	go func() {
		r, err := waitForDone(ctx, client, "op-1")
		resultCh <- r
		errCh <- err
	}()

	// Give the subscription time to establish before publishing —
	// Pub/Sub has no replay, so a publish before the subscriber is ready
	// would be lost (miniredis mirrors real Redis's semantics here).
	require.Eventually(t, func() bool {
		return client.PubSubNumSub(ctx, doneChannel("op-1")).Val()[doneChannel("op-1")] > 0
	}, time.Second, time.Millisecond)

	want := github.ProjectResult{ProjectName: "proj", Operation: "diff", Success: true, Output: "ok"}
	require.NoError(t, publishDone(ctx, client, "op-1", want))

	select {
	case got := <-resultCh:
		require.NoError(t, <-errCh)
		assert.Equal(t, want, got)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for waitForDone to return")
	}
}

func TestWaitForDone_ReturnsOnContextCancellation(t *testing.T) {
	client := newTestRedisClient(t)
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		_, err := waitForDone(ctx, client, "op-1")
		done <- err
	}()

	cancel()

	select {
	case err := <-done:
		require.Error(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("waitForDone did not return promptly after context cancellation")
	}
}
