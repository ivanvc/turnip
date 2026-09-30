package orchestrator

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivanvc/turnip/internal/github"
	"github.com/ivanvc/turnip/internal/plugin"
	"github.com/ivanvc/turnip/internal/rpc"
)

// Slice 47 checkpoint (tasks.md 4.1): from a Runner result, through the
// real HandleResult path, to what a reader of the pull request actually
// sees: the posted comment body (github.BuildConsolidatedComment, the
// same renderer the aggregator hands a comment client) and the
// check-run update HandleResult itself sends. Everything upstream
// (the parser, ChangeText, Unreported, the Server wiring in result.go)
// is exercised elsewhere; this file is only about the wiring reaching
// the reader, which is the bug Slice 47 fixes.

// TestHandleResult_MutationChangeSummary_Counted is Requirement 4.3: a
// successful apply carrying Changes{Add:0, Change:3, Destroy:0} renders
// "+0 ~3 -0" in both the comment's summary line and the Project_Check
// Title, through a Plugin that reports its counts (testRegistry's
// default fakePlugin, MutationChangesReported always true).
func TestHandleResult_MutationChangeSummary_Counted(t *testing.T) {
	o, client := testResultOrchestrator(t, &fakeLockManager{})
	createTestRecord(t, o, "op-apply-counted", "apply")

	got := publishedResult(t, o, "op-apply-counted", rpc.OperationResult{
		Success: true,
		Changes: rpc.ChangeSummary{Add: 0, Change: 3, Destroy: 0},
	})

	require.False(t, got.Changes.Unreported)
	assert.Equal(t, "+0 ~3 -0", client.updatedCheckRun.Title, "Project_Check Title")
	assert.Equal(t, "add: 0, change: 3, destroy: 0", client.updatedCheckRun.Summary, "Project_Check Summary")

	bodies := github.BuildConsolidatedComment([]github.ProjectResult{got})
	require.Len(t, bodies, 1)
	assert.Contains(t, bodies[0], "+0 ~3 -0", "comment summary line")
	assert.NotContains(t, bodies[0], "no changes")
}

// unreportingHelmfile wraps the shared fakePlugin (target_test.go) to
// answer false from MutationChangesReported, the one thing no existing
// fake needs to vary (tasks.md 1.3 keeps every other fake answering
// true, which is why the override lives here rather than on fakePlugin
// itself).
type unreportingHelmfile struct {
	*fakePlugin
}

func (unreportingHelmfile) MutationChangesReported(string) bool { return false }

// assertNoCounts fails when a check-run Summary for an unreported success
// reads as "no changes" or shows counts (Requirement 5.3).
func assertNoCounts(t *testing.T, summary string) {
	t.Helper()
	assert.Equal(t, "Succeeded.", summary, "Project_Check Summary")
	assert.NotContains(t, summary, "No changes")
	assert.NotContains(t, summary, "change:")
}

// TestHandleResult_MutationChangeSummary_Unreported drives the same path
// with a Plugin whose MutationChangesReported returns false (tasks.md
// 1.3). A successful apply renders "succeeded" in the summary line, the
// headline, the Title and the Summary, and never "no changes"; the
// plan from the same Plugin still renders its counts; a failed apply
// still renders failed.
func TestHandleResult_MutationChangeSummary_Unreported(t *testing.T) {
	plugins := PluginRegistry{
		"helmfile": unreportingHelmfile{&fakePlugin{
			name: "helmfile", operations: []string{"diff", "apply", "sync"},
			planOperation: "diff", applyOperation: "apply", actsWithoutChanges: true,
		}},
	}

	t.Run("successful apply renders succeeded, never no changes", func(t *testing.T) {
		o, client := testResultOrchestrator(t, &fakeLockManager{})
		o.plugins = plugins
		createTestRecord(t, o, "op-apply-unreported", "apply")

		got := publishedResult(t, o, "op-apply-unreported", rpc.OperationResult{
			Success: true,
			Changes: rpc.ChangeSummary{Add: 0, Change: 3, Destroy: 0},
		})

		require.True(t, got.Changes.Unreported)
		assert.Equal(t, "succeeded", client.updatedCheckRun.Title, "Project_Check Title")
		assertNoCounts(t, client.updatedCheckRun.Summary)

		bodies := github.BuildConsolidatedComment([]github.ProjectResult{got})
		require.Len(t, bodies, 1)
		body := bodies[0]

		assert.NotContains(t, body, "no changes", "an unreported success must never render as no changes")
		assert.Contains(t, body, "Succeeded in", "headline")
		assert.Contains(t, body, "succeeded", "summary line detail")
	})

	t.Run("plan from the same plugin still renders its counts", func(t *testing.T) {
		o, client := testResultOrchestrator(t, &fakeLockManager{})
		o.plugins = plugins
		createTestRecord(t, o, "op-plan-unreported-plugin", "diff")

		got := publishedResult(t, o, "op-plan-unreported-plugin", rpc.OperationResult{
			Success: true,
			Changes: rpc.ChangeSummary{Add: 0, Change: 3, Destroy: 0},
		})

		require.False(t, got.Changes.Unreported, "Unreported is decided for Mutating_Operations only")
		assert.Equal(t, "+0 ~3 -0", client.updatedCheckRun.Title)
		assert.Equal(t, "add: 0, change: 3, destroy: 0", client.updatedCheckRun.Summary)

		bodies := github.BuildConsolidatedComment([]github.ProjectResult{got})
		require.Len(t, bodies, 1)
		assert.Contains(t, bodies[0], "+0 ~3 -0")
		assert.NotContains(t, bodies[0], "no changes")
	})

	t.Run("failed apply still renders failed", func(t *testing.T) {
		o, client := testResultOrchestrator(t, &fakeLockManager{})
		o.plugins = plugins
		createTestRecord(t, o, "op-failed-apply-unreported", "apply")

		got := publishedResult(t, o, "op-failed-apply-unreported", rpc.OperationResult{
			Success:         false,
			Output:          "helmfile apply exited 1",
			ExitCode:        1,
			FailureCategory: rpc.FailureToolExited,
		})

		require.False(t, got.Success)
		assert.False(t, got.Changes.Unreported, "unreported is only decided for successful mutations")
		assert.Equal(t, "helmfile exited 1", client.updatedCheckRun.Title)
		assert.Contains(t, client.updatedCheckRun.Summary, "failed")

		bodies := github.BuildConsolidatedComment([]github.ProjectResult{got})
		require.Len(t, bodies, 1)
		body := bodies[0]

		assert.Contains(t, body, "failed")
		assert.NotContains(t, body, "no changes")
		assert.NotContains(t, body, "succeeded", "a failure must not be worded as a success")
	})
}

