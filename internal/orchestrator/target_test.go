package orchestrator

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivanvc/turnip/internal/config"
	"github.com/ivanvc/turnip/internal/github"
	"github.com/ivanvc/turnip/internal/lock"
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

// testSelection builds the value resolve hangs off, from the same pieces
// each test already constructs inline. prNumber, locks and modifiedSet
// are left zero; a test exercising a bare default sets the ones it needs
// on the returned value, which keeps the callers that never touch those
// paths free of fixture they would not read.
func testSelection(cfg *config.Config, authorizer *github.Authorizer) *selection {
	return &selection{
		cfg:        cfg,
		plugins:    testRegistry(),
		owner:      "o",
		repo:       "r",
		author:     "author",
		authorizer: authorizer,
	}
}

func TestResolveTargets_ToolFiltering(t *testing.T) {
	cfg := testConfig()
	authorizer := github.NewAuthorizer(&fakeAuthClient{permission: "write"})

	// "*" rather than a bare trigger: this test is about tool filtering,
	// and "every configured Project" is what a bare command used to mean
	// and what "*" means now. The bare default has its own tests.
	targets, rejected, _, err := testSelection(cfg, authorizer).resolve(context.Background(),
		&github.TriggerCommand{Tool: "helmfile", Operation: "diff", Projects: []string{"*"}})
	require.NoError(t, err)
	assert.Empty(t, rejected)
	require.Len(t, targets, 2)
	names := []string{targets[0].Project.Name, targets[1].Project.Name}
	assert.ElementsMatch(t, []string{"helm-a", "helm-b"}, names)
}

// destroy is no longer a Helmfile operation, because no plan can describe
// what it would remove. A trigger naming it is refused by the same path
// any unknown operation takes — and producing no Target is what makes
// "no Lock, no check run, no Job" true, rather than a separate guard
// anyone could forget.
func TestResolveTargets_DestroyIsNotAHelmfileOperation(t *testing.T) {
	cfg := testConfig()
	authorizer := github.NewAuthorizer(&fakeAuthClient{permission: "write"})

	targets, rejected, _, err := testSelection(cfg, authorizer).resolve(context.Background(),
		&github.TriggerCommand{Tool: "helmfile", Operation: "destroy", Projects: []string{"*"}})

	require.NoError(t, err)
	assert.Empty(t, targets, "no Target means no Lock, no check run and no Job")
	require.Len(t, rejected, 2, "one refusal per Helmfile Project")
	for _, r := range rejected {
		assert.False(t, r.Success)
		assert.Contains(t, r.Output, "not recognized")
	}
}

func TestResolveTargets_TurnipTargetsEveryTool(t *testing.T) {
	cfg := testConfig()
	authorizer := github.NewAuthorizer(&fakeAuthClient{permission: "write"})

	targets, rejected, _, err := testSelection(cfg, authorizer).resolve(context.Background(),
		&github.TriggerCommand{Tool: "turnip", Operation: "diff", Projects: []string{"*"}})
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

	targets, _, _, err := testSelection(cfg, authorizer).resolve(context.Background(),
		&github.TriggerCommand{Tool: "turnip", Operation: "diff", Projects: []string{"helm-a"}})
	require.NoError(t, err)
	require.Len(t, targets, 1)
	assert.Equal(t, "helm-a", targets[0].Project.Name)
}

func TestResolveTargets_UnmatchedNameErrors(t *testing.T) {
	cfg := testConfig()
	authorizer := github.NewAuthorizer(&fakeAuthClient{permission: "write"})

	_, _, _, err := testSelection(cfg, authorizer).resolve(context.Background(),
		&github.TriggerCommand{Tool: "turnip", Operation: "diff", Projects: []string{"does-not-exist"}})
	require.Error(t, err)
	var unmatched *UnmatchedProjectError
	require.ErrorAs(t, err, &unmatched)
	assert.Equal(t, "does-not-exist", unmatched.Name)
	require.ErrorIs(t, err, ErrUnmatchedProject)
}

func TestResolveTargets_UnrecognizedOperationIsRejectedNotWholeCommand(t *testing.T) {
	cfg := testConfig()
	authorizer := github.NewAuthorizer(&fakeAuthClient{permission: "write"})

	targets, rejected, _, err := testSelection(cfg, authorizer).resolve(context.Background(),
		&github.TriggerCommand{Tool: "helmfile", Operation: "bogus", Projects: []string{"*"}})
	require.NoError(t, err)
	assert.Empty(t, targets)
	assert.Len(t, rejected, 2)
}

