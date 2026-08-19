package plugin

import (
	"context"
	"errors"
	"reflect"
	"testing"
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
			if err != nil {
				t.Fatalf("Execute returned unexpected error: %v", err)
			}
			if len(calls) != 1 {
				t.Fatalf("got %d calls, want 1", len(calls))
			}
			if calls[0].name != "helmfile" {
				t.Errorf("name = %q, want %q", calls[0].name, "helmfile")
			}
			if calls[0].dir != "/work" {
				t.Errorf("dir = %q, want %q", calls[0].dir, "/work")
			}
			if !reflect.DeepEqual(calls[0].args, tt.wantArgs) {
				t.Errorf("args = %v, want %v", calls[0].args, tt.wantArgs)
			}
		})
	}
}

func TestHelmfilePlugin_UnsupportedOperation(t *testing.T) {
	var calls []recordedCall
	p := &HelmfilePlugin{run: fakeRunner(t, &calls, nil, nil, 0, nil)}

	result, err := p.Execute(context.Background(), "plan", ExecuteOptions{})
	if result != nil {
		t.Errorf("result = %+v, want nil", result)
	}
	var unsupported *UnsupportedOperationError
	if !errors.As(err, &unsupported) {
		t.Fatalf("err = %v (%T), want *UnsupportedOperationError", err, err)
	}
	if len(calls) != 0 {
		t.Errorf("got %d calls, want 0 (runner should not be invoked)", len(calls))
	}
}

func TestHelmfilePlugin_EnvironmentFlag(t *testing.T) {
	var calls []recordedCall
	p := &HelmfilePlugin{run: fakeRunner(t, &calls, nil, nil, 0, nil)}

	_, err := p.Execute(context.Background(), "diff", ExecuteOptions{
		Config: map[string]string{"environment": "staging"},
	})
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}
	want := []string{"--environment", "staging", "diff"}
	if !reflect.DeepEqual(calls[0].args, want) {
		t.Errorf("args = %v, want %v", calls[0].args, want)
	}
}

func TestHelmfilePlugin_NoEnvironmentFlagWhenAbsent(t *testing.T) {
	var calls []recordedCall
	p := &HelmfilePlugin{run: fakeRunner(t, &calls, nil, nil, 0, nil)}

	_, err := p.Execute(context.Background(), "diff", ExecuteOptions{})
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}
	want := []string{"diff"}
	if !reflect.DeepEqual(calls[0].args, want) {
		t.Errorf("args = %v, want %v", calls[0].args, want)
	}
}

func TestHelmfilePlugin_ExtraArgsAppended(t *testing.T) {
	var calls []recordedCall
	p := &HelmfilePlugin{run: fakeRunner(t, &calls, nil, nil, 0, nil)}

	_, err := p.Execute(context.Background(), "diff", ExecuteOptions{
		Config:    map[string]string{"environment": "staging"},
		ExtraArgs: []string{"--quiet"},
	})
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}
	want := []string{"--environment", "staging", "diff", "--quiet"}
	if !reflect.DeepEqual(calls[0].args, want) {
		t.Errorf("args = %v, want %v", calls[0].args, want)
	}
}

func TestHelmfilePlugin_ChangeSummaryOnlyForDiff(t *testing.T) {
	diffOutput := []byte("Comparing release=a, chart=charts/a\nsomething changed\n")

	for _, operation := range []string{"apply", "sync", "destroy"} {
		t.Run(operation, func(t *testing.T) {
			var calls []recordedCall
			p := &HelmfilePlugin{run: fakeRunner(t, &calls, diffOutput, nil, 0, nil)}

			result, err := p.Execute(context.Background(), operation, ExecuteOptions{})
			if err != nil {
				t.Fatalf("Execute returned unexpected error: %v", err)
			}
			if result.ChangeSummary != (ChangeSummary{}) {
				t.Errorf("ChangeSummary = %+v, want zero value for operation %q", result.ChangeSummary, operation)
			}
		})
	}

	t.Run("diff", func(t *testing.T) {
		var calls []recordedCall
		p := &HelmfilePlugin{run: fakeRunner(t, &calls, diffOutput, nil, 0, nil)}

		result, err := p.Execute(context.Background(), "diff", ExecuteOptions{})
		if err != nil {
			t.Fatalf("Execute returned unexpected error: %v", err)
		}
		if result.ChangeSummary.Change != 1 {
			t.Errorf("ChangeSummary.Change = %d, want 1", result.ChangeSummary.Change)
		}
	})
}

func TestHelmfilePlugin_PlanDataAlwaysNil(t *testing.T) {
	var calls []recordedCall
	p := &HelmfilePlugin{run: fakeRunner(t, &calls, []byte("out"), nil, 0, nil)}

	result, err := p.Execute(context.Background(), "apply", ExecuteOptions{PlanData: []byte("ignored")})
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}
	if result.PlanData != nil {
		t.Errorf("PlanData = %v, want nil", result.PlanData)
	}
}

func TestHelmfilePlugin_RunnerErrorPropagates(t *testing.T) {
	wantErr := errors.New("binary not found")
	var calls []recordedCall
	p := &HelmfilePlugin{run: fakeRunner(t, &calls, nil, nil, -1, wantErr)}

	result, err := p.Execute(context.Background(), "diff", ExecuteOptions{})
	if result != nil {
		t.Errorf("result = %+v, want nil", result)
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("err = %v, want %v", err, wantErr)
	}
}

func TestHelmfilePlugin_StderrAppendedToOutput(t *testing.T) {
	var calls []recordedCall
	p := &HelmfilePlugin{run: fakeRunner(t, &calls, []byte("out"), []byte("err"), 1, nil)}

	result, err := p.Execute(context.Background(), "apply", ExecuteOptions{})
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}
	if result.ExitCode != 1 {
		t.Errorf("ExitCode = %d, want 1", result.ExitCode)
	}
	if result.Output != "out\nerr" {
		t.Errorf("Output = %q, want %q", result.Output, "out\nerr")
	}
	if result.Error != nil {
		t.Errorf("Error = %v, want nil", result.Error)
	}
}

func TestHelmfilePlugin_NameAndOperations(t *testing.T) {
	p := NewHelmfilePlugin()
	if p.Name() != "helmfile" {
		t.Errorf("Name() = %q, want %q", p.Name(), "helmfile")
	}
	wantOps := []string{"diff", "apply", "sync", "destroy"}
	if !reflect.DeepEqual(p.GetOperations(), wantOps) {
		t.Errorf("GetOperations() = %v, want %v", p.GetOperations(), wantOps)
	}
	if p.GetPlanOperation() != "diff" {
		t.Errorf("GetPlanOperation() = %q, want %q", p.GetPlanOperation(), "diff")
	}
	if p.GetApplyOperation() != "apply" {
		t.Errorf("GetApplyOperation() = %q, want %q", p.GetApplyOperation(), "apply")
	}
}
