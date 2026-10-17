package orchestrator

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivanvc/turnip/internal/github"
	"github.com/ivanvc/turnip/internal/lock"
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
// default fakePlugin, ChangesReported always true).
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

	bodies := github.BuildConsolidatedComment([]github.ProjectResult{got}, 3)
	require.Len(t, bodies, 1)
	assert.Contains(t, bodies[0], "+0 ~3 -0", "comment summary line")
	assert.NotContains(t, bodies[0], "no changes")
}

// unreportingHelmfile wraps the shared fakePlugin (target_test.go) to
// answer false from ChangesReported, for every operation, the one thing no existing
// fake needs to vary (tasks.md 1.3 keeps every other fake answering
// true, which is why the override lives here rather than on fakePlugin
// itself).
type unreportingHelmfile struct {
	*fakePlugin
}

func (unreportingHelmfile) ChangesReported(string, string) bool { return false }

// assertNoCounts fails when a check-run Summary for an unreported success
// reads as "no changes" or shows counts (Requirement 5.3).
func assertNoCounts(t *testing.T, summary string) {
	t.Helper()
	assert.Equal(t, "Succeeded.", summary, "Project_Check Summary")
	assert.NotContains(t, summary, "No changes")
	assert.NotContains(t, summary, "change:")
}

