package plugin

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordedCall struct {
	dir  string
	name string
	args []string
}

func fakeRunner(t *testing.T, calls *[]recordedCall, stdout, stderr []byte, exitCode int, err error) commandRunner {
	t.Helper()
	return func(ctx context.Context, dir, name string, args []string) ([]byte, []byte, int, error) {
		*calls = append(*calls, recordedCall{dir: dir, name: name, args: args})
		return stdout, stderr, exitCode, err
	}
}

func TestHelmfilePlugin_ExecuteInvokesCorrectCommand(t *testing.T) {
	tests := []struct {
		operation string
		wantArgs  []string
	}{
		{"diff", []string{"diff"}},
		{"apply", []string{"apply"}},
		{"sync", []string{"sync"}},
		{"destroy", []string{"destroy"}},
	}

	for _, tt := range tests {
		t.Run(tt.operation, func(t *testing.T) {
			var calls []recordedCall
			p := &HelmfilePlugin{run: fakeRunner(t, &calls, []byte("out"), nil, 0, nil)}

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
	p := &HelmfilePlugin{run: fakeRunner(t, &calls, nil, nil, 0, nil)}

	result, err := p.Execute(context.Background(), "plan", ExecuteOptions{})
	assert.Nil(t, result)
	var unsupported *UnsupportedOperationError
	require.ErrorAs(t, err, &unsupported)
	assert.Empty(t, calls, "runner should not be invoked")
}

func TestHelmfilePlugin_EnvironmentFlag(t *testing.T) {
	var calls []recordedCall
	p := &HelmfilePlugin{run: fakeRunner(t, &calls, nil, nil, 0, nil)}

	_, err := p.Execute(context.Background(), "diff", ExecuteOptions{
		Config: map[string]string{"environment": "staging"},
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"--environment", "staging", "diff"}, calls[0].args)
}

func TestHelmfilePlugin_NoEnvironmentFlagWhenAbsent(t *testing.T) {
	var calls []recordedCall
	p := &HelmfilePlugin{run: fakeRunner(t, &calls, nil, nil, 0, nil)}

	_, err := p.Execute(context.Background(), "diff", ExecuteOptions{})
	require.NoError(t, err)
	assert.Equal(t, []string{"diff"}, calls[0].args)
}

func TestHelmfilePlugin_ExtraArgsAppended(t *testing.T) {
	var calls []recordedCall
	p := &HelmfilePlugin{run: fakeRunner(t, &calls, nil, nil, 0, nil)}

	_, err := p.Execute(context.Background(), "diff", ExecuteOptions{
		Config:    map[string]string{"environment": "staging"},
		ExtraArgs: []string{"--quiet"},
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"--environment", "staging", "diff", "--quiet"}, calls[0].args)
}

func TestHelmfilePlugin_ChangeSummaryOnlyForDiff(t *testing.T) {
	diffOutput := []byte("Comparing release=a, chart=charts/a\nsomething changed\n")

	for _, operation := range []string{"apply", "sync", "destroy"} {
		t.Run(operation, func(t *testing.T) {
			var calls []recordedCall
			p := &HelmfilePlugin{run: fakeRunner(t, &calls, diffOutput, nil, 0, nil)}

			result, err := p.Execute(context.Background(), operation, ExecuteOptions{})
			require.NoError(t, err)
			assert.Equal(t, ChangeSummary{}, result.ChangeSummary)
		})
	}

	t.Run("diff", func(t *testing.T) {
		var calls []recordedCall
		p := &HelmfilePlugin{run: fakeRunner(t, &calls, diffOutput, nil, 0, nil)}

		result, err := p.Execute(context.Background(), "diff", ExecuteOptions{})
		require.NoError(t, err)
		assert.Equal(t, 1, result.ChangeSummary.Change)
	})
}

func TestHelmfilePlugin_PlanDataAlwaysNil(t *testing.T) {
	var calls []recordedCall
	p := &HelmfilePlugin{run: fakeRunner(t, &calls, []byte("out"), nil, 0, nil)}

	result, err := p.Execute(context.Background(), "apply", ExecuteOptions{PlanData: []byte("ignored")})
	require.NoError(t, err)
	assert.Nil(t, result.PlanData)
}

func TestHelmfilePlugin_RunnerErrorPropagates(t *testing.T) {
	wantErr := errors.New("binary not found")
	var calls []recordedCall
	p := &HelmfilePlugin{run: fakeRunner(t, &calls, nil, nil, -1, wantErr)}

	result, err := p.Execute(context.Background(), "diff", ExecuteOptions{})
	assert.Nil(t, result)
	assert.ErrorIs(t, err, wantErr)
}

func TestHelmfilePlugin_StderrAppendedToOutput(t *testing.T) {
	var calls []recordedCall
	p := &HelmfilePlugin{run: fakeRunner(t, &calls, []byte("out"), []byte("err"), 1, nil)}

	result, err := p.Execute(context.Background(), "apply", ExecuteOptions{})
	require.NoError(t, err)
	assert.Equal(t, 1, result.ExitCode)
	assert.Equal(t, "out\nerr", result.Output)
	assert.NoError(t, result.Error)
}

func TestHelmfilePlugin_NameAndOperations(t *testing.T) {
	p := NewHelmfilePlugin()
	assert.Equal(t, "helmfile", p.Name())
	assert.Equal(t, []string{"diff", "apply", "sync", "destroy"}, p.GetOperations())
	assert.Equal(t, "diff", p.GetPlanOperation())
	assert.Equal(t, "apply", p.GetApplyOperation())
}
