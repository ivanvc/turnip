package lock

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisLockManager is a LockManager backed by Redis/Valkey.
type RedisLockManager struct {
	client *redis.Client
}

// NewRedisLockManager constructs a RedisLockManager using client.
func NewRedisLockManager(client *redis.Client) *RedisLockManager {
	return &RedisLockManager{client: client}
}

var _ LockManager = (*RedisLockManager)(nil)

func lockKey(projectKey string) string {
	return "lock:" + projectKey
}

// maxStateAttempts bounds the compare-and-swap loop. A retry happens only
// when another Operation moved the same Lock between this one's read and
// its write, which is rare and cannot repeat indefinitely without a
// competing writer also making progress.
const maxStateAttempts = 5

// ErrLockContended reports that the state moved under a caller repeatedly.
var ErrLockContended = errors.New("lock: state changed under the caller too many times")

func (m *RedisLockManager) AcquireForPlan(ctx context.Context, projectKey string, prNumber int, pullRequestURL, lockedBy string) (bool, Transition, error) {
	for range maxStateAttempts {
		data, err := m.getLockData(ctx, projectKey)
		if err != nil {
			return false, Transition{}, err
		}

		if data == nil {
			created, err := m.createLock(ctx, projectKey, prNumber, pullRequestURL, lockedBy)
			if err != nil {
				return false, Transition{}, err
			}
			if created {
				return true, Transition{To: StatePlanning}, nil
			}
			// Someone created it between the read and the write. Re-read
			// rather than assume whose it is.
			continue
		}

		if data.PRNumber != prNumber {
			return false, Transition{}, nil
		}

		from := data.DecodedState()
		tr, ok := ApplyEvent(from, EventPlanDispatched)
		if !ok {
			return true, tr, nil
		}
		// Planning and PlanStale dispatch to themselves, so a Lock already
		// written with that state needs no write at all. The comparison is
		// against the *stored* value rather than the decoded one, so a Lock
		// written before states existed is upgraded to an explicit state
		// here instead of being left to decode forever.
		if tr.To == from && data.State == from {
			return true, tr, nil
		}

		updated := *data
		updated.State = tr.To
		code, err := m.mutate(ctx, projectKey, prNumber, data.State, "set", &updated)
		if err != nil {
			return false, Transition{}, err
		}
		switch code {
		case 1:
			return true, tr, nil
		case 0:
			continue
		case -1:
			return false, Transition{}, nil
		default:
			continue
		}
	}

	return false, Transition{}, fmt.Errorf("lock: acquiring for plan on %q (PR #%d): %w", projectKey, prNumber, ErrLockContended)
}

func (m *RedisLockManager) Apply(ctx context.Context, projectKey string, prNumber int, ev Event, plan *PlanRecord) (Transition, error) {
	for range maxStateAttempts {
		data, err := m.getLockData(ctx, projectKey)
		if err != nil {
			return Transition{}, err
		}
		// An event for a Lock that no longer exists is a no-op, not a
		// failure: closing a pull request mid-apply deletes it, and the
		// Operation's own result still has to reach the reader.
		if data == nil {
			return Transition{}, nil
		}
		if data.PRNumber != prNumber {
			return Transition{}, fmt.Errorf("lock: applying %q to %q (PR #%d): %w", ev, projectKey, prNumber, ErrLockedByOtherPR)
		}

		from := data.DecodedState()
		tr, ok := ApplyEvent(from, ev)
		if !ok {
			// Not a combination the table expects — reachable only by a
			// race, where a result arrives for an Operation the Lock has
			// already moved past. Change nothing rather than guess.
			return tr, nil
		}

		op, updated := "del", (*LockData)(nil)
		if !tr.Released {
			next := *data
			next.State = tr.To
			if plan != nil {
				next.PlanArgs = plan.Args
				next.PlanData = plan.Data
				next.PlanSummary = plan.Summary
			}
			op, updated = "set", &next
		}

		code, err := m.mutate(ctx, projectKey, prNumber, data.State, op, updated)
		if err != nil {
			return Transition{}, err
		}
		switch code {
		case 1:
			return tr, nil
		case 0:
			return Transition{}, nil
		case -1:
			return Transition{}, fmt.Errorf("lock: applying %q to %q (PR #%d): %w", ev, projectKey, prNumber, ErrLockedByOtherPR)
		default:
			continue
		}
	}

	return Transition{}, fmt.Errorf("lock: applying %q to %q (PR #%d): %w", ev, projectKey, prNumber, ErrLockContended)
}