func TestResolveTargets_NonPlanOperationRequiresWritePermission(t *testing.T) {
	cfg := testConfig()
	authorizer := github.NewAuthorizer(&fakeAuthClient{permission: "read"})

	targets, rejected, _, err := testSelection(cfg, authorizer).resolve(context.Background(),
		&github.TriggerCommand{Tool: "helmfile", Operation: "apply", Projects: []string{"helm-a"}})
	require.NoError(t, err)
	assert.Empty(t, targets)
	require.Len(t, rejected, 1)
	assert.Equal(t, "helm-a", rejected[0].ProjectName)
}

func TestResolveTargets_PlanOperationDoesNotRequireWritePermission(t *testing.T) {
	cfg := testConfig()
	authorizer := github.NewAuthorizer(&fakeAuthClient{permission: "read"})

	targets, rejected, _, err := testSelection(cfg, authorizer).resolve(context.Background(),
		&github.TriggerCommand{Tool: "helmfile", Operation: "diff", Projects: []string{"helm-a"}})
	require.NoError(t, err)
	assert.Empty(t, rejected)
	require.Len(t, targets, 1)
}

func TestResolveTargets_ExtraArgsPassedThrough(t *testing.T) {
	cfg := testConfig()
	authorizer := github.NewAuthorizer(&fakeAuthClient{permission: "write"})

	targets, _, _, err := testSelection(cfg, authorizer).resolve(context.Background(),
		&github.TriggerCommand{Tool: "helmfile", Operation: "diff", Projects: []string{"helm-a"}, ExtraArgs: []string{"--foo"}})
	require.NoError(t, err)
	require.Len(t, targets, 1)
	assert.Equal(t, []string{"--foo"}, targets[0].ExtraArgs)
}

// selectorConfig backs the trigger-table cases: path-shaped names so
// patterns have something to match, a whenModified pattern per Project so
// the bare-plan default is exercised rather than trivially empty, and one
// Pulumi Project so the tool filter has something to exclude.
func selectorConfig() *config.Config {
	return &config.Config{Projects: []config.Project{
		{Name: "gcp/vpc", Directory: "env/gcp/vpc", Tool: "helmfile", WhenModified: []string{"env/gcp/vpc/**"}},
		{Name: "gcp/gke", Directory: "env/gcp/gke", Tool: "helmfile", WhenModified: []string{"env/gcp/gke/**"}},
		{Name: "aws/vpc", Directory: "env/aws/vpc", Tool: "helmfile", WhenModified: []string{"env/aws/vpc/**"}},
		{Name: "web", Directory: "apps/web", Tool: "pulumi", WhenModified: []string{"apps/web/**"}},
	}}
}

// names extracts Target Project names in the order resolve returned them,
// which is configuration order throughout.
func names(targets []Target) []string {
	out := make([]string, 0, len(targets))
	for _, t := range targets {
		out = append(out, t.Project.Name)
	}
	return out
}

