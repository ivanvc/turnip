package plugin

import (
	"context"
	"reflect"
	"testing"

	"pgregory.net/rapid"
)

// Feature: multi-iac-automation-platform, Property 4: Plugin Result Structure Completeness
func TestProperty_PluginResultStructureCompleteness(t *testing.T) {
	operations := (&HelmfilePlugin{}).GetOperations()

	rapid.Check(t, func(t *rapid.T) {
		operation := rapid.SampledFrom(operations).Draw(t, "operation")
		stdout := rapid.String().Draw(t, "stdout")
		stderr := rapid.String().Draw(t, "stderr")
		exitCode := rapid.IntRange(0, 255).Draw(t, "exitCode")

		p := &HelmfilePlugin{
			run: func(ctx context.Context, dir, name string, args []string) ([]byte, []byte, int, error) {
				return []byte(stdout), []byte(stderr), exitCode, nil
			},
		}

		result, err := p.Execute(context.Background(), operation, ExecuteOptions{})
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		if result == nil {
			t.Fatalf("Execute() result = nil, want a populated *ExecuteResult")
		}
		if result.ExitCode != exitCode {
			t.Fatalf("Execute().ExitCode = %d, want %d", result.ExitCode, exitCode)
		}
	})
}

// Feature: multi-iac-automation-platform, Property 22: Helmfile Plugin Command Execution
func TestProperty_HelmfilePluginCommandExecution(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		operation := rapid.SampledFrom([]string{"diff", "apply", "sync", "destroy"}).Draw(t, "operation")
		environment := rapid.SampledFrom([]string{"", "staging", "production"}).Draw(t, "environment")

		var gotName string
		var gotArgs []string
		p := &HelmfilePlugin{
			run: func(ctx context.Context, dir, name string, args []string) ([]byte, []byte, int, error) {
				gotName = name
				gotArgs = args
				return nil, nil, 0, nil
			},
		}

		opts := ExecuteOptions{}
		wantArgs := []string{}
		if environment != "" {
			opts.Config = map[string]string{"environment": environment}
			wantArgs = append(wantArgs, "--environment", environment)
		}
		wantArgs = append(wantArgs, operation)

		if _, err := p.Execute(context.Background(), operation, opts); err != nil {
			t.Fatalf("Execute() error = %v", err)
		}

		if gotName != "helmfile" {
			t.Fatalf("run() name = %q, want %q", gotName, "helmfile")
		}
		if !reflect.DeepEqual(gotArgs, wantArgs) {
			t.Fatalf("run() args = %v, want %v", gotArgs, wantArgs)
		}
	})
}