// TestHandleResult_MutationChangeSummary_Unreported drives the same path
// with a Plugin whose ChangesReported returns false (tasks.md 1.3). A
// successful apply renders "succeeded" in the summary line, the
// headline, the Title and the Summary, and never "no changes"; so does
// the plan from the same Plugin, since HandleResult asks for the plan
// too (fix-helmfile-diff-removals); a failed apply still renders failed.
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

		bodies := github.BuildConsolidatedComment([]github.ProjectResult{got}, 3)
		require.Len(t, bodies, 1)
		body := bodies[0]

		assert.NotContains(t, body, "no changes", "an unreported success must never render as no changes")
		assert.Contains(t, body, "Succeeded in", "headline")
		assert.Contains(t, body, "succeeded", "summary line detail")
	})

	// Mutation check: asking ChangesReported for Mutating_Operations
	// only, as before fix-helmfile-diff-removals, renders "+0 ~3 -0".
	t.Run("plan from the same plugin renders succeeded too", func(t *testing.T) {
		o, client := testResultOrchestrator(t, &fakeLockManager{})
		o.plugins = plugins
		createTestRecord(t, o, "op-plan-unreported-plugin", "diff")

		got := publishedResult(t, o, "op-plan-unreported-plugin", rpc.OperationResult{
			Success: true,
			Changes: rpc.ChangeSummary{Add: 0, Change: 3, Destroy: 0},
		})

		require.True(t, got.Changes.Unreported, "Unreported is decided for every successful operation, the plan included")
		assert.Equal(t, "succeeded", client.updatedCheckRun.Title, "Project_Check Title")
		assertNoCounts(t, client.updatedCheckRun.Summary)

		bodies := github.BuildConsolidatedComment([]github.ProjectResult{got}, 3)
		require.Len(t, bodies, 1)
		assert.NotContains(t, bodies[0], "+0 ~3 -0")
		assert.NotContains(t, bodies[0], "no changes")
		assert.Contains(t, bodies[0], "Succeeded in", "headline")
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

		bodies := github.BuildConsolidatedComment([]github.ProjectResult{got}, 3)
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

	bodies := github.BuildConsolidatedComment([]github.ProjectResult{got}, 3)
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

	bodies := github.BuildConsolidatedComment([]github.ProjectResult{got}, 3)
	require.Len(t, bodies, 1)
	assert.Contains(t, bodies[0], "Succeeded in", "headline")
	assert.NotContains(t, bodies[0], "no changes")
	assert.NotContains(t, bodies[0], "No changes")
}

// TestHandleResult_QuietHelmfilePlanRendersSucceeded is the
// plugin-helmfile delta's "A quiet plan" through the real Helmfile
// Plugin: a successful diff whose recorded command line carries -q
// printed no "Affected releases are:" summary, so its zero destroy count
// cannot be trusted, and it renders "succeeded", never "no changes",
// even though it printed a diff and carries a changed count.
//
// Mutation check: skipping the plan in HandleResult's ChangesReported
// call, or answering true for a diff in the Helmfile Plugin, renders
// "+0 ~1 -0" here.
func TestHandleResult_QuietHelmfilePlanRendersSucceeded(t *testing.T) {
	o, client := testResultOrchestrator(t, &fakeLockManager{})
	o.plugins = PluginRegistry{"helmfile": plugin.NewHelmfilePlugin()}
	createTestRecord(t, o, "op-diff-quiet-helmfile", "diff")

	quietTranscript := "@@ turnip: a, helmfile v1.8.1 @@\n" +
		"@@ turnip: helmfile --no-color diff -q @@\n" +
		"Comparing release=web, chart=example/web\n" +
		"-  replicas: 1\n" +
		"+  replicas: 2\n" +
		"@@ turnip: exit 0 in 3s @@"

	got := publishedResult(t, o, "op-diff-quiet-helmfile", rpc.OperationResult{
		Success: true,
		Output:  quietTranscript,
		Changes: rpc.ChangeSummary{Change: 1},
	})

	require.True(t, got.Changes.Unreported)
	assert.Equal(t, "succeeded", client.updatedCheckRun.Title, "Project_Check Title")
	assertNoCounts(t, client.updatedCheckRun.Summary)

	bodies := github.BuildConsolidatedComment([]github.ProjectResult{got}, 3)
	require.Len(t, bodies, 1)
	assert.Contains(t, bodies[0], "Succeeded in", "headline")
	assert.NotContains(t, bodies[0], "no changes")
	assert.NotContains(t, bodies[0], "+0 ~1 -0")
}

// TestHandleResult_HelmfilePlanCounted is the control for the quiet
// plan: the same diff run without -q reports its counts.
func TestHandleResult_HelmfilePlanCounted(t *testing.T) {
	o, client := testResultOrchestrator(t, &fakeLockManager{})
	o.plugins = PluginRegistry{"helmfile": plugin.NewHelmfilePlugin()}
	createTestRecord(t, o, "op-diff-helmfile", "diff")

	got := publishedResult(t, o, "op-diff-helmfile", rpc.OperationResult{
		Success: true,
		Output:  "@@ turnip: helmfile --no-color diff @@\nComparing release=web, chart=example/web\n+  replicas: 2\n",
		Changes: rpc.ChangeSummary{Change: 1},
	})

	require.False(t, got.Changes.Unreported)
	assert.Equal(t, "+0 ~1 -0", client.updatedCheckRun.Title, "Project_Check Title")
}

// writeStandInHelmfile puts a "helmfile" shell script first on PATH that
// prints the diff of one release on standard output and, on standard
// error, what Helmfile v1.8.1 logs for a removal after the diffs: the
// "Affected releases are:" summary with one DELETED release. It exits 0,
// as `helmfile diff` does without --detailed-exitcode. When the script is
// given -q it prints no summary, since Helmfile logs it at info level.
func writeStandInHelmfile(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	script := `#!/bin/sh
quiet=
for arg in "$@"; do
  case "$arg" in -q|--quiet) quiet=1 ;; esac
done
echo "Comparing release=web, chart=example/web"
echo "-  replicas: 1"
echo "+  replicas: 2"
if [ -z "$quiet" ]; then
  echo "Affected releases are:" >&2
  echo "  web (example/web) DELETED" >&2
  echo >&2
fi
exit 0
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "helmfile"), []byte(script), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// TestHandleResult_HelmfileRemovalThroughTheRealPlugin is the checkpoint
// of fix-helmfile-diff-removals (tasks.md 3.1). The count is made on the
// Runner, "not reported" is decided on the Server, and the Lock moves on
// the Server from both, so only the real Helmfile Plugin running a
// stand-in `helmfile` that prints what v1.8.1 prints for a removal, with
// its result taken through HandleResult, shows what the pull request
// reads: "+0 ~1 -1" for a plan, "succeeded" for a quiet one, and the
// Lock kept with its plan either way.
func TestHandleResult_HelmfileRemovalThroughTheRealPlugin(t *testing.T) {
	for _, tc := range []struct {
		name      string
		extraArgs []string
		quiet     bool
	}{
		{"counts the removal", nil, false},
		{"quiet plan renders succeeded", []string{"-q"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			writeStandInHelmfile(t)
			p := plugin.NewHelmfilePlugin()
			res, err := p.Execute(context.Background(), "diff", plugin.ExecuteOptions{
				WorkingDir:  t.TempDir(),
				Tool:        "helmfile",
				ToolVersion: "v1.8.1",
				ExtraArgs:   tc.extraArgs,
			})
			require.NoError(t, err)
			require.Equal(t, 0, res.ExitCode)

			locks := &fakeLockManager{}
			o, client := testResultOrchestrator(t, locks)
			o.plugins = PluginRegistry{"helmfile": p}
			createTestRecord(t, o, "op-diff-removal", "diff")

			got := publishedResult(t, o, "op-diff-removal", rpc.OperationResult{
				Success:  true,
				Output:   res.Output,
				PlanData: res.PlanData,
				Changes: rpc.ChangeSummary{
					Add:     int32(res.ChangeSummary.Add),
					Change:  int32(res.ChangeSummary.Change),
					Destroy: int32(res.ChangeSummary.Destroy),
				},
			})

			bodies := github.BuildConsolidatedComment([]github.ProjectResult{got}, 3)
			require.Len(t, bodies, 1)

			if tc.quiet {
				require.True(t, got.Changes.Unreported)
				assert.Equal(t, "succeeded", client.updatedCheckRun.Title, "Project_Check Title")
				assertNoCounts(t, client.updatedCheckRun.Summary)
				assert.Contains(t, bodies[0], "Succeeded in", "headline")
				assert.NotContains(t, bodies[0], "no changes")
				assert.NotContains(t, bodies[0], "No changes")
			} else {
				require.False(t, got.Changes.Unreported)
				assert.Equal(t, "+0 ~1 -1", client.updatedCheckRun.Title, "Project_Check Title")
				assert.Contains(t, bodies[0], "+0 ~1 -1", "comment summary line")
				assert.NotContains(t, bodies[0], "no changes")
			}

			assert.True(t, got.Locked, "a plan that changes something keeps its Lock")
			assert.Equal(t, []lock.Event{lock.EventPlanApplicable}, locks.appliedEvents())
		})
	}
}