// TestResolveTargets_TriggerTable is design.md's normative table, row by
// row. A row without a test is a row that will drift, which is why the
// table is written there to be executed here rather than described.
func TestResolveTargets_TriggerTable(t *testing.T) {
	// Touches one gcp Project and the Pulumi one; aws is untouched, so a
	// bare plan must not reach it.
	modified := []string{"env/gcp/vpc/main.tf", "apps/web/index.ts"}

	// gcp/vpc alone carries a plan from this pull request.
	heldPlans := map[string]bool{"gcp/vpc": true}

	tests := []struct {
		name         string
		tool         string
		operation    string
		projects     []string
		wantTargets  []string
		wantRejected int
		wantNotices  []string
		wantErrIs    error
	}{
		{
			name: "bare plan targets the Modified_Set across tools",
			tool: "turnip", operation: "diff",
			wantTargets: []string{"gcp/vpc"},
			// "web" is Pulumi: "diff" is not one of its operations, so it
			// is refused by name rather than silently dropped.
			wantRejected: 1,
		},
		{
			name: "bare plan scoped to a tool",
			tool: "helmfile", operation: "diff",
			wantTargets: []string{"gcp/vpc"},
		},
		{
			name: "named project is reached whether or not it was modified",
			tool: "turnip", operation: "diff", projects: []string{"aws/vpc"},
			wantTargets: []string{"aws/vpc"},
		},
		{
			name: "star targets every configured project",
			tool: "turnip", operation: "diff", projects: []string{"*"},
			wantTargets:  []string{"gcp/vpc", "gcp/gke", "aws/vpc"},
			wantRejected: 1,
		},
		{
			name: "star still respects the tool filter",
			tool: "helmfile", operation: "diff", projects: []string{"*"},
			wantTargets: []string{"gcp/vpc", "gcp/gke", "aws/vpc"},
		},
		{
			name: "bare apply targets what this pull request planned",
			tool: "helmfile", operation: "apply",
			wantTargets: []string{"gcp/vpc"},
		},
		{
			name: "named apply reaches a project holding no plan",
			tool: "helmfile", operation: "apply", projects: []string{"aws/vpc"},
			wantTargets: []string{"aws/vpc"},
		},
		{
			name: "pattern selects by name",
			tool: "helmfile", operation: "diff", projects: []string{"gcp/*"},
			wantTargets: []string{"gcp/vpc", "gcp/gke"},
		},
		{
			name: "patterns union",
			tool: "helmfile", operation: "diff", projects: []string{"gcp/*", "aws/*"},
			wantTargets: []string{"gcp/vpc", "gcp/gke", "aws/vpc"},
		},
		{
			name: "name and pattern compose",
			tool: "helmfile", operation: "diff", projects: []string{"aws/vpc", "gcp/*"},
			wantTargets: []string{"gcp/vpc", "gcp/gke", "aws/vpc"},
		},
		{
			name: "unmatched pattern is reported, not fatal",
			tool: "helmfile", operation: "diff", projects: []string{"aws/vpc", "gpc/*"},
			wantTargets: []string{"aws/vpc"},
			wantNotices: []string{"gpc/*"},
		},
		{
			name: "star combined with a name is refused",
			tool: "turnip", operation: "diff", projects: []string{"aws/vpc", "*"},
			wantErrIs: ErrMixedSelector,
		},
		{
			name: "named project that does not exist fails the command",
			tool: "turnip", operation: "diff", projects: []string{"nope"},
			wantErrIs: ErrUnmatchedProject,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sel := testSelection(selectorConfig(), github.NewAuthorizer(&fakeAuthClient{permission: "write"}))
			sel.prNumber = 42
			sel.modifiedSet = func(context.Context) ([]string, error) { return modified, nil }
			sel.locks = &fakeLockManager{
				getLockStatusFunc: func(_ context.Context, key string) (*lock.LockStatus, error) {
					for name, held := range heldPlans {
						if key == projectKey("o", "r", name) {
							return &lock.LockStatus{Locked: held, PRNumber: 42, HasPlan: held}, nil
						}
					}
					return &lock.LockStatus{}, nil
				},
			}

			targets, rejected, notices, err := sel.resolve(context.Background(),
				&github.TriggerCommand{Tool: tt.tool, Operation: tt.operation, Projects: tt.projects})

			if tt.wantErrIs != nil {
				require.Error(t, err)
				assert.ErrorIs(t, err, tt.wantErrIs)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantTargets, names(targets))
			assert.Len(t, rejected, tt.wantRejected)
			require.Len(t, notices, len(tt.wantNotices))
			for i, want := range tt.wantNotices {
				assert.Contains(t, notices[i], want)
			}
		})
	}
}

// The regression that motivated making "*" a reserved word rather than a
// pattern. doublestar's "*" stops at a separator, so evaluating it as a
// glob silently drops every Project named for its path — the exact naming
// convention that makes patterns worth having. This fails if anyone ever
// "simplifies" the reserved word into a pattern.
func TestResolveTargets_StarReachesPathShapedNames(t *testing.T) {
	sel := testSelection(selectorConfig(), github.NewAuthorizer(&fakeAuthClient{permission: "write"}))

	targets, _, _, err := sel.resolve(context.Background(),
		&github.TriggerCommand{Tool: "helmfile", Operation: "diff", Projects: []string{"*"}})

	require.NoError(t, err)
	assert.Equal(t, []string{"gcp/vpc", "gcp/gke", "aws/vpc"}, names(targets),
		`"*" must reach every project, including those whose names contain a separator`)
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
