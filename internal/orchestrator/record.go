package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/ivanvc/turnip/internal/config"
)

// operationTTL is a leak-prevention safety net, not a correctness
// mechanism: an Operation Record is deleted explicitly once finalized
// (result.go, sweep.go); this only guards against a Server crash between
// finalizing and deleting.
const operationTTL = 24 * time.Hour

// OperationRecord is the per-Operation context persisted in Redis so any
// Server replica's gRPC handler or sweep tick can act on an Operation ID
// it did not itself create the Job for.
type OperationRecord struct {
	OperationID    string         `json:"operation_id"`
	ProjectKey     string         `json:"project_key"`
	Project        config.Project `json:"project"`
	Owner          string         `json:"owner"`
	Repo           string         `json:"repo"`
	InstallationID int64          `json:"installation_id"`
	PRNumber       int            `json:"pr_number"`
	PRURL          string         `json:"pr_url"`
	HeadSHA        string         `json:"head_sha"`
	Operation      string         `json:"operation"`
	IsApply        bool           `json:"is_apply"`
	ExtraArgs      []string       `json:"extra_args,omitempty"`
	PlanData       []byte         `json:"plan_data,omitempty"`
	TriggeredBy    string         `json:"triggered_by"`
	CheckRunID     int64          `json:"check_run_id,omitempty"`
	JobName        string         `json:"job_name,omitempty"`
	StartDeadline  int64          `json:"start_deadline"`
	Started        bool           `json:"started"`
	Finalized      bool           `json:"finalized"`
	CreatedAt      time.Time      `json:"created_at"`
}

func operationKey(operationID string) string {
	return "operation:" + operationID
}

// recordStore persists OperationRecords in Redis. Mirrors
// internal/lock/redis.go's pattern: GET, cjson.decode, check, mutate, SET
// ... KEEPTTL, all inside one Lua script so Redis's single-threaded
// script execution provides atomicity without WATCH/MULTI.
type recordStore struct {
	client *redis.Client
}

func newRecordStore(client *redis.Client) *recordStore {
	return &recordStore{client: client}
}

// Create persists rec, setting the leak-prevention TTL.
func (s *recordStore) Create(ctx context.Context, rec *OperationRecord) error {
	payload, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("orchestrator: marshaling operation record %q: %w", rec.OperationID, err)
	}
	if err := s.client.Set(ctx, operationKey(rec.OperationID), payload, operationTTL).Err(); err != nil {
		return fmt.Errorf("orchestrator: creating operation record %q: %w", rec.OperationID, err)
	}
	return nil
}

// SetJobName records the created Job's name. Single-writer (the goroutine
// that just created the record, before any concurrent reader could exist)
// — a plain GET/mutate/SET is safe, no CAS needed.
func (s *recordStore) SetJobName(ctx context.Context, operationID, jobName string) error {
	return s.mutate(ctx, operationID, func(rec *OperationRecord) { rec.JobName = jobName })
}

// SetCheckRunID records the created check run's ID. Same single-writer
// reasoning as SetJobName.
func (s *recordStore) SetCheckRunID(ctx context.Context, operationID string, checkRunID int64) error {
	return s.mutate(ctx, operationID, func(rec *OperationRecord) { rec.CheckRunID = checkRunID })
}

func (s *recordStore) mutate(ctx context.Context, operationID string, fn func(*OperationRecord)) error {
	rec, err := s.get(ctx, operationID)
	if err != nil {
		return err
	}
	if rec == nil {
		return fmt.Errorf("orchestrator: operation record %q not found", operationID)
	}
	fn(rec)
	payload, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("orchestrator: marshaling operation record %q: %w", operationID, err)
	}
	if err := s.client.Set(ctx, operationKey(operationID), payload, redis.KeepTTL).Err(); err != nil {
		return fmt.Errorf("orchestrator: updating operation record %q: %w", operationID, err)
	}
	return nil
}

func (s *recordStore) get(ctx context.Context, operationID string) (*OperationRecord, error) {
	raw, err := s.client.Get(ctx, operationKey(operationID)).Bytes()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("orchestrator: reading operation record %q: %w", operationID, err)
	}
	var rec OperationRecord
	if err := json.Unmarshal(raw, &rec); err != nil {
		return nil, fmt.Errorf("orchestrator: decoding operation record %q: %w", operationID, err)
	}
	return &rec, nil
}

// markStartedScript atomically marks an Operation Record started, unless
// it's already finalized or already gone (Requirement 8.2). Also reports
// whether this call performed the false→true transition, and the
// record's created_at when it did — Requirement 2.3's Runner Job Start
// Latency needs both to be observed exactly once per Operation, cluster-
// wide, across however many Server instances or reconnects call this.
//
// KEYS[1] = operation:{id}
// Returns {-1, ""} if the record is gone or already finalized, {1,
// created_at} if this call performed the transition, {0, ""} if it was
// already started.
const markStartedScript = `
local existing = redis.call('GET', KEYS[1])
if not existing then
    return {-1, ''}
end
local data = cjson.decode(existing)
if data.finalized then
    return {-1, ''}
end
if not data.started then
    data.started = true
    redis.call('SET', KEYS[1], cjson.encode(data), 'KEEPTTL')
    return {1, data.created_at}
end
return {0, ''}
`

