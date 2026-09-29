package config

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runnerConfigYAML builds a file with an optional top-level block and two
// Projects, web and api, each with an optional runner: block. Every block
// is given already indented for its position.
func runnerConfigYAML(top, web, api string) []byte {
	s := "schemaVersion: " + SupportedSchemaVersion + "\n"
	if top != "" {
		s += "runner:\n" + top
	}
	s += "projects:\n" +
		"  - name: web\n" +
		"    directory: infra/web\n" +
		"    uses: helmfile@v1.7.4\n"
	if web != "" {
		s += "    runner:\n" + web
	}
	s += "  - name: api\n" +
		"    directory: infra/api\n" +
		"    uses: helmfile@v1.7.4\n"
	if api != "" {
		s += "    runner:\n" + api
	}
	return []byte(s)
}

// Requirement 1.3: a file without the key parses exactly as before.
func TestParse_NoTopLevelRunnerLeavesProjectsAsWritten(t *testing.T) {
	cfg, err := Parse(runnerConfigYAML("",
		"      serviceAccount: web-sa\n      env:\n        A: \"1\"\n",
		""), testCatalog)
	require.NoError(t, err)

	assert.Equal(t, RunnerSpec{}, cfg.Runner)
	assert.Equal(t, RunnerSpec{ServiceAccount: "web-sa", Env: map[string]string{"A": "1"}}, cfg.Projects[0].Runner)
	assert.Equal(t, ServiceAccountSourceProject, cfg.Projects[0].ServiceAccountSource)
	assert.Equal(t, RunnerSpec{}, cfg.Projects[1].Runner)
	assert.Nil(t, cfg.Projects[1].Runner.Env, "an absent env stays nil at both levels")
	assert.Equal(t, ServiceAccountSourceNone, cfg.Projects[1].ServiceAccountSource)
}

// Requirements 1.1 and 1.2: the top-level block accepts each field a
// Project's does, and is kept on Config as written.
func TestParse_TopLevelRunnerAcceptsEveryField(t *testing.T) {
	cfg, err := Parse(runnerConfigYAML(
		"  serviceAccount: shared-sa\n  env:\n    KUBECONFIG: /turnip/src/.turnip/kubeconfig\n",
		"", ""), testCatalog)
	require.NoError(t, err)

	assert.Equal(t, RunnerSpec{
		ServiceAccount: "shared-sa",
		Env:            map[string]string{"KUBECONFIG": "/turnip/src/.turnip/kubeconfig"},
	}, cfg.Runner)
}

// Requirement 2: serviceAccount is replaced, the most specific level
// winning, and the merge records which level that was.
func TestParse_ServiceAccountMostSpecificWins(t *testing.T) {
	tests := []struct {
		name       string
		top, web   string
		wantSA     string
		wantSource ServiceAccountSource
	}{
		{"neither", "", "", "", ServiceAccountSourceNone},
		{"project only", "", "      serviceAccount: web-sa\n", "web-sa", ServiceAccountSourceProject},
		{"top level only", "  serviceAccount: shared-sa\n", "", "shared-sa", ServiceAccountSourceTopLevel},
		{"both", "  serviceAccount: shared-sa\n", "      serviceAccount: web-sa\n", "web-sa", ServiceAccountSourceProject},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := Parse(runnerConfigYAML(tc.top, tc.web, ""), testCatalog)
			require.NoError(t, err)

			web := cfg.Projects[0]
			assert.Equal(t, tc.wantSA, web.Runner.ServiceAccount)
			assert.Equal(t, tc.wantSource, web.ServiceAccountSource)
		})
	}
}

