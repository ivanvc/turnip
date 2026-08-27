package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// planCommentTTL is a safety net for PlanCommentRecord — sized in days,
// not hours like operationTTL, because this record's legitimate lifetime
// is "as long as the PR stays open" (potentially months), not one Job's
// few-minute-to-hour execution window. Reset on every write (see Set), so
// an actively-worked PR's record never approaches expiry; it only fires
// if Requirement 11.7's close-triggered delete is itself missed.
const planCommentTTL = 90 * 24 * time.Hour

// PlanCommentRecord tracks the GraphQL node ID(s) of the most recently
// posted plan comment(s) for one PR — used only to know which comment(s)
// to mark outdated the next time a plan completes (Requirement 10.3).
// Has no apply-side equivalent: apply comments are never looked up again
// once posted (Requirement 10.7).
type PlanCommentRecord struct {
	NodeIDs []string `json:"node_ids"`
}

func planCommentKey(owner, repo string, prNumber int) string {
	return fmt.Sprintf("plan-comment:%s/%s#%d", owner, repo, prNumber)
}

// Get returns the PR's PlanCommentRecord, or (nil, nil) if none exists —
// missing is not an error, mirroring internal/lock's convention.
func (s *recordStore) GetPlanCommentRecord(ctx context.Context, owner, repo string, prNumber int) (*PlanCommentRecord, error) {
	raw, err := s.client.Get(ctx, planCommentKey(owner, repo, prNumber)).Bytes()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("orchestrator: reading plan comment record for %s/%s#%d: %w", owner, repo, prNumber, err)
	}
	var rec PlanCommentRecord
	if err := json.Unmarshal(raw, &rec); err != nil {
		return nil, fmt.Errorf("orchestrator: decoding plan comment record for %s/%s#%d: %w", owner, repo, prNumber, err)
	}
	return &rec, nil
}

// SetPlanCommentRecord replaces the PR's PlanCommentRecord with nodeIDs,
// resetting the TTL countdown (not just setting it once on first write).
func (s *recordStore) SetPlanCommentRecord(ctx context.Context, owner, repo string, prNumber int, nodeIDs []string) error {
	payload, err := json.Marshal(PlanCommentRecord{NodeIDs: nodeIDs})
	if err != nil {
		return fmt.Errorf("orchestrator: marshaling plan comment record for %s/%s#%d: %w", owner, repo, prNumber, err)
	}
	if err := s.client.Set(ctx, planCommentKey(owner, repo, prNumber), payload, planCommentTTL).Err(); err != nil {
		return fmt.Errorf("orchestrator: writing plan comment record for %s/%s#%d: %w", owner, repo, prNumber, err)
	}
	return nil
}

// DeletePlanCommentRecord removes the PR's PlanCommentRecord, if any —
// the primary cleanup path (Requirement 11.7), called when the PR
// closes; the TTL above is only a safety net for when this is missed.
func (s *recordStore) DeletePlanCommentRecord(ctx context.Context, owner, repo string, prNumber int) error {
	if err := s.client.Del(ctx, planCommentKey(owner, repo, prNumber)).Err(); err != nil {
		return fmt.Errorf("orchestrator: deleting plan comment record for %s/%s#%d: %w", owner, repo, prNumber, err)
	}
	return nil
}
