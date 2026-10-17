package plugin

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordedCall struct {
	dir  string
	name string
	args []string
}

// fakeRunner records lines as the command's record. Fixtures state their
// interleaving explicitly, because the order of the two streams is part of
// what a Plugin is handed.
func fakeRunner(t *testing.T, calls *[]recordedCall, lines []Line, exitCode int, err error) commandRunner {
	t.Helper()
	return func(ctx context.Context, dir, name string, args []string, version string, rec *Record) (int, error) {
		*calls = append(*calls, recordedCall{dir: dir, name: name, args: args})
		for _, l := range lines {
			rec.Append(l.Stream, l.Text)
		}
		rec.Close()
		return exitCode, err
	}
}

func stdoutLine(text string) Line { return Line{Stream: Stdout, Text: text} }
func stderrLine(text string) Line { return Line{Stream: Stderr, Text: text} }
func turnipLine(text string) Line { return Line{Stream: Turnip, Text: text} }

func TestHelmfilePlugin_ExecuteInvokesCorrectCommand(t *testing.T) {
	tests := []struct {
		operation string
		wantArgs  []string
	}{
		{"diff", []string{"--no-color", "diff"}},
		{"apply", []string{"--no-color", "apply"}},
		{"sync", []string{"--no-color", "sync"}},
	}

	for _, tt := range tests {
		t.Run(tt.operation, func(t *testing.T) {
			var calls []recordedCall
			p := &HelmfilePlugin{run: fakeRunner(t, &calls, []Line{stdoutLine("out")}, 0, nil)}

			_, err := p.Execute(context.Background(), tt.operation, ExecuteOptions{WorkingDir: "/work"})
			require.NoError(t, err)
			require.Len(t, calls, 1)
			assert.Equal(t, "helmfile", calls[0].name)
			assert.Equal(t, "/work", calls[0].dir)
			assert.Equal(t, tt.wantArgs, calls[0].args)
		})
	}
}

func TestHelmfilePlugin_UnsupportedOperation(t *testing.T) {
	var calls []recordedCall
	p := &HelmfilePlugin{run: fakeRunner(t, &calls, nil, 0, nil)}

	result, err := p.Execute(context.Background(), "plan", ExecuteOptions{})
	assert.Nil(t, result)
	var unsupported *UnsupportedOperationError
	require.ErrorAs(t, err, &unsupported)
	assert.Empty(t, calls, "runner should not be invoked")
}

func TestHelmfilePlugin_EnvironmentFlag(t *testing.T) {
	var calls []recordedCall
	p := &HelmfilePlugin{run: fakeRunner(t, &calls, nil, 0, nil)}

	_, err := p.Execute(context.Background(), "diff", ExecuteOptions{
		Config: map[string]string{"environment": "staging"},
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"--no-color", "--environment", "staging", "diff"}, calls[0].args)
}

// A key under with: that Helmfile's Plugin does not know has no effect on
// the command line, a `color` key included: --no-color is turnip's, always.
func TestHelmfilePlugin_UnknownWithKeyChangesNothing(t *testing.T) {
	var calls []recordedCall
	p := &HelmfilePlugin{run: fakeRunner(t, &calls, nil, 0, nil)}

	_, err := p.Execute(context.Background(), "diff", ExecuteOptions{
		Config: map[string]string{"environment": "staging", "color": "blue"},
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"--no-color", "--environment", "staging", "diff"}, calls[0].args)
}

func TestHelmfilePlugin_NoEnvironmentFlagWhenAbsent(t *testing.T) {
	var calls []recordedCall
	p := &HelmfilePlugin{run: fakeRunner(t, &calls, nil, 0, nil)}

	_, err := p.Execute(context.Background(), "diff", ExecuteOptions{})
	require.NoError(t, err)
	assert.Equal(t, []string{"--no-color", "diff"}, calls[0].args)
}

