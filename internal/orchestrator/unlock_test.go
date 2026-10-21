package orchestrator

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivanvc/turnip/internal/lock"
)

// listHeld scripts ListLocks (and nothing else) for owner/repo: project
// name to the pull request holding it.
func listHeld(held map[string]int) func(context.Context, string) ([]lock.ListedLock, error) {
	return func(_ context.Context, prefix string) ([]lock.ListedLock, error) {
		var out []lock.ListedLock
		for name, pr := range held {
			key := "owner/repo/" + name
			if strings.HasPrefix(key, prefix) {
				out = append(out, lock.ListedLock{ProjectKey: key, Status: lock.LockStatus{Locked: true, PRNumber: pr, State: lock.StatePlanReady}})
			}
		}
		return out, nil
	}
}

// statusHeld scripts GetLockStatus for named reads; a name not in held has
// no Lock.
func statusHeld(held map[string]int) func(context.Context, string) (*lock.LockStatus, error) {
	return func(_ context.Context, key string) (*lock.LockStatus, error) {
		pr, ok := held[strings.TrimPrefix(key, "owner/repo/")]
		if !ok {
			return &lock.LockStatus{}, nil
		}
		return &lock.LockStatus{Locked: true, PRNumber: pr, State: lock.StatePlanReady}, nil
	}
}

// unlockFixture is a lock manager over the given holders whose Apply
// removes a Lock the way the real one does, recording what was released.
func unlockFixture(t *testing.T, held map[string]int) (*fakeLockManager, *fakeCommentEventClient, *Orchestrator, *[]string) {
	t.Helper()
	var released []string
	locks := &fakeLockManager{
		listLocksFunc:     listHeld(held),
		getLockStatusFunc: statusHeld(held),
		applyFunc: func(_ context.Context, key string, pr int, ev lock.Event, _ *lock.PlanRecord) (lock.Transition, error) {
			name := strings.TrimPrefix(key, "owner/repo/")
			holder, ok := held[name]
			if !ok {
				return lock.Transition{}, nil
			}
			if holder != pr {
				return lock.Transition{}, lock.ErrLockedByOtherPR
			}
			released = append(released, name)
			delete(held, name)
			return lock.Transition{Released: true}, nil
		},
	}
	// No configuration at all: an unlock-only comment never fetches it, so
	// a missing file must not show up in the reply.
	client := &fakeCommentEventClient{permission: "write", pr: openPR()}
	return locks, client, testCommentOrchestrator(t, locks), &released
}

func unlockReply(t *testing.T, o *Orchestrator, client *fakeCommentEventClient, body string) string {
	t.Helper()
	require.NoError(t, callHandleIssueComment(o, client, commentEvent(body, "alice")))
	assert.Empty(t, client.getFileCallLog(), "an unlock-only comment makes no configuration request")
	posted := client.postedComments()
	require.Len(t, posted, 1)
	return posted[0]
}

func TestUnlock_BareReleasesOnlyThisPullRequestsLocks(t *testing.T) {
	_, client, o, released := unlockFixture(t, map[string]int{"web": 42, "api": 42, "ops": 12})
	reply := unlockReply(t, o, client, "/turnip unlock")

	assert.ElementsMatch(t, []string{"web", "api"}, *released)
	assert.Equal(t, "Unlocked:\n- api\n- web\n", reply)
	assert.NotContains(t, reply, "ops")
}

func TestUnlock_NamedHeldElsewhere(t *testing.T) {
	_, client, o, released := unlockFixture(t, map[string]int{"web": 12})
	reply := unlockReply(t, o, client, "/turnip unlock web")

	assert.Empty(t, *released)
	assert.Contains(t, reply, "Not unlocked (held by a different PR):\n- web (held by #12)")
}

func TestUnlock_NamedWithoutLockIsUnmentioned(t *testing.T) {
	_, client, o, _ := unlockFixture(t, map[string]int{})
	assert.Equal(t, "No projects matched this unlock command.", unlockReply(t, o, client, "/turnip unlock web"))
}

func TestUnlock_StarIsAName(t *testing.T) {
	_, client, o, released := unlockFixture(t, map[string]int{"web": 42})
	assert.Equal(t, "No projects matched this unlock command.", unlockReply(t, o, client, "/turnip unlock *"))
	assert.Empty(t, *released)
}

func TestUnlock_BareWithNothingHeld(t *testing.T) {
	_, client, o, _ := unlockFixture(t, map[string]int{"api": 12})
	assert.Equal(t, "No projects matched this unlock command.", unlockReply(t, o, client, "/turnip unlock"))
}

func TestUnlock_ProjectTheConfigurationNoLongerDeclares(t *testing.T) {
	_, client, o, released := unlockFixture(t, map[string]int{"web": 42})
	reply := unlockReply(t, o, client, "/turnip unlock web")

	assert.Equal(t, []string{"web"}, *released)
	assert.Equal(t, "Unlocked:\n- web\n", reply)
}

func TestUnlock_ToolTriggerWordActsAsTurnip(t *testing.T) {
	_, client, o, released := unlockFixture(t, map[string]int{"web": 42, "api": 42})
	reply := unlockReply(t, o, client, "/helmfile unlock")

	assert.ElementsMatch(t, []string{"web", "api"}, *released)
	assert.Contains(t, reply, "Unlocked:")
}

func TestUnlock_FailedListReleasesNothing(t *testing.T) {
	locks, client, o, released := unlockFixture(t, map[string]int{"web": 42})
	locks.listLocksFunc = func(context.Context, string) ([]lock.ListedLock, error) {
		return nil, errors.New("redis down")
	}
	reply := unlockReply(t, o, client, "/turnip unlock")

	assert.Empty(t, *released)
	assert.Equal(t, "turnip could not read this pull request's locks, so nothing was unlocked. Comment again to retry.", reply)
}

func TestUnlock_FailedReadAmongSeveralNamesReleasesNothing(t *testing.T) {
	locks, client, o, released := unlockFixture(t, map[string]int{"web": 42, "api": 42})
	read := statusHeld(map[string]int{"web": 42, "api": 42})
	locks.getLockStatusFunc = func(ctx context.Context, key string) (*lock.LockStatus, error) {
		if strings.HasSuffix(key, "/api") {
			return nil, errors.New("redis down")
		}
		return read(ctx, key)
	}
	reply := unlockReply(t, o, client, "/turnip unlock web api")

	assert.Empty(t, *released)
	assert.Contains(t, reply, "nothing was unlocked")
}

func TestUnlock_RefusedAuthorReadsNoLocks(t *testing.T) {
	locks, client, o, _ := unlockFixture(t, map[string]int{"web": 42})
	client.permission = "read"
	called := false
	locks.listLocksFunc = func(context.Context, string) ([]lock.ListedLock, error) {
		called = true
		return nil, nil
	}
	reply := unlockReply(t, o, client, "/turnip unlock")

	assert.False(t, called)
	assert.Equal(t, "@alice does not have write permission required to unlock.", reply)
}
