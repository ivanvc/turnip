package plugin

import (
	"context"
	"reflect"
	"testing"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
)

func testParameters() *gopter.TestParameters {
	params := gopter.DefaultTestParameters()
	params.MinSuccessfulTests = 100
	return params
}

// Feature: multi-iac-automation-platform, Property 4: Plugin Result Structure Completeness
func TestProperty_PluginResultStructureCompleteness(t *testing.T) {
	properties := gopter.NewProperties(testParameters())
	operations := (&HelmfilePlugin{}).GetOperations()

	properties.Property("Execute always returns a populated ExecuteResult for a supported operation", prop.ForAll(
		func(opIndex int, stdout, stderr string, exitCode int) bool {
			operation := operations[opIndex%len(operations)]
			p := &HelmfilePlugin{
				run: func(ctx context.Context, dir, name string, args []string) ([]byte, []byte, int, error) {
					return []byte(stdout), []byte(stderr), exitCode, nil
				},
			}

			result, err := p.Execute(context.Background(), operation, ExecuteOptions{})
			if err != nil || result == nil {
				return false
			}

			return result.ExitCode == exitCode
		},
		gen.IntRange(0, len(operations)-1),
		gen.AlphaString(),
		gen.AlphaString(),
		gen.IntRange(0, 255),
	))

	properties.TestingRun(t)
}

// Feature: multi-iac-automation-platform, Property 22: Helmfile Plugin Command Execution
func TestProperty_HelmfilePluginCommandExecution(t *testing.T) {
	properties := gopter.NewProperties(testParameters())

	properties.Property("Execute invokes `helmfile <operation>`, with --environment placed before it when configured", prop.ForAll(
		func(operation, environment string) bool {
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
				return false
			}

			return gotName == "helmfile" && reflect.DeepEqual(gotArgs, wantArgs)
		},
		gen.OneConstOf("diff", "apply", "sync", "destroy"),
		gen.OneConstOf("", "staging", "production"),
	))

	properties.TestingRun(t)
}