// createLock writes a Lock only when the key is absent. SET NX remains the
// primitive that decides who holds a Lock; the state machine adds a field
// to the value it guards, never a second way to decide ownership.
func (m *RedisLockManager) createLock(ctx context.Context, projectKey string, prNumber int, pullRequestURL, lockedBy string) (bool, error) {
	data := LockData{
		PRNumber:       prNumber,
		PullRequestURL: pullRequestURL,
		LockedAt:       time.Now(),
		LockedBy:       lockedBy,
		State:          StatePlanning,
	}
	payload, err := json.Marshal(data)
	if err != nil {
		return false, fmt.Errorf("lock: marshaling new lock data for %q: %w", projectKey, err)
	}

	created, err := m.client.SetNX(ctx, lockKey(projectKey), payload, 0).Result()
	if err != nil {
		return false, fmt.Errorf("lock: creating lock for %q: %w", projectKey, err)
	}

	return created, nil
}

// mutate runs the state-guarded mutation and returns the script's raw
// code, so the caller can tell "the state moved" from "a different PR
// holds it" and retry only the former.
func (m *RedisLockManager) mutate(ctx context.Context, projectKey string, prNumber int, expected LockState, op string, data *LockData) (int64, error) {
	var payload []byte
	if data != nil {
		var err error
		payload, err = json.Marshal(data)
		if err != nil {
			return 0, fmt.Errorf("lock: marshaling lock data for %q: %w", projectKey, err)
		}
	}

	res, err := m.client.Eval(ctx, compareStateAndMutateScript, []string{lockKey(projectKey)},
		strconv.Itoa(prNumber), string(expected), op, payload).Result()
	if err != nil {
		return 0, fmt.Errorf("lock: mutating lock for %q (PR #%d): %w", projectKey, prNumber, err)
	}

	return toInt64(res), nil
}

func (m *RedisLockManager) GetPlan(ctx context.Context, projectKey string, prNumber int) (PlanRecord, error) {
	data, err := m.getLockData(ctx, projectKey)
	if err != nil {
		return PlanRecord{}, err
	}
	if data == nil {
		return PlanRecord{}, fmt.Errorf("lock: getting plan for %q (PR #%d): %w", projectKey, prNumber, ErrNoLock)
	}
	if data.PRNumber != prNumber {
		return PlanRecord{}, fmt.Errorf("lock: getting plan for %q (PR #%d): %w", projectKey, prNumber, ErrLockedByOtherPR)
	}
	// Keyed on the state, not on the artifact's length. Keying this off
	// len(PlanData) is what made a Helmfile apply unreachable: its plan
	// legitimately produces no bytes, so every apply was refused as though
	// no plan had ever run. The state also answers the question the old
	// has_plan flag could not — whether the recorded plan is still true.
	if data.DecodedState() != StatePlanReady {
		return PlanRecord{}, fmt.Errorf("lock: getting plan for %q (PR #%d): %w", projectKey, prNumber, ErrNoPlan)
	}

	return PlanRecord{
		Data:    data.PlanData,
		Args:    data.PlanArgs,
		Summary: data.PlanSummary,
	}, nil
}

func (m *RedisLockManager) GetLockStatus(ctx context.Context, projectKey string) (*LockStatus, error) {
	data, err := m.getLockData(ctx, projectKey)
	if err != nil {
		return nil, err
	}
	if data == nil {
		return &LockStatus{Locked: false}, nil
	}

	return &LockStatus{
		Locked:         true,
		PRNumber:       data.PRNumber,
		PullRequestURL: data.PullRequestURL,
		LockedAt:       data.LockedAt,
		LockedBy:       data.LockedBy,
		PlanSummary:    data.PlanSummary,
		State:          data.DecodedState(),
	}, nil
}

func (m *RedisLockManager) IsLockedByPR(ctx context.Context, projectKey string, prNumber int) (bool, error) {
	status, err := m.GetLockStatus(ctx, projectKey)
	if err != nil {
		return false, err
	}

	return status.Locked && status.PRNumber == prNumber, nil
}

// getLockData fetches and decodes the raw Lock at projectKey, returning
// (nil, nil) when no Lock exists.
func (m *RedisLockManager) getLockData(ctx context.Context, projectKey string) (*LockData, error) {
	raw, err := m.client.Get(ctx, lockKey(projectKey)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("lock: reading lock for %q: %w", projectKey, err)
	}

	var data LockData
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, fmt.Errorf("lock: decoding lock for %q: %w", projectKey, err)
	}

	return &data, nil
}

func toInt64(v any) int64 {
	n, _ := v.(int64)
	return n
}