func TestHelmfilePlugin_ExtraArgsAppended(t *testing.T) {
	var calls []recordedCall
	p := &HelmfilePlugin{run: fakeRunner(t, &calls, nil, 0, nil)}

	_, err := p.Execute(context.Background(), "diff", ExecuteOptions{
		Config:    map[string]string{"environment": "staging"},
		ExtraArgs: []string{"--quiet"},
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"--no-color", "--environment", "staging", "diff", "--quiet"}, calls[0].args)
}

func TestHelmfilePlugin_ChangeSummaryOnlyForDiff(t *testing.T) {
	diffOutput := []Line{stdoutLine("Comparing release=a, chart=charts/a"), stdoutLine("something changed")}

	for _, operation := range []string{"apply", "sync"} {
		t.Run(operation, func(t *testing.T) {
			var calls []recordedCall
			p := &HelmfilePlugin{run: fakeRunner(t, &calls, diffOutput, 0, nil)}

			result, err := p.Execute(context.Background(), operation, ExecuteOptions{})
			require.NoError(t, err)
			assert.Equal(t, ChangeSummary{}, result.ChangeSummary)
		})
	}

	t.Run("diff", func(t *testing.T) {
		var calls []recordedCall
		p := &HelmfilePlugin{run: fakeRunner(t, &calls, diffOutput, 0, nil)}

		result, err := p.Execute(context.Background(), "diff", ExecuteOptions{})
		require.NoError(t, err)
		assert.Equal(t, 1, result.ChangeSummary.Change)
	})
}

// The counts are read from the bytes the tool wrote, before anything
// replaces invalid UTF-8 for the Server.
func TestHelmfilePlugin_DiffWhoseOnlyBodyLineIsInvalidUTF8StillCountsTheRelease(t *testing.T) {
	var calls []recordedCall
	diffOutput := []Line{stdoutLine("Comparing release=a, chart=charts/a"), stdoutLine("name: caf\xe9")}
	p := &HelmfilePlugin{run: fakeRunner(t, &calls, diffOutput, 0, nil)}

	result, err := p.Execute(context.Background(), "diff", ExecuteOptions{})
	require.NoError(t, err)
	assert.Equal(t, 1, result.ChangeSummary.Change)
}

func TestHelmfilePlugin_PlanDataAlwaysNil(t *testing.T) {
	var calls []recordedCall
	p := &HelmfilePlugin{run: fakeRunner(t, &calls, []Line{stdoutLine("out")}, 0, nil)}

	result, err := p.Execute(context.Background(), "apply", ExecuteOptions{PlanData: []byte("ignored")})
	require.NoError(t, err)
	assert.Nil(t, result.PlanData)
}

func TestHelmfilePlugin_RunnerErrorPropagates(t *testing.T) {
	wantErr := errors.New("binary not found")
	var calls []recordedCall
	p := &HelmfilePlugin{run: fakeRunner(t, &calls, nil, -1, wantErr)}

	result, err := p.Execute(context.Background(), "diff", ExecuteOptions{})
	assert.Nil(t, result)
	assert.ErrorIs(t, err, wantErr)
}

// The command annotation is built from the arguments Execute passes, so it
// shows --no-color and the environment turnip supplied.
func TestHelmfilePlugin_TheAnnotationShowsTheCommandLineAsRun(t *testing.T) {
	var calls []recordedCall
	p := &HelmfilePlugin{run: fakeRunner(t, &calls, nil, 0, nil)}

	_, err := p.Execute(context.Background(), "diff", ExecuteOptions{
		Config:    map[string]string{"environment": "staging"},
		ExtraArgs: []string{"-l", "name=api"},
	})
	require.NoError(t, err)
	require.Len(t, calls, 1)

	assert.Equal(t, []string{
		"@@ turnip: infra/web, helmfile v1.7.4 @@",
		"@@ turnip: helmfile --no-color --environment staging diff -l name=api @@",
	}, transcriptHeader("infra/web", calls[0].name, calls[0].args, "v1.7.4"))
}

