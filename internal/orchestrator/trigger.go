package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/ivanvc/turnip/internal/github"
)

// triggerTTL is how long a trigger lives after its latest command began.
// It is an hour past the 24 hours the sweep gives a started Operation, so
// the trigger is still there when the sweep abandons the last Operation.
const triggerTTL = 25 * time.Hour

// triggerMeta is everything a trigger needs that never changes after it
// is created: the pull request, the selection notices and the resolved
// commands. Every command is already selected and authorized by then, so
// whichever replica carries the trigger forward dispatches Targets as
// they are.
type triggerMeta struct {
	Repo           github.Repository `json:"repo"`
	InstallationID int64             `json:"installation_id"`
	PRNumber       int               `json:"pr_number"`
	HeadSHA        string            `json:"head_sha"`
	BaseRef        string            `json:"base_ref"`
	HeadRef        string            `json:"head_ref"`
	Notices        []string          `json:"notices,omitempty"`
	Commands       [][]Target        `json:"commands"`
}

// triggerState is a trigger read back from Redis.
type triggerState struct {
	Meta triggerMeta
	// Command is the index of the command now running.
	Command int
	// Results are the Projects' results in the order they were added.
	Results []github.ProjectResult
}

func triggerKey(triggerID string) string {
	return "trigger:" + triggerID
}

// pendingField names the hash field that is present while a Project of a
// command has not completed. The command's index is part of the name
// because one Project can be in two commands of a comment.
func pendingField(command int, project string) string {
	return "pending:" + strconv.Itoa(command) + ":" + project
}

// completedField names the hash field that records, once a Project of a
// command has completed, the token of the completion that did it and the
// pending count that completion was answered with.
func completedField(command int, project string) string {
	return "completed:" + strconv.Itoa(command) + ":" + project
}

// completeTriggerScript adds one Project's result to its trigger and
// reports how many Projects of the command are still pending. Deleting a
// named field succeeds once, so a completion delivered twice adds nothing.
//
// A caller sends the same token on every try of one completion. When the
// pending field is already gone and the completion that removed it had
// this token, the script answers what it answered then: a try whose answer
// was lost on the way back, by the connection or by the Redis client
// trying again on its own, learns whether it finished the command. Any
// other token is answered -1.
//
// KEYS[1] = trigger:{id}
// ARGV[1] = pending field, ARGV[2] = command index, ARGV[3] = result JSON,
// ARGV[4] = completed field, ARGV[5] = the completion's token
// Returns -1 when the Project was already completed by another completion
// or the trigger is gone, otherwise how many Projects of the command are
// still pending.
const completeTriggerScript = `
if redis.call('HDEL', KEYS[1], ARGV[1]) == 0 then
    local prior = redis.call('HGET', KEYS[1], ARGV[4])
    if prior then
        local sep = string.find(prior, ':', 1, true)
        if sep and string.sub(prior, 1, sep - 1) == ARGV[5] then
            return tonumber(string.sub(prior, sep + 1))
        end
    end
    return -1
end
local n = redis.call('HINCRBY', KEYS[1], 'results', 1)
redis.call('HSET', KEYS[1], 'result:' .. n, ARGV[3])
local pending = 0
for _, field in ipairs(redis.call('HKEYS', KEYS[1])) do
    if string.sub(field, 1, #ARGV[2] + 9) == 'pending:' .. ARGV[2] .. ':' then
        pending = pending + 1
    end
end
redis.call('HSET', KEYS[1], ARGV[4], ARGV[5] .. ':' .. pending)
return pending
`

// triggerStore persists triggers in Redis, one hash per trigger. The key
// is one hash on purpose: results carry up to 1 MiB of output each, so a
// script moves a result in as an opaque string rather than decoding the
// whole trigger.
type triggerStore struct {
	client *redis.Client
}

func newTriggerStore(client *redis.Client) *triggerStore {
	return &triggerStore{client: client}
}

