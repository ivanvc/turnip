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

func (m *RedisLockManager) AcquireLock(ctx context.Context, projectKey string, prNumber int, pullRequestURL, lockedBy string) (bool, error) {
	data := LockData{
		PRNumber:       prNumber,
		PullRequestURL: pullRequestURL,
		LockedAt:       time.Now(),
		LockedBy:       lockedBy,
	}
	payload, err := json.Marshal(data)
	if err != nil {
		return false, fmt.Errorf("lock: marshaling new lock data for %q: %w", projectKey, err)
	}

	res, err := m.client.Eval(ctx, acquireScript, []string{lockKey(projectKey)}, strconv.Itoa(prNumber), payload).Result()
	if err != nil {
		return false, fmt.Errorf("lock: acquiring lock for %q: %w", projectKey, err)
	}

	return toInt64(res) == 1, nil
}

func (m *RedisLockManager) StorePlan(ctx context.Context, projectKey string, prNumber int, plan PlanRecord) error {
	existing, err := m.getLockData(ctx, projectKey)
	if err != nil {
		return err
	}
	if existing == nil {
		return fmt.Errorf("lock: storing plan for %q (PR #%d): %w", projectKey, prNumber, ErrNoLock)
	}

	// HasPlan is set from the fact of a successful plan, never from
	// whether the Plugin handed back an artifact — that distinction is the
	// whole point of the field.
	existing.HasPlan = true
	existing.PlanArgs = plan.Args
	existing.PlanData = plan.Data
	existing.PlanSummary = plan.Summary
	payload, err := json.Marshal(existing)
	if err != nil {
		return fmt.Errorf("lock: marshaling plan for %q: %w", projectKey, err)
	}

	res, err := m.client.Eval(ctx, compareAndMutateScript, []string{lockKey(projectKey)}, strconv.Itoa(prNumber), "set", payload).Result()
	if err != nil {
		return fmt.Errorf("lock: storing plan for %q (PR #%d): %w", projectKey, prNumber, err)
	}

	switch toInt64(res) {
	case 1:
		return nil
	case 0:
		return fmt.Errorf("lock: storing plan for %q (PR #%d): %w", projectKey, prNumber, ErrNoLock)
	default:
		return fmt.Errorf("lock: storing plan for %q (PR #%d): %w", projectKey, prNumber, ErrLockedByOtherPR)
	}
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
	// Tests the recorded fact, not the artifact's length. Keying this off
	// len(PlanData) is what made a Helmfile apply unreachable: its plan
	// legitimately produces no bytes, so every apply was refused as though
	// no plan had ever run.
	if !data.HasPlan {
		return PlanRecord{}, fmt.Errorf("lock: getting plan for %q (PR #%d): %w", projectKey, prNumber, ErrNoPlan)
	}

	return PlanRecord{
		Data:    data.PlanData,
		Args:    data.PlanArgs,
		Summary: data.PlanSummary,
	}, nil
}

func (m *RedisLockManager) ReleaseLock(ctx context.Context, projectKey string, prNumber int) error {
	res, err := m.client.Eval(ctx, compareAndMutateScript, []string{lockKey(projectKey)}, strconv.Itoa(prNumber), "del", "").Result()
	if err != nil {
		return fmt.Errorf("lock: releasing lock for %q (PR #%d): %w", projectKey, prNumber, err)
	}

	switch toInt64(res) {
	case 1, 0:
		return nil
	default:
		return fmt.Errorf("lock: releasing lock for %q (PR #%d): %w", projectKey, prNumber, ErrLockedByOtherPR)
	}
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
		// The third site of the same expression the two gates above
		// carried, reporting the recorded fact instead of the artifact's
		// length so this field agrees with GetPlan.
		//
		// Nothing reads it today — both callers of GetLockStatus use only
		// Locked and PRNumber, and the comment renderer decides what to
		// offer from the ProjectResult. It is corrected because a field
		// that reports something false is a trap for the first caller that
		// does read it, not because any reader is currently misled.
		HasPlan:     data.HasPlan,
		PlanSummary: data.PlanSummary,
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
