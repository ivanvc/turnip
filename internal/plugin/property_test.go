package plugin

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
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
		require.NoError(t, err)
		require.NotNil(t, result)
		require.Equal(t, exitCode, result.ExitCode)
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

		_, err := p.Execute(context.Background(), operation, opts)
		require.NoError(t, err)
		require.Equal(t, "helmfile", gotName)
		require.Equal(t, wantArgs, gotArgs)
	})
}