// A failed operation's output is its standard error alone, between
// turnip's lines, with a line saying how much standard output was left
// out. Helmfile reports every error on standard error; standard output
// holds the diffs of a plan that cannot be applied.
func TestHelmfilePlugin_AFailedOperationShowsStandardErrorAlone(t *testing.T) {
	lines := []Line{
		turnipLine("@@ turnip: infra/web, helmfile v1.7.4 @@"),
		turnipLine("@@ turnip: helmfile --no-color diff @@"),
		stderrLine("Building dependency release=web, chart=charts/web"),
		stdoutLine("Comparing release=web, chart=charts/web"),
		stdoutLine("web, Deployment (apps) has changed:"),
		stdoutLine("+  replicas: 2"),
		stdoutLine("Comparing release=api, chart=charts/api"),
		stderrLine("in ./helmfile.yaml: failed processing release api"),
		stderrLine("Error: plugin \"diff\" exited with error"),
		turnipLine("@@ turnip: exit 1 in 4.2s @@"),
	}

	for _, operation := range []string{"diff", "apply", "sync"} {
		t.Run(operation, func(t *testing.T) {
			var calls []recordedCall
			p := &HelmfilePlugin{run: fakeRunner(t, &calls, lines, 1, nil)}

			result, err := p.Execute(context.Background(), operation, ExecuteOptions{})
			require.NoError(t, err)
			assert.Equal(t, 1, result.ExitCode)
			require.NoError(t, result.Error)
			assert.Equal(t, strings.Join([]string{
				"@@ turnip: infra/web, helmfile v1.7.4 @@",
				"@@ turnip: helmfile --no-color diff @@",
				"@@ turnip: 4 lines of standard output omitted because the operation failed @@",
				"Building dependency release=web, chart=charts/web",
				"in ./helmfile.yaml: failed processing release api",
				"Error: plugin \"diff\" exited with error",
				"@@ turnip: exit 1 in 4.2s @@",
			}, "\n"), result.Output)
		})
	}
}

// The spec's scenario: 30,000 lines of diffs, errors for two releases.
func TestHelmfilePlugin_AFailedDiffOmitsThirtyThousandLines(t *testing.T) {
	lines := []Line{
		turnipLine("@@ turnip: ., helmfile v1.7.4 @@"),
		turnipLine("@@ turnip: helmfile --no-color diff @@"),
	}
	for i := range 30000 {
		lines = append(lines, stdoutLine(fmt.Sprintf("+ manifest line %d", i)))
	}
	lines = append(lines,
		stderrLine("release \"web\" failed"),
		stderrLine("release \"api\" failed"),
		turnipLine("@@ turnip: exit 1 in 9s @@"),
	)
	var calls []recordedCall
	p := &HelmfilePlugin{run: fakeRunner(t, &calls, lines, 1, nil)}

	result, err := p.Execute(context.Background(), "diff", ExecuteOptions{})
	require.NoError(t, err)
	assert.Equal(t, strings.Join([]string{
		"@@ turnip: ., helmfile v1.7.4 @@",
		"@@ turnip: helmfile --no-color diff @@",
		"@@ turnip: 30000 lines of standard output omitted because the operation failed @@",
		"release \"web\" failed",
		"release \"api\" failed",
		"@@ turnip: exit 1 in 9s @@",
	}, "\n"), result.Output)
	assert.NotContains(t, result.Output, "manifest line", "no line of a diff among the errors")
}

func TestHelmfilePlugin_AFailureWithNoStandardOutputHasNoOmissionLine(t *testing.T) {
	var calls []recordedCall
	p := &HelmfilePlugin{run: fakeRunner(t, &calls, []Line{
		turnipLine("@@ turnip: helmfile --no-color diff @@"),
		stderrLine("err: no releases found"),
		turnipLine("@@ turnip: exit 1 in 1s @@"),
	}, 1, nil)}

	result, err := p.Execute(context.Background(), "diff", ExecuteOptions{})
	require.NoError(t, err)
	assert.Equal(t, "@@ turnip: helmfile --no-color diff @@\nerr: no releases found\n@@ turnip: exit 1 in 1s @@", result.Output)
	assert.NotContains(t, result.Output, "omitted")
}

