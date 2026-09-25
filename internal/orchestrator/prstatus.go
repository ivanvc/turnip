package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/ivanvc/turnip/internal/lock"
)

// prStatusTTL bounds a Pull_Request_Record's life. Sized like
// planCommentTTL, and for the same reason: a pull request can stay open
// for months, and the record must outlive every Operation on its commit.
// Refreshed on every write, so it only fires on commits nobody touches
// any more. There is no explicit delete: there is one record per pushed
// commit, and deleting them on close would need an index of them.
const prStatusTTL = 90 * 24 * time.Hour

// Outcome is what the Pull_Request_Record holds for one Project: the
// latest thing that happened to it on this commit (aggregate-check-run
// Requirement 4).
type Outcome string

const (
	OutcomeAwaitingApply  Outcome = "awaiting"
	OutcomeNothingToApply Outcome = "nothing"
	OutcomeNotPlanned     Outcome = "not_planned"
	OutcomeApplied        Outcome = "applied"
	OutcomeApplyFailed    Outcome = "apply_failed"
	// OutcomeRefused is a Configuration_Refusal: the Project asked for an
	// override this Server does not permit (check-run-refusals
	// Requirement 3.3). Only the author can fix it.
	OutcomeRefused Outcome = "refused"
)

// done reports whether the Project needs nothing further before merge.
func (o Outcome) done() bool {
	return o == OutcomeApplied || o == OutcomeNothingToApply
}

// mutating reports whether the Outcome came from a Mutating_Operation
// having run — what lets the Aggregate_Check appear before every plan is
// carried out (Requirement 6.1).
func (o Outcome) mutating() bool {
	return o == OutcomeApplied || o == OutcomeApplyFailed
}

// outcomeForEvent derives a Project's Outcome from the lock.Event its
// finished Operation produced. The event, not the Lock transition it
// caused: what happened to the Operation is known even when the Lock was
// already gone or could not be updated.
//
// The applicable/nothing-to-apply split is lockEventFor's, which is where
// a Plugin's ActsWithoutChanges is honored — so whether a no-change plan
// still needs an apply is decided per tool, in one place.
func outcomeForEvent(ev lock.Event) (Outcome, bool) {
	switch ev {
	case lock.EventPlanApplicable:
		return OutcomeAwaitingApply, true
	case lock.EventPlanNothingToApply:
		return OutcomeNothingToApply, true
	case lock.EventPlanFailed, lock.EventPlanTimedOut:
		return OutcomeNotPlanned, true
	case lock.EventMutatingSucceeded:
		return OutcomeApplied, true
	case lock.EventMutatingFailed, lock.EventMutatingTimedOut:
		return OutcomeApplyFailed, true
	default:
		return "", false
	}
}

// prRef identifies a Pull_Request_Record: one pull request at one head
// commit. Keying by commit is what makes a push start a fresh record
// without any code doing so, and lets a result that arrives after the push
// land on the commit it belongs to (design Decision 1).
type prRef struct {
	Owner    string
	Repo     string
	PRNumber int
	HeadSHA  string
}

func prStatusKey(ref prRef) string {
	return fmt.Sprintf("pr-status:%s/%s#%d@%s", ref.Owner, ref.Repo, ref.PRNumber, ref.HeadSHA)
}

// ProjectEntry is one Project's field in the record. Operation is carried
// so the summary can name the Project_Check. BlockedBy (on not_planned:
// the pull request holding the Lock, when known) and Setting (on refused:
// the override not permitted) are omitempty, so an entry written before
// they existed decodes unchanged.
type ProjectEntry struct {
	Outcome   Outcome `json:"outcome"`
	Operation string  `json:"operation,omitempty"`
	BlockedBy int     `json:"blocked_by,omitempty"`
	Setting   string  `json:"setting,omitempty"`
}

// Field names. Project fields and metadata fields have disjoint prefixes,
// so no Project name can collide with a metadata field.
const (
	prStatusProjectPrefix = "p:"
	// prStatusVersion is bumped by every write that changes the verdict,
	// and only by those — the publisher's own bookkeeping below does not
	// bump it, or every publish would look like a change and publish again.
	prStatusVersion  = "m:version"
	prStatusMutated  = "m:mutated"
	prStatusConfig   = "m:config"
	prStatusEmpty    = "m:empty"
	prStatusCheckRun = "m:check_run"
	// prStatusCheckDone records whether the check run named by
	// m:check_run was last published completed. A hint, not a truth: it
	// only decides whether to try updating that run or to create a new
	// one, and a wrong guess is caught by the update failing.
	prStatusCheckDone = "m:check_done"
)