// TestHandleResult_MutationChangeSummary_ZeroIsStillNoChanges is the
// control: a Plugin that does report its counts (the default fake, as
// in testRegistry) and an apply that genuinely changed nothing still
// renders "no changes". Unreported is a distinct signal from a zero
// count (Requirement 5.3's rationale), and this checkpoint would not
// catch a renderer that collapsed the two back together.
func TestHandleResult_MutationChangeSummary_ZeroIsStillNoChanges(t *testing.T) {
	o, client := testResultOrchestrator(t, &fakeLockManager{})
	createTestRecord(t, o, "op-apply-zero", "apply")

	got := publishedResult(t, o, "op-apply-zero", rpc.OperationResult{Success: true})

	require.False(t, got.Changes.Unreported)
	assert.Equal(t, "no changes", client.updatedCheckRun.Title)
	assert.Equal(t, "No changes.", client.updatedCheckRun.Summary)

	bodies := github.BuildConsolidatedComment([]github.ProjectResult{got})
	require.Len(t, bodies, 1)
	assert.Contains(t, bodies[0], "no changes")
}

// TestHandleResult_OlderHelmfileRendersSucceeded is Requirement 1.7 end
// to end, through the real Helmfile Plugin rather than a fake: an apply
// whose transcript carries the older "UPDATED RELEASES:" banner (which
// the Runner's parser does not read, so it arrives with zero counts)
// renders "succeeded" in the Title, the Summary, the headline and the
// summary line, never "no changes". A regression in how the Server hands
// result.Output to the Plugin fails here.
func TestHandleResult_OlderHelmfileRendersSucceeded(t *testing.T) {
	o, client := testResultOrchestrator(t, &fakeLockManager{})
	o.plugins = PluginRegistry{"helmfile": plugin.NewHelmfilePlugin()}
	createTestRecord(t, o, "op-apply-older-helmfile", "apply")

	olderTranscript := "@@ turnip: a, helmfile v1.1.0 @@\n" +
		"@@ turnip: helmfile apply @@\n" +
		"Upgrading release=web, chart=example.com/web\n" +
		"\n" +
		"UPDATED RELEASES:\n" +
		"NAME   CHART             VERSION   DURATION\n" +
		"web    example.com/web   1.2.3     2s\n" +
		"\n" +
		"@@ turnip: exit 0 in 3s @@"

	got := publishedResult(t, o, "op-apply-older-helmfile", rpc.OperationResult{
		Success: true,
		Output:  olderTranscript,
	})

	require.True(t, got.Changes.Unreported)
	assert.Equal(t, "succeeded", client.updatedCheckRun.Title, "Project_Check Title")
	assertNoCounts(t, client.updatedCheckRun.Summary)

	bodies := github.BuildConsolidatedComment([]github.ProjectResult{got})
	require.Len(t, bodies, 1)
	assert.Contains(t, bodies[0], "Succeeded in", "headline")
	assert.NotContains(t, bodies[0], "no changes")
	assert.NotContains(t, bodies[0], "No changes")
}