func TestHelmfilePlugin_OneOmittedLineIsSingular(t *testing.T) {
	var calls []recordedCall
	p := &HelmfilePlugin{run: fakeRunner(t, &calls, []Line{stdoutLine("out"), stderrLine("err")}, 1, nil)}

	result, err := p.Execute(context.Background(), "apply", ExecuteOptions{})
	require.NoError(t, err)
	assert.Equal(t, "@@ turnip: 1 line of standard output omitted because the operation failed @@\nerr", result.Output)
}

// A failed apply keeps Helmfile's own account of what failed, which it
// prints on standard error, without the diffs it printed before trying.
func TestHelmfilePlugin_AFailedApplyKeepsItsTables(t *testing.T) {
	var calls []recordedCall
	p := &HelmfilePlugin{run: fakeRunner(t, &calls, []Line{
		stdoutLine("Comparing release=web, chart=charts/web"),
		stdoutLine("web, Deployment (apps) has changed:"),
		stderrLine("Upgrading release=web, chart=charts/web"),
		stderrLine(fixedBanner("Failed Releases")),
		stderrLine("NAME   NAMESPACE   CHART   VERSION   DURATION"),
		stderrLine("web    default     charts/web          3s"),
		stderrLine("Error: UPGRADE FAILED: timed out waiting for the condition"),
	}, 1, nil)}

	result, err := p.Execute(context.Background(), "apply", ExecuteOptions{})
	require.NoError(t, err)
	assert.Contains(t, result.Output, fixedBanner("Failed Releases"))
	assert.Contains(t, result.Output, "Error: UPGRADE FAILED")
	assert.NotContains(t, result.Output, "has changed:", "the diffs are standard output")
	assert.Contains(t, result.Output, "@@ turnip: 2 lines of standard output omitted because the operation failed @@")
}

// Counts come from the record, not from the output: a failed diff still
// reports the releases it found changed, though its output shows none of
// their diffs.
func TestHelmfilePlugin_AFailedDiffStillCountsItsChangedReleases(t *testing.T) {
	var calls []recordedCall
	p := &HelmfilePlugin{run: fakeRunner(t, &calls, []Line{
		stdoutLine("Comparing release=web, chart=charts/web"),
		stdoutLine("web, Deployment (apps) has changed:"),
		stdoutLine("Comparing release=api, chart=charts/api"),
		stdoutLine("api, ConfigMap (v1) has changed:"),
		stdoutLine("Comparing release=db, chart=charts/db"),
		stderrLine("Error: release db failed"),
	}, 1, nil)}

	result, err := p.Execute(context.Background(), "diff", ExecuteOptions{})
	require.NoError(t, err)
	assert.Equal(t, 2, result.ChangeSummary.Change)
	assert.NotContains(t, result.Output, "Comparing release=")
}

// The caller's Record is the one the command is recorded into, so whoever
// follows it sees the output while the command runs.
func TestHelmfilePlugin_RecordsIntoTheCallersRecord(t *testing.T) {
	var calls []recordedCall
	p := &HelmfilePlugin{run: fakeRunner(t, &calls, []Line{stdoutLine("out")}, 0, nil)}

	rec := NewRecord()
	result, err := p.Execute(context.Background(), "diff", ExecuteOptions{Record: rec})
	require.NoError(t, err)

	lines, done := rec.Next(context.Background(), 0)
	require.Len(t, lines, 1)
	assert.Equal(t, "out", lines[0].Text)
	assert.True(t, done, "the Record is closed once the command has ended")
	assert.Equal(t, "out", result.Output)
}

