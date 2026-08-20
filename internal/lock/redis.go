package lock

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/ivanvc/turnip/internal/plugin"
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

func (m *RedisLockManager) StorePlanData(ctx context.Context, projectKey string, prNumber int, planData []byte, summary plugin.ChangeSummary) error {
	existing, err := m.getLockData(ctx, projectKey)
	if err != nil {
		return err
	}
	if existing == nil {
		return fmt.Errorf("lock: storing plan data for %q (PR #%d): %w", projectKey, prNumber, ErrNoLock)
	}

	existing.PlanData = planData
	existing.PlanSummary = summary
	payload, err := json.Marshal(existing)
	if err != nil {
		return fmt.Errorf("lock: marshaling plan data for %q: %w", projectKey, err)
	}

	res, err := m.client.Eval(ctx, compareAndMutateScript, []string{lockKey(projectKey)}, strconv.Itoa(prNumber), "set", payload).Result()
	if err != nil {
		return fmt.Errorf("lock: storing plan data for %q (PR #%d): %w", projectKey, prNumber, err)
	}

	switch toInt64(res) {
	case 1:
		return nil
	case 0:
		return fmt.Errorf("lock: storing plan data for %q (PR #%d): %w", projectKey, prNumber, ErrNoLock)
	default:
		return fmt.Errorf("lock: storing plan data for %q (PR #%d): %w", projectKey, prNumber, ErrLockedByOtherPR)
	}
}

func (m *RedisLockManager) GetPlanData(ctx context.Context, projectKey string, prNumber int) ([]byte, plugin.ChangeSummary, error) {
	data, err := m.getLockData(ctx, projectKey)
	if err != nil {
		return nil, plugin.ChangeSummary{}, err
	}
	if data == nil {
		return nil, plugin.ChangeSummary{}, fmt.Errorf("lock: getting plan data for %q (PR #%d): %w", projectKey, prNumber, ErrNoLock)
	}
	if data.PRNumber != prNumber {
		return nil, plugin.ChangeSummary{}, fmt.Errorf("lock: getting plan data for %q (PR #%d): %w", projectKey, prNumber, ErrLockedByOtherPR)
	}
	if len(data.PlanData) == 0 {
		return nil, plugin.ChangeSummary{}, fmt.Errorf("lock: getting plan data for %q (PR #%d): %w", projectKey, prNumber, ErrNoPlanData)
	}

	return data.PlanData, data.PlanSummary, nil
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
		HasPlan:        len(data.PlanData) > 0,
		PlanSummary:    data.PlanSummary,
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
