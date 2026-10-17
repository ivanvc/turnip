package orchestrator

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivanvc/turnip/internal/github"
	"github.com/ivanvc/turnip/internal/lock"
	"github.com/ivanvc/turnip/internal/plugin"
	"github.com/ivanvc/turnip/internal/rpc"
)

// Feature: fix-apply-suggested-with-lock, tasks.md 2.1. The trailer's
// apply line reads Locked, which only HandleResult sets from the
// transition the Lock manager confirms. These tests take real results
// through HandleResult and render the section, so they show the trailer
// each case really gets rather than one a hand-built ProjectResult would.

const (
	applyLine  = "- Apply just this project: `/turnip apply helm-a`"
	replanLine = "- Re-plan it: `/turnip diff helm-a`"
	unlockLine = "- Release its lock: `/turnip unlock helm-a`"
)

// trailerLines returns the command lines of the section rendered for r.
func trailerLines(t *testing.T, r github.ProjectResult) []string {
	t.Helper()
	bodies := github.BuildConsolidatedComment([]github.ProjectResult{r}, 3)
	require.Len(t, bodies, 1)
	var lines []string
	for _, l := range strings.Split(bodies[0], "\n") {
		if strings.HasPrefix(l, "- Apply just") || strings.HasPrefix(l, "- Re-plan") || strings.HasPrefix(l, "- Release its lock") {
			lines = append(lines, l)
		}
	}
	return lines
}

func TestHandleResult_ApplyOfferFollowsTheLock(t *testing.T) {
	helmfile := PluginRegistry{"helmfile": plugin.NewHelmfilePlugin()}

	t.Run("diff with no changes keeps its Lock and offers the apply", func(t *testing.T) {
		o, _ := testResultOrchestrator(t, &fakeLockManager{})
		o.plugins = helmfile
		createTestRecord(t, o, "op-clean-diff", "diff")

		got := publishedResult(t, o, "op-clean-diff", rpc.OperationResult{Success: true, PlanData: []byte("plan")})

		require.True(t, got.Locked)
		assert.Equal(t, []string{applyLine, replanLine, unlockLine}, trailerLines(t, got))
	})

	t.Run("quiet diff reading succeeded offers the apply", func(t *testing.T) {
		o, _ := testResultOrchestrator(t, &fakeLockManager{})
		o.plugins = helmfile
		createTestRecord(t, o, "op-quiet-diff", "diff")

		got := publishedResult(t, o, "op-quiet-diff", rpc.OperationResult{
			Success:  true,
			PlanData: []byte("plan"),
			Output:   "@@ turnip: helmfile --no-color diff -q @@\n",
		})

		require.True(t, got.Changes.Unreported)
		require.True(t, got.Locked)
		assert.Contains(t, trailerLines(t, got), applyLine)
	})

	t.Run("successful apply with changes offers no apply", func(t *testing.T) {
		o, _ := testResultOrchestrator(t, &fakeLockManager{})
		o.plugins = helmfile
		createTestRecord(t, o, "op-apply", "apply")

		got := publishedResult(t, o, "op-apply", rpc.OperationResult{
			Success: true,
			Output:  "@@ turnip: helmfile --no-color apply @@\n",
			Changes: rpc.ChangeSummary{Add: 1, Change: 2},
		})

		assert.NotContains(t, trailerLines(t, got), applyLine)
	})

	t.Run("plan whose Lock another pull request took offers only the re-plan", func(t *testing.T) {
		o, _ := testResultOrchestrator(t, &fakeLockManager{
			applyFunc: func(context.Context, string, int, lock.Event, *lock.PlanRecord) (lock.Transition, error) {
				return lock.Transition{}, fmt.Errorf("lock: applying: %w", lock.ErrLockedByOtherPR)
			},
		})
		o.plugins = helmfile
		createTestRecord(t, o, "op-taken", "diff")

		got := publishedResult(t, o, "op-taken", rpc.OperationResult{
			Success:  true,
			PlanData: []byte("plan"),
			Changes:  rpc.ChangeSummary{Change: 1},
		})

		require.False(t, got.Locked, "another pull request holds the Lock")
		assert.Equal(t, []string{replanLine}, trailerLines(t, got))
	})

	t.Run("clean plan of a tool that does not act without changes offers only the re-plan", func(t *testing.T) {
		o, _ := testResultOrchestrator(t, &fakeLockManager{})
		o.plugins = PluginRegistry{"helmfile": &fakePlugin{
			name: "helmfile", operations: []string{"diff", "apply"},
			planOperation: "diff", applyOperation: "apply", actsWithoutChanges: false,
		}}
		createTestRecord(t, o, "op-released", "diff")

		got := publishedResult(t, o, "op-released", rpc.OperationResult{Success: true})

		require.False(t, got.Locked, "the clean plan's Lock is released")
		assert.Equal(t, []string{replanLine}, trailerLines(t, got))
	})
}
