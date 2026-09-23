package orchestrator

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivanvc/turnip/internal/config"
	"github.com/ivanvc/turnip/internal/github"
	"github.com/ivanvc/turnip/internal/jobs"
)

// fakeScopeClient serves one .gitmodules, or an error, for the scope
// lookup and nothing else.
type fakeScopeClient struct {
	github.GitHubClient
	gitmodules []byte
	err        error
	calls      int
}

func (f *fakeScopeClient) GetFile(_ context.Context, _, _, path, _ string) ([]byte, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	if path != ".gitmodules" {
		return nil, github.ErrFileNotFound
	}
	return f.gitmodules, nil
}

var testScopeRepo = github.Repository{
	Owner: "acme",
	Name:  "infra",
	URL:   "https://github.com/acme/infra.git",
}

func scopeFor(t *testing.T, c *fakeScopeClient, mode string) github.TokenScope {
	t.Helper()
	o := &Orchestrator{}
	return o.tokenScopeFor(context.Background(), c, testScopeRepo, "abc123", mode)
}

func TestTokenScope_NoSubmodulesIsJustTheRepository(t *testing.T) {
	c := &fakeScopeClient{err: github.ErrFileNotFound}
	assert.Equal(t, []string{"infra"}, scopeFor(t, c, config.SubmodulesTopLevel).Repositories)
}

// `none` does not read the repository at all — there is nothing a
// submodule declaration could change.
func TestTokenScope_NoneSkipsTheLookup(t *testing.T) {
	c := &fakeScopeClient{gitmodules: []byte("[submodule \"x\"]\n\turl = ../other.git\n")}
	scope := scopeFor(t, c, config.SubmodulesNone)
	assert.Equal(t, []string{"infra"}, scope.Repositories)
	assert.Zero(t, c.calls, "nothing to read when submodules are off")
}

// The case the singular wording would have broken: a sibling private
// repository, fetched with the same token, in the default mode.
func TestTokenScope_SiblingSubmodulesAreIncluded(t *testing.T) {
	c := &fakeScopeClient{gitmodules: []byte(`
[submodule "shared"]
	path = vendor/shared
	url = ../shared-modules.git
[submodule "charts"]
	path = vendor/charts
	url = https://github.com/acme/helm-charts.git
[submodule "viassh"]
	path = vendor/viassh
	url = git@github.com:acme/ssh-form.git
`)}
	scope := scopeFor(t, c, config.SubmodulesTopLevel)
	assert.Equal(t, []string{"infra", "shared-modules", "helm-charts", "ssh-form"}, scope.Repositories)
}

// Neither of these is reachable with this installation's token however it
// is minted, so naming them would make GitHub reject the mint and — by
// Requirement 1.3 — fail an Operation that would otherwise have run and
// failed only that one submodule.
func TestTokenScope_UnreachableSubmodulesAreLeftOut(t *testing.T) {
	c := &fakeScopeClient{gitmodules: []byte(`
[submodule "elsewhere"]
	url = https://gitlab.com/acme/other.git
[submodule "otheracct"]
	url = https://github.com/other-account/thing.git
`)}
	assert.Equal(t, []string{"infra"}, scopeFor(t, c, config.SubmodulesTopLevel).Repositories)
}

func TestTokenScope_DuplicatesCollapse(t *testing.T) {
	c := &fakeScopeClient{gitmodules: []byte("[a]\nurl = ../infra.git\n[b]\nurl = ../infra.git\n")}
	assert.Equal(t, []string{"infra"}, scopeFor(t, c, config.SubmodulesTopLevel).Repositories)
}

// Requirement 1.4: the nested set is not knowable before the clone, so
// the repository scope stays wide. Permissions are narrowed regardless —
// that half is never unknowable — which internal/github asserts.
func TestTokenScope_RecursiveLeavesRepositoriesWide(t *testing.T) {
	c := &fakeScopeClient{gitmodules: []byte("[a]\nurl = ../shared.git\n")}
	scope := scopeFor(t, c, config.SubmodulesRecursive)
	assert.Nil(t, scope.Repositories, "omitted, not narrowed to the top level turnip happens to see")
	assert.Zero(t, c.calls, "reading it would only produce an incomplete answer")
}

// Widening on a read failure is deliberate, and is not the fallback
// Requirement 1.3 forbids: turnip never obtained a narrow set to fall
// back from. A clone that worked yesterday must not stop working because
// a file could not be read.
func TestTokenScope_UnreadableGitmodulesWidensRatherThanGuesses(t *testing.T) {
	c := &fakeScopeClient{err: errors.New("502 bad gateway")}
	scope := scopeFor(t, c, config.SubmodulesTopLevel)
	assert.Nil(t, scope.Repositories)
}

func TestSubmoduleRepoNames_RemoteForms(t *testing.T) {
	for name, tc := range map[string]struct {
		url  string
		want []string
	}{
		"relative parent":   {"../sibling.git", []string{"sibling"}},
		"relative here":     {"./nested.git", []string{"nested"}},
		"no .git suffix":    {"https://github.com/acme/plain", []string{"plain"}},
		"scp form":          {"git@github.com:acme/scp.git", []string{"scp"}},
		"ssh scheme":        {"ssh://git@github.com/acme/sshscheme.git", []string{"sshscheme"}},
		"different host":    {"https://example.com/acme/nope.git", nil},
		"different account": {"https://github.com/nope/thing.git", nil},
		"unparseable":       {"not a url at all", nil},
	} {
		t.Run(name, func(t *testing.T) {
			got := submoduleRepoNames([]byte("[submodule \"s\"]\n\turl = "+tc.url+"\n"), testScopeRepo)
			assert.Equal(t, tc.want, got)
		})
	}
}

// The scope is computed at dispatch and recorded for the fetch to mint
// with. Dispatch itself mints nothing: the credential must not be live
// through queueing, scheduling and image pulls (Requirement 2.4).
func TestExecuteOne_RecordsTheScopeAndMintsNothing(t *testing.T) {
	locks := &fakeLockManager{}
	jobsClient := &fakeJobCreator{t: t, result: github.ProjectResult{Success: true}}
	o, redisClient := testOrchestrator(t, locks, jobsClient)
	client := &fakeExecuteClient{gitmodules: []byte("[s]\nurl = ../shared.git\n")}

	o.executeOne(context.Background(), client, testRepo, testPR, 1, testHelmfileTarget())

	job := jobsClient.lastCreatedJob()
	require.NotNil(t, job)

	rec, err := newRecordStore(redisClient).get(context.Background(), job.Labels[jobs.OperationIDLabel])
	require.NoError(t, err)
	require.NotNil(t, rec)
	assert.Contains(t, rec.TokenRepositories, testRepo.Name)
	assert.Contains(t, rec.TokenRepositories, "shared",
		"a submodule the clone will fetch has to be in the scope, or the clone fails")

	assert.Empty(t, client.tokenScope.Repositories, "dispatch mints nothing")
}
