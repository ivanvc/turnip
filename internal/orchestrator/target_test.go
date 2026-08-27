package orchestrator

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivanvc/turnip/internal/config"
	"github.com/ivanvc/turnip/internal/github"
	"github.com/ivanvc/turnip/internal/plugin"
)

// fakePlugin is a minimal plugin.Plugin for exercising target.go's
// resolution logic without a real tool binary.
type fakePlugin struct {
	name           string
	operations     []string
	planOperation  string
	applyOperation string
}

func (f *fakePlugin) Name() string              { return f.name }
func (f *fakePlugin) GetOperations() []string   { return f.operations }
func (f *fakePlugin) GetPlanOperation() string  { return f.planOperation }
func (f *fakePlugin) GetApplyOperation() string { return f.applyOperation }
func (f *fakePlugin) Execute(ctx context.Context, operation string, opts plugin.ExecuteOptions) (*plugin.ExecuteResult, error) {
	panic("not used by target_test.go")
}

func testRegistry() PluginRegistry {
	return PluginRegistry{
		"helmfile": &fakePlugin{name: "helmfile", operations: []string{"diff", "apply", "sync"}, planOperation: "diff", applyOperation: "apply"},
		"pulumi":   &fakePlugin{name: "pulumi", operations: []string{"preview", "up"}, planOperation: "preview", applyOperation: "up"},
	}
}

func testConfig() *config.Config {
	return &config.Config{Projects: []config.Project{
		{Name: "helm-a", Directory: "a", Tool: "helmfile"},
		{Name: "helm-b", Directory: "b", Tool: "helmfile"},
		{Name: "pulumi-a", Directory: "c", Tool: "pulumi"},
	}}
}

// fakeAuthClient is a minimal github.GitHubClient backing an Authorizer
// in tests, returning a scripted permission for every call.
type fakeAuthClient struct {
	github.GitHubClient
	permission string
	err        error
}

func (f *fakeAuthClient) GetCollaboratorPermission(ctx context.Context, owner, repo, username string) (string, error) {
	return f.permission, f.err
}

func TestResolveTargets_ToolFiltering(t *testing.T) {
	cfg := testConfig()
	authorizer := github.NewAuthorizer(&fakeAuthClient{permission: "write"})

	targets, rejected, err := resolveTargets(context.Background(), cfg, testRegistry(),
		&github.TriggerCommand{Tool: "helmfile", Operation: "diff"}, "o", "r", "author", authorizer)
	require.NoError(t, err)
	assert.Empty(t, rejected)
	require.Len(t, targets, 2)
	names := []string{targets[0].Project.Name, targets[1].Project.Name}
	assert.ElementsMatch(t, []string{"helm-a", "helm-b"}, names)
}

func TestResolveTargets_TurnipTargetsEveryTool(t *testing.T) {
	cfg := testConfig()
	authorizer := github.NewAuthorizer(&fakeAuthClient{permission: "write"})

	targets, rejected, err := resolveTargets(context.Background(), cfg, testRegistry(),
		&github.TriggerCommand{Tool: "turnip", Operation: "diff"}, "o", "r", "author", authorizer)
	require.NoError(t, err)
	// "diff" is only recognized by helmfile — pulumi's candidates are
	// rejected (4.5), not silently dropped, and never fail the whole
	// command (this is the mixed-tool case design.md calls out).
	require.Len(t, targets, 2)
	require.Len(t, rejected, 1)
	assert.Equal(t, "pulumi-a", rejected[0].ProjectName)
}

func TestResolveTargets_NamedProjectNarrows(t *testing.T) {
	cfg := testConfig()
	authorizer := github.NewAuthorizer(&fakeAuthClient{permission: "write"})

	targets, _, err := resolveTargets(context.Background(), cfg, testRegistry(),
		&github.TriggerCommand{Tool: "turnip", Operation: "diff", Projects: []string{"helm-a"}}, "o", "r", "author", authorizer)
	require.NoError(t, err)
	require.Len(t, targets, 1)
	assert.Equal(t, "helm-a", targets[0].Project.Name)
}