// claimForFinalizationScript atomically claims an Operation Record for
// finalization — used by both a genuine result (mode "result") and the
// timeout sweep (mode "timeout"), so exactly one of them ever finalizes a
// given Operation (design.md's Decision 1).
//
// KEYS[1] = operation:{id}
// ARGV[1] = mode: "result" | "timeout"
// ARGV[2] = now (unix seconds; only read when mode == "timeout")
// Returns 1 if this call claimed it, 0 otherwise (gone, already
// finalized, or — timeout mode only — started since last checked, or the
// deadline hasn't passed yet).
const claimForFinalizationScript = `
local existing = redis.call('GET', KEYS[1])
if not existing then
    return 0
end
local data = cjson.decode(existing)
if data.finalized then
    return 0
end
if ARGV[1] == 'timeout' then
    if data.started then
        return 0
    end
    if tonumber(ARGV[2]) < data.start_deadline then
        return 0
    end
end
data.finalized = true
redis.call('SET', KEYS[1], cjson.encode(data), 'KEEPTTL')
return 1
`

// MarkStarted records that operationID has produced output (Requirement
// 8.2). A late call against an already-finalized or already-deleted
// record is silently accepted, not an error. justStarted is true only
// for the one caller, across every Server instance and every reconnect,
// whose call actually performed the false→true transition — everyone
// else gets false, so Requirement 2.3's Runner Job Start Latency
// histogram is observed exactly once per Operation.
func (s *recordStore) MarkStarted(ctx context.Context, operationID string) (justStarted bool, createdAt time.Time, err error) {
	res, err := s.client.Eval(ctx, markStartedScript, []string{operationKey(operationID)}).Result()
	if err != nil {
		return false, time.Time{}, fmt.Errorf("orchestrator: marking operation %q started: %w", operationID, err)
	}
	items, ok := res.([]any)
	if !ok || len(items) != 2 {
		return false, time.Time{}, fmt.Errorf("orchestrator: marking operation %q started: unexpected script reply %v", operationID, res)
	}
	if toInt64(items[0]) != 1 {
		return false, time.Time{}, nil
	}
	raw, _ := items[1].(string)
	created, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return false, time.Time{}, fmt.Errorf("orchestrator: marking operation %q started: parsing created_at %q: %w", operationID, raw, err)
	}
	return true, created, nil
}

// ClaimForResult atomically claims operationID for a genuine HandleResult
// call. claimed is false when the record is already gone or already
// finalized (Requirement 7.9) — the caller must treat that as a no-op,
// not an error.
func (s *recordStore) ClaimForResult(ctx context.Context, operationID string) (rec *OperationRecord, claimed bool, err error) {
	return s.claim(ctx, operationID, "result", time.Time{})
}

// ClaimForTimeout atomically claims operationID for the timeout sweep.
// claimed is false when the record is gone, already finalized, has
// already started, or its start deadline hasn't passed yet (Requirement
// 8.3) — none of these are errors.
func (s *recordStore) ClaimForTimeout(ctx context.Context, operationID string, now time.Time) (rec *OperationRecord, claimed bool, err error) {
	return s.claim(ctx, operationID, "timeout", now)
}

func (s *recordStore) claim(ctx context.Context, operationID, mode string, now time.Time) (*OperationRecord, bool, error) {
	// Read the record's content before attempting the claim. This is safe
	// even under a race: every field read here (Project, PR identity,
	// TriggeredBy, ...) is immutable after Create — only started/finalized
	// (and JobName/CheckRunID, both set once, before this record is ever
	// eligible to be claimed) ever change.
	rec, err := s.get(ctx, operationID)
	if err != nil {
		return nil, false, err
	}
	if rec == nil {
		return nil, false, nil
	}

	res, err := s.client.Eval(ctx, claimForFinalizationScript, []string{operationKey(operationID)}, mode, now.Unix()).Result()
	if err != nil {
		return nil, false, fmt.Errorf("orchestrator: claiming operation %q (%s): %w", operationID, mode, err)
	}
	if toInt64(res) != 1 {
		return nil, false, nil
	}
	return rec, true, nil
}

// Delete removes operationID's record, once fully finalized and every
// follow-up action has been attempted.
func (s *recordStore) Delete(ctx context.Context, operationID string) error {
	if err := s.client.Del(ctx, operationKey(operationID)).Err(); err != nil {
		return fmt.Errorf("orchestrator: deleting operation record %q: %w", operationID, err)
	}
	return nil
}

// ScanOperationKeys returns every "operation:*" key, paginating through
// the full keyspace rather than truncating (this repo's established
// convention — internal/github.GetModifiedFiles's pagination loop).
func (s *recordStore) ScanOperationKeys(ctx context.Context) ([]string, error) {
	var keys []string
	var cursor uint64
	for {
		batch, next, err := s.client.Scan(ctx, cursor, "operation:*", 0).Result()
		if err != nil {
			return nil, fmt.Errorf("orchestrator: scanning operation records: %w", err)
		}
		keys = append(keys, batch...)
		if next == 0 {
			break
		}
		cursor = next
	}
	return keys, nil
}

func toInt64(v any) int64 {
	n, _ := v.(int64)
	return n
}
