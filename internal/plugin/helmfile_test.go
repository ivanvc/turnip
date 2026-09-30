package plugin

import (
	"context"
	"errors"
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

// fakeRunner returns lines as the command's record. Fixtures state their
// interleaving explicitly, because the order of the two streams is part of
// what a Plugin is handed.
func fakeRunner(t *testing.T, calls *[]recordedCall, lines []OutputLine, exitCode int, err error) commandRunner {
	t.Helper()
	return func(ctx context.Context, dir, name string, args []string, version string, onOutput func(stream, line string)) ([]OutputLine, int, error) {
		*calls = append(*calls, recordedCall{dir: dir, name: name, args: args})
		return lines, exitCode, err
	}
}

func stdoutLine(text string) OutputLine { return OutputLine{Stream: "stdout", Text: text} }
func stderrLine(text string) OutputLine { return OutputLine{Stream: "stderr", Text: text} }

func TestHelmfilePlugin_ExecuteInvokesCorrectCommand(t *testing.T) {
	tests := []struct {
		operation string
		wantArgs  []string
	}{
		{"diff", []string{"diff"}},
		{"apply", []string{"apply"}},
		{"sync", []string{"sync"}},
	}

	for _, tt := range tests {
		t.Run(tt.operation, func(t *testing.T) {
			var calls []recordedCall
			p := &HelmfilePlugin{run: fakeRunner(t, &calls, []OutputLine{stdoutLine("out")}, 0, nil)}

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
	assert.Equal(t, []string{"--environment", "staging", "diff"}, calls[0].args)
}

func TestHelmfilePlugin_NoEnvironmentFlagWhenAbsent(t *testing.T) {
	var calls []recordedCall
	p := &HelmfilePlugin{run: fakeRunner(t, &calls, nil, 0, nil)}

	_, err := p.Execute(context.Background(), "diff", ExecuteOptions{})
	require.NoError(t, err)
	assert.Equal(t, []string{"diff"}, calls[0].args)
}

func TestHelmfilePlugin_ExtraArgsAppended(t *testing.T) {
	var calls []recordedCall
	p := &HelmfilePlugin{run: fakeRunner(t, &calls, nil, 0, nil)}

	_, err := p.Execute(context.Background(), "diff", ExecuteOptions{
		Config:    map[string]string{"environment": "staging"},
		ExtraArgs: []string{"--quiet"},
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"--environment", "staging", "diff", "--quiet"}, calls[0].args)
}

func TestHelmfilePlugin_ChangeSummaryOnlyForDiff(t *testing.T) {
	diffOutput := []OutputLine{stdoutLine("Comparing release=a, chart=charts/a"), stdoutLine("something changed")}

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

func TestHelmfilePlugin_PlanDataAlwaysNil(t *testing.T) {
	var calls []recordedCall
	p := &HelmfilePlugin{run: fakeRunner(t, &calls, []OutputLine{stdoutLine("out")}, 0, nil)}

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

func TestHelmfilePlugin_StderrAppendedToOutput(t *testing.T) {
	var calls []recordedCall
	p := &HelmfilePlugin{run: fakeRunner(t, &calls, []OutputLine{stdoutLine("out"), stderrLine("err")}, 1, nil)}

	result, err := p.Execute(context.Background(), "apply", ExecuteOptions{})
	require.NoError(t, err)
	assert.Equal(t, 1, result.ExitCode)
	assert.Equal(t, "out\nerr", result.Output)
	assert.NoError(t, result.Error)
}

func TestHelmfilePlugin_OnOutputReachesCommandRunnerUnchanged(t *testing.T) {
	var calls []recordedCall
	var gotOnOutput func(stream, line string)
	p := &HelmfilePlugin{run: func(ctx context.Context, dir, name string, args []string, version string, onOutput func(stream, line string)) ([]OutputLine, int, error) {
		calls = append(calls, recordedCall{dir: dir, name: name, args: args})
		gotOnOutput = onOutput
		return []OutputLine{stdoutLine("out")}, 0, nil
	}}

	var got []string
	wantOnOutput := func(stream, line string) { got = append(got, stream+":"+line) }

	_, err := p.Execute(context.Background(), "diff", ExecuteOptions{OnOutput: wantOnOutput})
	require.NoError(t, err)
	require.NotNil(t, gotOnOutput)

	gotOnOutput("stdout", "a line")
	assert.Equal(t, []string{"stdout:a line"}, got)
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
	p := &HelmfilePlugin{run: fakeRunner(t, &calls, []OutputLine{
		stdoutLine("@@ turnip: env, helmfile v1 @@"),
		stderrLine("Adding repo stable https://example.test/charts"),
		stdoutLine("Comparing release=web, chart=charts/web"),
		stderrLine("Building dependency release=api, chart=charts/api"),
		stdoutLine("Comparing release=api, chart=charts/api"),
		stdoutLine("@@ turnip: exit 0 in 1s @@"),
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
	p := &HelmfilePlugin{run: fakeRunner(t, &calls, []OutputLine{
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
			p := &HelmfilePlugin{run: fakeRunner(t, &calls, []OutputLine{
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
		p := &HelmfilePlugin{run: fakeRunner(t, &calls, []OutputLine{
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
	p := &HelmfilePlugin{run: fakeRunner(t, &calls, []OutputLine{
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
	p := &HelmfilePlugin{run: fakeRunner(t, &calls, []OutputLine{
		stdoutLine(fixedBanner("Updated Releases")),
		stdoutLine(updatedHeader),
		stdoutLine("web   default   example.com/web   1.2.3   1s"),
	}, 0, nil)}

	result, err := p.Execute(context.Background(), "apply", ExecuteOptions{})
	require.NoError(t, err)
	assert.Equal(t, ChangeSummary{}, result.ChangeSummary)
}

func TestHelmfilePlugin_MutationChangesReported(t *testing.T) {
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
			assert.Equal(t, tt.want, p.MutationChangesReported(tt.output))
		})
	}
}

// A stderr line between two releases changes neither.
//
// Mutation check: counting from the whole record reports 2 here.
func TestHelmfilePlugin_ChangeCountIgnoresStderrBetweenReleases(t *testing.T) {
	var calls []recordedCall
	p := &HelmfilePlugin{run: fakeRunner(t, &calls, []OutputLine{
		stdoutLine("Comparing release=web, chart=charts/web"),
		stderrLine("Building dependency release=api, chart=charts/api"),
		stdoutLine("Comparing release=api, chart=charts/api"),
		stdoutLine("api, ConfigMap (v1) has changed:"),
	}, 0, nil)}

	result, err := p.Execute(context.Background(), "diff", ExecuteOptions{})
	require.NoError(t, err)
	assert.Equal(t, 1, result.ChangeSummary.Change, "web is unchanged; only api has a body")
}