func TestResolveTargets_UnmatchedNameErrors(t *testing.T) {
	cfg := testConfig()
	authorizer := github.NewAuthorizer(&fakeAuthClient{permission: "write"})

	_, _, err := resolveTargets(context.Background(), cfg, testRegistry(),
		&github.TriggerCommand{Tool: "turnip", Operation: "diff", Projects: []string{"does-not-exist"}}, "o", "r", "author", authorizer)
	require.Error(t, err)
	var unmatched *UnmatchedProjectError
	require.ErrorAs(t, err, &unmatched)
	assert.Equal(t, "does-not-exist", unmatched.Name)
	require.ErrorIs(t, err, ErrUnmatchedProject)
}

func TestResolveTargets_UnrecognizedOperationIsRejectedNotWholeCommand(t *testing.T) {
	cfg := testConfig()
	authorizer := github.NewAuthorizer(&fakeAuthClient{permission: "write"})

	targets, rejected, err := resolveTargets(context.Background(), cfg, testRegistry(),
		&github.TriggerCommand{Tool: "helmfile", Operation: "bogus"}, "o", "r", "author", authorizer)
	require.NoError(t, err)
	assert.Empty(t, targets)
	assert.Len(t, rejected, 2)
}

func TestResolveTargets_NonPlanOperationRequiresWritePermission(t *testing.T) {
	cfg := testConfig()
	authorizer := github.NewAuthorizer(&fakeAuthClient{permission: "read"})

	targets, rejected, err := resolveTargets(context.Background(), cfg, testRegistry(),
		&github.TriggerCommand{Tool: "helmfile", Operation: "apply", Projects: []string{"helm-a"}}, "o", "r", "author", authorizer)
	require.NoError(t, err)
	assert.Empty(t, targets)
	require.Len(t, rejected, 1)
	assert.Equal(t, "helm-a", rejected[0].ProjectName)
}

func TestResolveTargets_PlanOperationDoesNotRequireWritePermission(t *testing.T) {
	cfg := testConfig()
	authorizer := github.NewAuthorizer(&fakeAuthClient{permission: "read"})

	targets, rejected, err := resolveTargets(context.Background(), cfg, testRegistry(),
		&github.TriggerCommand{Tool: "helmfile", Operation: "diff", Projects: []string{"helm-a"}}, "o", "r", "author", authorizer)
	require.NoError(t, err)
	assert.Empty(t, rejected)
	require.Len(t, targets, 1)
}

func TestResolveTargets_ExtraArgsPassedThrough(t *testing.T) {
	cfg := testConfig()
	authorizer := github.NewAuthorizer(&fakeAuthClient{permission: "write"})

	targets, _, err := resolveTargets(context.Background(), cfg, testRegistry(),
		&github.TriggerCommand{Tool: "helmfile", Operation: "diff", Projects: []string{"helm-a"}, ExtraArgs: []string{"--foo"}},
		"o", "r", "author", authorizer)
	require.NoError(t, err)
	require.Len(t, targets, 1)
	assert.Equal(t, []string{"--foo"}, targets[0].ExtraArgs)
}

func TestResolveUnlockCandidates_SkipsOperationValidation(t *testing.T) {
	cfg := testConfig()
	candidates, err := resolveUnlockCandidates(cfg, &github.TriggerCommand{Tool: "turnip", Operation: "unlock"})
	require.NoError(t, err)
	assert.Len(t, candidates, 3)
}

func TestResolveUnlockCandidates_NamedProjectNarrows(t *testing.T) {
	cfg := testConfig()
	candidates, err := resolveUnlockCandidates(cfg, &github.TriggerCommand{Tool: "turnip", Operation: "unlock", Projects: []string{"helm-a"}})
	require.NoError(t, err)
	require.Len(t, candidates, 1)
	assert.Equal(t, "helm-a", candidates[0].Name)
}
