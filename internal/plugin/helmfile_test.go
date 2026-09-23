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