// Create writes the trigger with the results of the Projects refused during
// selection, which are the first results added. No command has begun yet.
func (s *triggerStore) Create(ctx context.Context, triggerID string, meta triggerMeta, refused []github.ProjectResult) error {
	metaJSON, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("orchestrator: marshaling trigger %q: %w", triggerID, err)
	}
	fields := map[string]any{
		"meta":    string(metaJSON),
		"command": 0,
		"results": len(refused),
	}
	for i, r := range refused {
		payload, err := json.Marshal(r)
		if err != nil {
			return fmt.Errorf("orchestrator: marshaling trigger %q result: %w", triggerID, err)
		}
		fields["result:"+strconv.Itoa(i+1)] = string(payload)
	}
	key := triggerKey(triggerID)
	pipe := s.client.TxPipeline()
	pipe.Del(ctx, key)
	pipe.HSet(ctx, key, fields)
	pipe.Expire(ctx, key, triggerTTL)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("orchestrator: creating trigger %q: %w", triggerID, err)
	}
	return nil
}

// BeginCommand records that command is now running with one pending field
// per Project, and sets the expiry again. Every pending field is set
// before any of the command's Operations is dispatched.
func (s *triggerStore) BeginCommand(ctx context.Context, triggerID string, command int, projects []string) error {
	key := triggerKey(triggerID)
	fields := map[string]any{"command": command}
	for _, p := range projects {
		fields[pendingField(command, p)] = "1"
	}
	pipe := s.client.TxPipeline()
	pipe.HSet(ctx, key, fields)
	pipe.Expire(ctx, key, triggerTTL)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("orchestrator: beginning command %d of trigger %q: %w", command, triggerID, err)
	}
	return nil
}

// Complete adds project's result for command to the trigger. It returns
// how many Projects of the command are still pending, so exactly one
// completion is returned 0, and -1 when another completion already
// completed the Project or the trigger is gone. token identifies the
// completion: pass the same one on every try, so a try after an answer
// that was lost is given that answer again. It must not contain ':'.
func (s *triggerStore) Complete(ctx context.Context, triggerID string, command int, project, token string, result github.ProjectResult) (int, error) {
	payload, err := json.Marshal(result)
	if err != nil {
		return 0, fmt.Errorf("orchestrator: marshaling trigger %q result: %w", triggerID, err)
	}
	res, err := s.client.Eval(ctx, completeTriggerScript, []string{triggerKey(triggerID)},
		pendingField(command, project), strconv.Itoa(command), string(payload),
		completedField(command, project), token).Result()
	if err != nil {
		return 0, fmt.Errorf("orchestrator: completing %q in command %d of trigger %q: %w", project, command, triggerID, err)
	}
	return int(toInt64(res)), nil
}

// Get reads a trigger back. It returns nil when the trigger is gone.
func (s *triggerStore) Get(ctx context.Context, triggerID string) (*triggerState, error) {
	fields, err := s.client.HGetAll(ctx, triggerKey(triggerID)).Result()
	if err != nil {
		return nil, fmt.Errorf("orchestrator: reading trigger %q: %w", triggerID, err)
	}
	if len(fields) == 0 {
		return nil, nil
	}
	st := &triggerState{}
	if err := json.Unmarshal([]byte(fields["meta"]), &st.Meta); err != nil {
		return nil, fmt.Errorf("orchestrator: decoding trigger %q: %w", triggerID, err)
	}
	if st.Command, err = strconv.Atoi(fields["command"]); err != nil {
		return nil, fmt.Errorf("orchestrator: decoding trigger %q command: %w", triggerID, err)
	}
	count, err := strconv.Atoi(fields["results"])
	if err != nil {
		return nil, fmt.Errorf("orchestrator: decoding trigger %q result count: %w", triggerID, err)
	}
	for i := 1; i <= count; i++ {
		var r github.ProjectResult
		if err := json.Unmarshal([]byte(fields["result:"+strconv.Itoa(i)]), &r); err != nil {
			return nil, fmt.Errorf("orchestrator: decoding trigger %q result %d: %w", triggerID, i, err)
		}
		st.Results = append(st.Results, r)
	}
	return st, nil
}

// Delete removes the trigger, once its comment is posted.
func (s *triggerStore) Delete(ctx context.Context, triggerID string) error {
	if err := s.client.Del(ctx, triggerKey(triggerID)).Err(); err != nil {
		return fmt.Errorf("orchestrator: deleting trigger %q: %w", triggerID, err)
	}
	return nil
}