func TestHelmfilePlugin_NameAndOperations(t *testing.T) {
	p := NewHelmfilePlugin()
	assert.Equal(t, "helmfile", p.Name())
	assert.Equal(t, []string{"diff", "apply", "sync"}, p.GetOperations())
	assert.Equal(t, "diff", p.GetPlanOperation())
	assert.Equal(t, "apply", p.GetApplyOperation())
}

// The declared value decides whether a Helmfile Project's Lock survives a
// plan that found nothing, so the reasoning matters as much as the value.
func TestHelmfile_ActsWithoutChanges(t *testing.T) {
	p := NewHelmfilePlugin()

	assert.True(t, p.ActsWithoutChanges(),
		"helmfile sync upgrades every release regardless of the diff, so it acts when a diff found nothing")
	assert.Contains(t, p.GetOperations(), "sync",
		"the declaration above is true only while sync is exposed; if it ever goes, revisit it")
}

// Output is the record in the order it arrived. Helmfile writes its
// repository setup to stderr *before* the diff; grouping by stream put it
// after the diff and after the trailer, telling a different story from the
// run.
func TestHelmfilePlugin_OutputKeepsTheRecordsInterleaving(t *testing.T) {
	var calls []recordedCall
	p := &HelmfilePlugin{run: fakeRunner(t, &calls, []Line{
		turnipLine("@@ turnip: env, helmfile v1 @@"),
		stderrLine("Adding repo stable https://example.test/charts"),
		stdoutLine("Comparing release=web, chart=charts/web"),
		stderrLine("Building dependency release=api, chart=charts/api"),
		stdoutLine("Comparing release=api, chart=charts/api"),
		turnipLine("@@ turnip: exit 0 in 1s @@"),
	}, 0, nil)}

	result, err := p.Execute(context.Background(), "diff", ExecuteOptions{})
	require.NoError(t, err)
	assert.Equal(t, strings.Join([]string{
		"@@ turnip: env, helmfile v1 @@",
		"Adding repo stable https://example.test/charts",
		"Comparing release=web, chart=charts/web",
		"Building dependency release=api, chart=charts/api",
		"Comparing release=api, chart=charts/api",
		"@@ turnip: exit 0 in 1s @@",
	}, "\n"), result.Output)
}

// The change count reads stdout only. Stderr lines following an unchanged
// last release — the shape a real diff produced — are not its diff body.
//
// Mutation check: counting from the whole record reports 2 here.
func TestHelmfilePlugin_ChangeCountIgnoresStderrAfterTheLastRelease(t *testing.T) {
	var calls []recordedCall
	p := &HelmfilePlugin{run: fakeRunner(t, &calls, []Line{
		stdoutLine("Comparing release=web, chart=charts/web"),
		stdoutLine("web, Deployment (apps) has changed:"),
		stdoutLine("Comparing release=api, chart=charts/api"),
		stderrLine("Adding repo stable https://example.test/charts"),
	}, 0, nil)}

	result, err := p.Execute(context.Background(), "diff", ExecuteOptions{})
	require.NoError(t, err)
	assert.Equal(t, 1, result.ChangeSummary.Change, "api is unchanged; the stderr line is not its body")
}

