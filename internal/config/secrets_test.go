package config

import (
	"encoding/json"
	"strings"
	"testing"

	yaml "go.yaml.in/yaml/v3"
	"k8s.io/apimachinery/pkg/util/validation"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

const invalidKeyMessage = `is not a valid Secret key: use up to 253 letters, digits, "-", "_" or ".", other than "." and not starting with ".."`

func TestParse_SecretsAbsentEmptyAndSetAtBothLevels(t *testing.T) {
	cfg, err := Parse(runnerConfigYAML(
		"  secrets: [shared-kubeconfig]\n",
		"      secrets: []\n",
		"      secrets: [api-kubeconfig]\n"), testCatalog)
	require.NoError(t, err)

	assert.Equal(t, SecretKeys{"shared-kubeconfig"}, cfg.Runner.Secrets)
	require.NotNil(t, cfg.Projects[0].Runner.Secrets, "an empty list is an opt-out, not absent")
	assert.Empty(t, cfg.Projects[0].Runner.Secrets)
	assert.Equal(t, SecretKeys{"api-kubeconfig"}, cfg.Projects[1].Runner.Secrets)

	cfg, err = Parse(runnerConfigYAML("  secrets: [shared]\n", "", ""), testCatalog)
	require.NoError(t, err)
	assert.Equal(t, SecretKeys{"shared"}, cfg.Projects[0].Runner.Secrets)
	assert.Equal(t, SecretKeys{"shared"}, cfg.Projects[1].Runner.Secrets)

	cfg, err = Parse(runnerConfigYAML("", "", ""), testCatalog)
	require.NoError(t, err)
	assert.Nil(t, cfg.Projects[0].Runner.Secrets)
}

func TestSecretKeys_AbsentAndEmptyStayDistinct(t *testing.T) {
	for name, tc := range map[string]struct {
		in    SecretKeys
		empty bool
	}{
		"absent": {nil, false},
		"empty":  {SecretKeys{}, true},
		"set":    {SecretKeys{"a"}, false},
	} {
		t.Run(name, func(t *testing.T) {
			r := RunnerSpec{Secrets: tc.in}

			data, err := yaml.Marshal(r)
			require.NoError(t, err)
			var fromYAML RunnerSpec
			require.NoError(t, yaml.Unmarshal(data, &fromYAML))
			assert.Equal(t, tc.in, fromYAML.Secrets, "yaml: %s", data)

			data, err = json.Marshal(r)
			require.NoError(t, err)
			var fromJSON RunnerSpec
			require.NoError(t, json.Unmarshal(data, &fromJSON))
			assert.Equal(t, tc.in, fromJSON.Secrets, "json: %s", data)
		})
	}
}

func TestParse_InheritedSecretsAreNotShared(t *testing.T) {
	cfg, err := Parse(runnerConfigYAML("  secrets: [a, b]\n", "", ""), testCatalog)
	require.NoError(t, err)

	cfg.Projects[0].Runner.Secrets[0] = "changed"

	assert.Equal(t, SecretKeys{"a", "b"}, cfg.Projects[1].Runner.Secrets)
	assert.Equal(t, SecretKeys{"a", "b"}, cfg.Runner.Secrets)
}

// Feature: repository-secrets, Property 1: Effective secrets list
func TestProperty_EffectiveSecretsIsProjectsWhenPresent(t *testing.T) {
	gen := rapid.Custom(func(t *rapid.T) SecretKeys {
		switch rapid.IntRange(0, 2).Draw(t, "form") {
		case 0:
			return nil
		case 1:
			return SecretKeys{}
		}
		return rapid.SliceOfN(rapid.StringMatching("[a-z]{1,6}"), 1, 4).Draw(t, "keys")
	})
	rapid.Check(t, func(t *rapid.T) {
		top, project := gen.Draw(t, "top"), gen.Draw(t, "project")

		got := mergeRunner(RunnerSpec{Secrets: top}, RunnerSpec{Secrets: project}).Secrets

		want := top
		if project != nil {
			want = project
		}
		assert.Equal(t, want, got)
		assert.Equal(t, want == nil, got == nil, "nil versus empty must be preserved")
	})
}

func TestParse_SecretKeyValidationMessages(t *testing.T) {
	_, err := Parse(runnerConfigYAML("", "      secrets: [clusters/poc]\n", ""), testCatalog)
	require.Error(t, err)
	assert.Equal(t, `config: web: runner.secrets[0]: "clusters/poc" `+invalidKeyMessage, err.Error())

	_, err = Parse(runnerConfigYAML("  secrets: [poc-kubeconfig, poc-kubeconfig]\n", "", ""), testCatalog)
	require.Error(t, err)
	verrs := asValidationErrors(t, err)
	require.Len(t, verrs, 1, "reported once, not once per Project: %v", verrs)
	assert.Equal(t, `config: turnip.yaml: runner.secrets[1]: "poc-kubeconfig" is already listed`, err.Error())
}

func TestValidateRunnerSecrets(t *testing.T) {
	long := strings.Repeat("a", 253)
	tests := []struct {
		name string
		keys SecretKeys
		want []string
	}{
		{"valid", SecretKeys{"lab-kubeconfig", "a_b.c", ".hidden", "a..b", long}, nil},
		{"empty list", SecretKeys{}, nil},
		{"absent", nil, nil},
		{"empty key", SecretKeys{""}, []string{`runner.secrets[0]: "" ` + invalidKeyMessage}},
		{"dot", SecretKeys{"."}, []string{`runner.secrets[0]: "." ` + invalidKeyMessage}},
		{"double dot", SecretKeys{".."}, []string{`runner.secrets[0]: ".." ` + invalidKeyMessage}},
		{"leading double dot", SecretKeys{"..a"}, []string{`runner.secrets[0]: "..a" ` + invalidKeyMessage}},
		{"space", SecretKeys{"ok", "a b"}, []string{`runner.secrets[1]: "a b" ` + invalidKeyMessage}},
		{"too long", SecretKeys{long + "a"}, []string{`runner.secrets[0]: "` + long + `a" ` + invalidKeyMessage}},
		{"duplicate on second entry", SecretKeys{"a", "b", "a"}, []string{`runner.secrets[2]: "a" is already listed`}},
		{"everything is reported", SecretKeys{"a/b", "x", "x", "c d"}, []string{
			`runner.secrets[0]: "a/b" ` + invalidKeyMessage,
			`runner.secrets[2]: "x" is already listed`,
			`runner.secrets[3]: "c d" ` + invalidKeyMessage,
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var got []string
			for _, e := range validateRunnerSecrets(tc.keys, "web") {
				got = append(got, e.Field+": "+e.Message)
			}
			assert.Equal(t, tc.want, got)
		})
	}
}

func kubernetesAcceptsKey(key string) bool {
	return len(validation.IsConfigMapKey(key)) == 0
}

// The rule is written out in this package so that it stays free of
// Kubernetes imports; this keeps it from drifting from Kubernetes' own.
func TestValidSecretKey_AgreesWithKubernetes(t *testing.T) {
	corpus := []string{
		"", ".", "..", "..a", "a..b", ".a", "a.", "a/b", "a b", "a\nb", "é", "日本", "a-b_c.d",
		"-", "_", "A9", strings.Repeat("a", 253), strings.Repeat("a", 254),
		"." + strings.Repeat("a", 252), "..", "...",
	}
	for _, key := range corpus {
		assert.Equalf(t, kubernetesAcceptsKey(key), validSecretKey(key), "key %q", key)
	}
}

// Feature: repository-secrets, Property 2: Key rule matches Kubernetes
func TestProperty_ValidSecretKeyAgreesWithKubernetes(t *testing.T) {
	alphabet := []rune("abzAZ09-_.. /é\n")
	rapid.Check(t, func(t *rapid.T) {
		key := rapid.StringOfN(rapid.SampledFrom(alphabet), 0, 8, -1).Draw(t, "key")
		assert.Equalf(t, kubernetesAcceptsKey(key), validSecretKey(key), "key %q", key)
	})
}