// prStatus is a decoded Pull_Request_Record.
type prStatus struct {
	Projects      map[string]ProjectEntry
	Version       int64
	Mutated       bool
	ConfigInvalid bool
	Empty         bool
	CheckRunID    int64
	CheckDone     bool
}

// WriteOutcome sets one Project's entry, leaving every other Project's
// alone. No read-modify-write is involved — the latest Outcome replaces
// the previous one unconditionally — so two Operations of one pull
// request finishing on different Server instances cannot lose each
// other's writes (Requirement 3.6, design Decision 2).
func (s *recordStore) WriteOutcome(ctx context.Context, ref prRef, project string, entry ProjectEntry) error {
	payload, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("orchestrator: marshaling outcome for %s: %w", project, err)
	}
	fields := []any{prStatusProjectPrefix + project, payload}
	if entry.Outcome.mutating() {
		fields = append(fields, prStatusMutated, "1")
	}
	return s.writePRStatus(ctx, ref, true, fields...)
}

// MarkConfigInvalid records that the automatic plan found an invalid
// turnip.yaml on this commit (Requirement 7.1).
func (s *recordStore) MarkConfigInvalid(ctx context.Context, ref prRef) error {
	return s.writePRStatus(ctx, ref, true, prStatusConfig, "invalid")
}

// MarkEmpty records that the automatic plan matched no Project on this
// commit (Requirement 6.3).
func (s *recordStore) MarkEmpty(ctx context.Context, ref prRef) error {
	return s.writePRStatus(ctx, ref, true, prStatusEmpty, "1")
}

// SetAggregateCheckRun records which check run carries the verdict and
// whether it was left completed. Publisher bookkeeping, so it does not
// bump the version.
func (s *recordStore) SetAggregateCheckRun(ctx context.Context, ref prRef, checkRunID int64, completed bool) error {
	return s.writePRStatus(ctx, ref, false, prStatusCheckRun, strconv.FormatInt(checkRunID, 10), prStatusCheckDone, boolField(completed))
}

// SetAggregateCheckDone updates only the completed hint, after an update
// in place.
func (s *recordStore) SetAggregateCheckDone(ctx context.Context, ref prRef, completed bool) error {
	return s.writePRStatus(ctx, ref, false, prStatusCheckDone, boolField(completed))
}

func (s *recordStore) writePRStatus(ctx context.Context, ref prRef, bumpVersion bool, fields ...any) error {
	key := prStatusKey(ref)
	_, err := s.client.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.HSet(ctx, key, fields...)
		if bumpVersion {
			pipe.HIncrBy(ctx, key, prStatusVersion, 1)
		}
		pipe.Expire(ctx, key, prStatusTTL)
		return nil
	})
	if err != nil {
		return fmt.Errorf("orchestrator: writing pull request record %s: %w", key, err)
	}
	return nil
}

// ReadPRStatus reads the whole record in one command, so the verdict is
// computed from a consistent snapshot. A missing record decodes to an
// empty one.
func (s *recordStore) ReadPRStatus(ctx context.Context, ref prRef) (prStatus, error) {
	key := prStatusKey(ref)
	raw, err := s.client.HGetAll(ctx, key).Result()
	if err != nil {
		return prStatus{}, fmt.Errorf("orchestrator: reading pull request record %s: %w", key, err)
	}
	st := prStatus{Projects: map[string]ProjectEntry{}}
	for field, value := range raw {
		switch {
		case strings.HasPrefix(field, prStatusProjectPrefix):
			var entry ProjectEntry
			if err := json.Unmarshal([]byte(value), &entry); err != nil {
				return prStatus{}, fmt.Errorf("orchestrator: decoding %s in %s: %w", field, key, err)
			}
			st.Projects[strings.TrimPrefix(field, prStatusProjectPrefix)] = entry
		case field == prStatusVersion:
			st.Version, _ = strconv.ParseInt(value, 10, 64)
		case field == prStatusMutated:
			st.Mutated = value == "1"
		case field == prStatusConfig:
			st.ConfigInvalid = value == "invalid"
		case field == prStatusEmpty:
			st.Empty = value == "1"
		case field == prStatusCheckRun:
			st.CheckRunID, _ = strconv.ParseInt(value, 10, 64)
		case field == prStatusCheckDone:
			st.CheckDone = value == "1"
		}
	}
	return st, nil
}

func boolField(b bool) string {
	if b {
		return "1"
	}
	return "0"
}