// Execute reads apply's and sync's counts from stderr's Release_Tables,
// never from stdout, and diff keeps counting from stdout as before
// (design's Testing section, "Execute").
func TestHelmfilePlugin_ExecuteReadsMutationCountsFromStderr(t *testing.T) {
	stderrTable := table(fixedBanner("Updated Releases"), updatedHeader,
		"web   default   example.com/web   1.2.3   1s")

	for _, operation := range []string{"apply", "sync"} {
		t.Run(operation, func(t *testing.T) {
			var calls []recordedCall
			p := &HelmfilePlugin{run: fakeRunner(t, &calls, []Line{
				stdoutLine("this line names Updated Releases but is stdout, not the table"),
				stderrLine(fixedBanner("Updated Releases")),
				stderrLine(updatedHeader),
				stderrLine("web   default   example.com/web   1.2.3   1s"),
			}, 0, nil)}

			result, err := p.Execute(context.Background(), operation, ExecuteOptions{})
			require.NoError(t, err)
			assert.Equal(t, ChangeSummary{Change: 1}, result.ChangeSummary)
		})
	}

	t.Run("diff still counts from stdout", func(t *testing.T) {
		var calls []recordedCall
		p := &HelmfilePlugin{run: fakeRunner(t, &calls, []Line{
			stdoutLine("Comparing release=web, chart=charts/web"),
			stdoutLine("web, Deployment (apps) has changed:"),
			stderrLine(stderrTable),
		}, 0, nil)}

		result, err := p.Execute(context.Background(), "diff", ExecuteOptions{})
		require.NoError(t, err)
		assert.Equal(t, ChangeSummary{Change: 1}, result.ChangeSummary, "diff's own stdout parse, unaffected by stderr")
	})
}

// A release whose diff was "empty after suppression" on stdout is still
// upgraded by apply, and appears in stderr's Updated table. It is counted
// once, from the table, never again from stdout (Requirement 4.1).
func TestHelmfilePlugin_ExecuteCountsSuppressedDiffReleaseOnce(t *testing.T) {
	var calls []recordedCall
	p := &HelmfilePlugin{run: fakeRunner(t, &calls, []Line{
		stdoutLine("Comparing release=web, chart=example.com/web"),
		stdoutLine("default, web, Deployment (apps) has changed:"),
		stdoutLine("-  replicas: 1"),
		stdoutLine("+  replicas: 2"),
		stdoutLine("Comparing release=api, chart=example.com/api"),
		stdoutLine("default, api, Secret (v1) has changed, but diff is empty after suppression."),
		stderrLine("Upgrading release=web, chart=example.com/web"),
		stderrLine("Upgrading release=api, chart=example.com/api"),
		stderrLine(""),
		stderrLine(fixedBanner("Updated Releases")),
		stderrLine(updatedHeader),
		stderrLine("web   default   example.com/web   1.2.3   2s"),
		stderrLine("api   default   example.com/api   4.5.6   1s"),
		stderrLine(""),
	}, 0, nil)}

	result, err := p.Execute(context.Background(), "apply", ExecuteOptions{})
	require.NoError(t, err)
	assert.Equal(t, ChangeSummary{Change: 2}, result.ChangeSummary)
}

// A Release_Table's banner text landing in a stdout diff body (e.g. a
// ConfigMap whose data happens to hold it) must never inflate an apply's
// or sync's count: Execute reads stderr only for those operations.
func TestHelmfilePlugin_ExecuteIgnoresTableTextOnStdout(t *testing.T) {
	var calls []recordedCall
	p := &HelmfilePlugin{run: fakeRunner(t, &calls, []Line{
		stdoutLine(fixedBanner("Updated Releases")),
		stdoutLine(updatedHeader),
		stdoutLine("web   default   example.com/web   1.2.3   1s"),
	}, 0, nil)}

	result, err := p.Execute(context.Background(), "apply", ExecuteOptions{})
	require.NoError(t, err)
	assert.Equal(t, ChangeSummary{}, result.ChangeSummary)
}