// Requirement 3: env is merged per variable.
func TestParse_EnvMergedPerKey(t *testing.T) {
	tests := []struct {
		name     string
		top, web string
		want     map[string]string
	}{
		{"neither", "", "", nil},
		{"project only", "", "      env:\n        A: p\n", map[string]string{"A": "p"}},
		{"top level only", "  env:\n    A: t\n", "", map[string]string{"A": "t"}},
		{
			"project overrides one and keeps the rest",
			"  env:\n    A: t\n    B: t\n",
			"      env:\n        B: p\n        C: p\n",
			map[string]string{"A": "t", "B": "p", "C": "p"},
		},
		{
			"an empty string overrides",
			"  env:\n    A: t\n",
			"      env:\n        A: \"\"\n",
			map[string]string{"A": ""},
		},
		{
			"no value overrides with the empty string",
			"  env:\n    A: t\n",
			"      env:\n        A:\n",
			map[string]string{"A": ""},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := Parse(runnerConfigYAML(tc.top, tc.web, ""), testCatalog)
			require.NoError(t, err)

			assert.Equal(t, tc.want, cfg.Projects[0].Runner.Env)
		})
	}
}

// Every Project that inherits the top-level env gets a map of its own:
// changing one must not reach the other, nor the top-level block.
func TestParse_InheritedEnvIsNotShared(t *testing.T) {
	cfg, err := Parse(runnerConfigYAML("  env:\n    A: t\n", "", ""), testCatalog)
	require.NoError(t, err)

	cfg.Projects[0].Runner.Env["A"] = "changed"

	assert.Equal(t, "t", cfg.Projects[1].Runner.Env["A"])
	assert.Equal(t, "t", cfg.Runner.Env["A"])
}

// The strict pass covers the new block like any other.
func TestParse_UnknownKeyInsideTopLevelRunnerIsRejected(t *testing.T) {
	_, err := Parse(runnerConfigYAML("  serviceAcount: typo\n", "", ""), testCatalog)

	require.Error(t, err)
	verrs := asValidationErrors(t, err)
	require.Len(t, verrs, 1)
	assert.Equal(t, "serviceAcount", verrs[0].Field)
	assert.Equal(t, configFileRef, verrs[0].ProjectRef)
}

// Requirement 4: a reserved name at the top level is reported once,
// against the file, in the same pass as a Project's own violation.
func TestParse_TopLevelEnvViolationReportedOnceBesideProjects(t *testing.T) {
	_, err := Parse(runnerConfigYAML(
		"  env:\n    TURNIP_SERVER_ADDR: elsewhere\n",
		"      env:\n        PATH: /nowhere\n",
		""), testCatalog)

	require.Error(t, err)
	verrs := asValidationErrors(t, err)
	require.Len(t, verrs, 2, "one per violation, not one per inheriting Project: %v", verrs)

	got := make([][2]string, len(verrs))
	for i, v := range verrs {
		got[i] = [2]string{v.ProjectRef, v.Field}
	}
	assert.ElementsMatch(t, [][2]string{
		{"web", `runner.env["PATH"]`},
		{configFileRef, `runner.env["TURNIP_SERVER_ADDR"]`},
	}, got)
}

// Requirement 6.2: every RunnerSpec field has a merge rule, so a field
// added later cannot land without someone choosing one.
func TestRunnerMergeRules_CoverEveryField(t *testing.T) {
	assert.Empty(t, unruledFields(reflect.TypeFor[RunnerSpec](), runnerMergeRules),
		"every RunnerSpec field needs an entry in runnerMergeRules")

	for name, rule := range runnerMergeRules {
		f, ok := reflect.TypeFor[RunnerSpec]().FieldByName(name)
		if assert.Truef(t, ok, "runnerMergeRules names %q, which RunnerSpec does not have", name) &&
			rule == mergePerKey {
			assert.Equalf(t, reflect.Map, f.Type.Kind(), "%q is merged per key, so it must be a map", name)
		}
	}
}

// The field walk must report a field that has no rule; otherwise the test
// above could pass by checking nothing.
func TestRunnerMergeRules_ReportAnUnlistedField(t *testing.T) {
	type runnerWithUnlistedField struct {
		ServiceAccount string
		Env            map[string]string
		Tolerations    []string
	}

	assert.Equal(t, []string{"Tolerations"},
		unruledFields(reflect.TypeFor[runnerWithUnlistedField](), runnerMergeRules))
}
