package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/redis/go-redis/v9"

	"github.com/ivanvc/turnip/internal/github"
)

// doneChannel is where result.go/sweep.go publish a finalized
// ProjectResult, and where a Target's own goroutine (execute.go) waits
// for it. This is a Redis Pub/Sub channel, not an in-process Go channel,
// because per Requirement 12 the replica that finalizes a given Operation
// may not be the replica whose goroutine is waiting — an in-process
// channel is only visible within the one replica that created it.
func doneChannel(operationID string) string {
	return "operation-done:" + operationID
}

// publishDone announces operationID's final outcome. Called as the very
// last step of finalizing an Operation (after the Operation Record has
// already been deleted), by whichever replica processed it.
func publishDone(ctx context.Context, client *redis.Client, operationID string, result github.ProjectResult) error {
	payload, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("orchestrator: encoding done notification for %q: %w", operationID, err)
	}
	if err := client.Publish(ctx, doneChannel(operationID), payload).Err(); err != nil {
		return fmt.Errorf("orchestrator: publishing done notification for %q: %w", operationID, err)
	}
	return nil
}

// waitForDone subscribes to operationID's done channel and blocks until a
// result is published or ctx is done. The subscription must be
// established before there is any chance publishDone could fire for this
// operation ID — Redis Pub/Sub has no replay, so a publish before a
// subscriber exists is simply lost. execute.go is responsible for that
// ordering (subscribing before the Runner Job that could trigger a
// publish is created).
func waitForDone(ctx context.Context, client *redis.Client, operationID string) (github.ProjectResult, error) {
	sub := client.Subscribe(ctx, doneChannel(operationID))
	defer func() { _ = sub.Close() }()

	select {
	case msg, ok := <-sub.Channel():
		if !ok {
			return github.ProjectResult{}, fmt.Errorf("orchestrator: done channel for %q closed unexpectedly", operationID)
		}
		var result github.ProjectResult
		if err := json.Unmarshal([]byte(msg.Payload), &result); err != nil {
			return github.ProjectResult{}, fmt.Errorf("orchestrator: decoding done notification for %q: %w", operationID, err)
		}
		return result, nil
	case <-ctx.Done():
		return github.ProjectResult{}, ctx.Err()
	}
}