// A diff counts the releases it will remove from the "Affected releases
// are:" summary on stderr, beside the changed releases from stdout
// (plugin-helmfile, "Change counts for diff").
func TestHelmfilePlugin_ExecuteDiffCountsRemovals(t *testing.T) {
	tests := []struct {
		name  string
		lines []Line
		want  ChangeSummary
	}{
		{
			name: "pending removal",
			lines: []Line{
				stderrLine("Affected releases are:"),
				stderrLine("  web (example/web) DELETED"),
				stderrLine(""),
			},
			want: ChangeSummary{Destroy: 1},
		},
		{
			name: "removals and changes together",
			lines: []Line{
				stdoutLine("Comparing release=a, chart=example/a"),
				stdoutLine("default, a, Deployment (apps) has changed:"),
				stdoutLine("Comparing release=b, chart=example/b"),
				stdoutLine("default, b, Deployment (apps) has changed:"),
				stderrLine("Affected releases are:"),
				stderrLine("  c (example/c) DELETED"),
				stderrLine(""),
			},
			want: ChangeSummary{Change: 2, Destroy: 1},
		},
		{
			name: "removals in two helmfiles",
			lines: []Line{
				stderrLine("Affected releases are:"),
				stderrLine("  web (example/web) DELETED"),
				stderrLine(""),
				stderrLine("Affected releases are:"),
				stderrLine("  api (example/api) DELETED"),
				stderrLine(""),
			},
			want: ChangeSummary{Destroy: 2},
		},
		{
			name: "updated releases in the summary",
			lines: []Line{
				stdoutLine("Comparing release=a, chart=example/a"),
				stdoutLine("default, a, Deployment (apps) has changed:"),
				stderrLine("Affected releases are:"),
				stderrLine("  a (example/a) UPDATED"),
				stderrLine(""),
			},
			want: ChangeSummary{Change: 1},
		},
		{
			name: "the summary's text in a diff body",
			lines: []Line{
				stdoutLine("Comparing release=cm, chart=example/cm"),
				stdoutLine("Affected releases are:"),
				stdoutLine("  web (example/web) DELETED"),
			},
			want: ChangeSummary{Change: 1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls []recordedCall
			p := &HelmfilePlugin{run: fakeRunner(t, &calls, tt.lines, 0, nil)}

			result, err := p.Execute(context.Background(), "diff", ExecuteOptions{})
			require.NoError(t, err)
			assert.Equal(t, tt.want, result.ChangeSummary)
		})
	}
}

// The existing apply cases, asked for an apply (and for a sync, which
// answers the same).
func TestHelmfilePlugin_ChangesReported(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   bool
	}{
		{
			name:   "a current transcript with the framed banner",
			output: table(fixedBanner("Updated Releases"), updatedHeader, "web   default   example.com/web   1.2.3   1s"),
			want:   true,
		},
		{
			name:   "an older-format transcript",
			output: "UPDATED RELEASES:\n" + updatedHeader + "\nweb   default   example.com/web   1.2.3   1s\n\n",
			want:   false,
		},
		{
			name:   "no table at all",
			output: "Upgraded release=web\n",
			want:   true,
		},
		{
			name:   "--no-color, which turnip always passes, silences nothing",
			output: "@@ turnip: helmfile --no-color apply @@\n",
			want:   true,
		},
		{
			name:   "--no-color before a quiet plan's replayed -q",
			output: "@@ turnip: helmfile --no-color apply -q @@\n",
			want:   false,
		},
		{
			name:   "--debug still overrides --quiet behind --no-color",
			output: "@@ turnip: helmfile --no-color apply --quiet --debug @@\n",
			want:   true,
		},
		{
			name:   "an older-format transcript that only reinstalled (v1.4.0 to v1.5.x)",
			output: "REINSTALLED RELEASES:\n" + updatedHeader + "\nweb   default   example.com/web   1.2.3   1s\n\n",
			want:   false,
		},
		{
			name:   "run with -q: Helmfile printed no tables",
			output: "@@ turnip: a, helmfile v1.8.1 @@\n@@ turnip: helmfile apply -q @@\n@@ turnip: exit 0 in 2s @@",
			want:   false,
		},
		{
			name:   "run with --quiet",
			output: "@@ turnip: helmfile --environment staging apply --quiet @@\n",
			want:   false,
		},
		{
			name:   "run with --log-level warn",
			output: "@@ turnip: helmfile apply --log-level warn @@\n",
			want:   false,
		},
		{
			name:   "run with --log-level=error",
			output: "@@ turnip: helmfile sync --log-level=error @@\n",
			want:   false,
		},
		{
			name:   "--debug overrides -q, as in Helmfile",
			output: "@@ turnip: helmfile apply -q --debug @@\n" + table(fixedBanner("Updated Releases"), updatedHeader, "web   default   example.com/web   1.2.3   1s"),
			want:   true,
		},
		{
			name:   "run with --log-level info still prints its tables",
			output: "@@ turnip: helmfile apply --log-level info @@\n",
			want:   true,
		},
		{
			name:   "-q after -- is not Helmfile's flag",
			output: "@@ turnip: helmfile apply -- -q @@\n",
			want:   true,
		},
		{
			name:   "-q in the tool's own output is not the argv",
			output: "@@ turnip: helmfile apply @@\nhelmfile apply -q\n",
			want:   true,
		},
		{
			name: "a current run whose stdout diff holds the older banner's text: the accepted trade-off",
			output: "Comparing release=web, chart=charts/web\n" +
				"UPDATED RELEASES:\n" +
				"this is diff content that happens to contain that text\n",
			want: false,
		},
	}

	p := NewHelmfilePlugin()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, p.ChangesReported("apply", tt.output))
			assert.Equal(t, tt.want, p.ChangesReported("sync", tt.output))
		})
	}
}

