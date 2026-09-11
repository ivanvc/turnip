//go:build load

package orchestrator

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivanvc/turnip/internal/github"
)

// load_test.go validates Requirement 1.4 of the ha-validation slice: 100
// concurrent webhook events against a real Redis, asserting no deadlock
// and that lock contention resolves cleanly. Build-tag gated (`load`) and
// run on demand via `make test-load` — deliberately not part of the
// default `go test -race ./...` suite (Requirement 1.7): this is a load
// test, not a correctness check every commit needs to pay for.
func TestLoad_100ConcurrentEventsNoDeadlock(t *testing.T) {
	addr := realTestRedisAddr(t)

	const numEvents = 100
	owner := "owner-" + uuid.NewString()[:8]
	repo := "repo"
	client := newHAFakeClient([]byte(haTurnipYAML))

	o := newHAOrchestrator(t, addr, client)
	wireFakeJobCreator(t, o, github.ProjectResult{Success: true, Output: "no changes"})

	var wg sync.WaitGroup
	for i := 1; i <= numEvents; i++ {
		wg.Add(1)
		go func(prNumber int) {
			defer wg.Done()
			assert.NoError(t, o.HandlePullRequest(context.Background(), haPlanEvent(owner, repo, prNumber)))
		}(i)
	}

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("100 concurrent HandlePullRequest calls did not all return within 10s — possible deadlock")
	}

	require.Eventually(t, func() bool {
		posted := 0
		for i := 1; i <= numEvents; i++ {
			if len(client.commentsFor(i)) >= 1 {
				posted++
			}
		}
		// The winner's own Operation never resolves within this test's
		// window (nothing ever calls HandleResult for it — see
		// wireFakeJobCreator's doc comment in ha_test.go), so only the
		// numEvents-1 rejections are expected to post.
		return posted >= numEvents-1
	}, 10*time.Second, 20*time.Millisecond, "expected at least numEvents-1 rejection comments to post")

	rejected := 0
	for i := 1; i <= numEvents; i++ {
		comments := client.commentsFor(i)
		if len(comments) > 0 && strings.Contains(comments[0], "locked by PR #") {
			rejected++
		}
	}
	assert.Equal(t, numEvents-1, rejected, "exactly one PR should have acquired the lock; every other one should be rejected as already locked")
}