// A quiet diff prints no "Affected releases are:" summary, so its counts
// are not reported (fix-helmfile-diff-removals, Decision 2). Only the log
// level is read for a diff: the older banners are an apply's.
//
// Mutation check: answering true for every diff fails the quiet cases;
// looking for the older banners in a diff fails the last case.
func TestHelmfilePlugin_ChangesReportedDiff(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   bool
	}{
		{
			name:   "no flag",
			output: "@@ turnip: helmfile --no-color diff @@\nComparing release=web, chart=charts/web\n",
			want:   true,
		},
		{
			name:   "-q",
			output: "@@ turnip: helmfile --no-color diff -q @@\nComparing release=web, chart=charts/web\n",
			want:   false,
		},
		{
			name:   "--quiet",
			output: "@@ turnip: helmfile --no-color diff --quiet @@\n",
			want:   false,
		},
		{
			name:   "--log-level warn",
			output: "@@ turnip: helmfile --no-color diff --log-level warn @@\n",
			want:   false,
		},
		{
			name:   "--debug overrides -q",
			output: "@@ turnip: helmfile --no-color diff --debug -q @@\n",
			want:   true,
		},
		{
			name:   "an older banner's text in a diff body is not an apply's banner",
			output: "@@ turnip: helmfile --no-color diff @@\nUPDATED RELEASES:\n",
			want:   true,
		},
	}

	p := NewHelmfilePlugin()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, p.ChangesReported("diff", tt.output))
		})
	}
}

// An operation Helmfile does not count from (none today) reports its
// counts, whatever its transcript holds.
func TestHelmfilePlugin_ChangesReportedOtherOperation(t *testing.T) {
	p := NewHelmfilePlugin()
	assert.True(t, p.ChangesReported("lint", "@@ turnip: helmfile lint -q @@\nUPDATED RELEASES:\n"))
}

// A stderr line between two releases changes neither.
//
// Mutation check: counting from the whole record reports 2 here.
func TestHelmfilePlugin_ChangeCountIgnoresStderrBetweenReleases(t *testing.T) {
	var calls []recordedCall
	p := &HelmfilePlugin{run: fakeRunner(t, &calls, []Line{
		stdoutLine("Comparing release=web, chart=charts/web"),
		stderrLine("Building dependency release=api, chart=charts/api"),
		stdoutLine("Comparing release=api, chart=charts/api"),
		stdoutLine("api, ConfigMap (v1) has changed:"),
	}, 0, nil)}

	result, err := p.Execute(context.Background(), "diff", ExecuteOptions{})
	require.NoError(t, err)
	assert.Equal(t, 1, result.ChangeSummary.Change, "web is unchanged; only api has a body")
}
