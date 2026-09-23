package plugin

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseChangedReleases(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   int
	}{
		{
			name:   "empty input",
			output: "",
			want:   0,
		},
		{
			name:   "no changed releases",
			output: "Comparing release=a, chart=charts/a\nComparing release=b, chart=charts/b\n",
			want:   0,
		},
		{
			name:   "one changed release",
			output: "Comparing release=a, chart=charts/a\ndefault, a, Deployment (apps) has changed:\n  -replicas: 1\n  +replicas: 2\n",
			want:   1,
		},
		{
			name: "multiple changed releases",
			output: "Comparing release=a, chart=charts/a\n" +
				"default, a, Deployment (apps) has changed:\n  -replicas: 1\n  +replicas: 2\n" +
				"Comparing release=b, chart=charts/b\n" +
				"default, b, ConfigMap has changed:\n  -foo: bar\n  +foo: baz\n",
			want: 2,
		},
		{
			name: "mix of changed and unchanged releases",
			output: "Comparing release=a, chart=charts/a\n" +
				"Comparing release=b, chart=charts/b\n" +
				"default, b, ConfigMap has changed:\n  -foo: bar\n  +foo: baz\n" +
				"Comparing release=c, chart=charts/c\n",
			want: 1,
		},
		{
			name:   "no Comparing headers at all",
			output: "some unrelated output\nwith no release headers\n",
			want:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, parseChangedReleases(tt.output))
		})
	}
}

// The regression the shared annotation prefix exists to prevent.
//
// parseChangedReleases counts a release as changed when any non-empty line
// follows its "Comparing release=" line. The transcript's trailer follows
// the *last* release, so without skipping turnip's own lines a diff whose
// final release is unchanged would report it as changed — silently
// inflating the count a reviewer reads.
func TestParseChangedReleases_IgnoresTurnipsOwnAnnotations(t *testing.T) {
	withTranscript := strings.Join([]string{
		"@@ turnip: /turnip/src/env, helmfile v0.169.0 @@",
		"@@ turnip: helmfile --environment staging diff @@",
		"Comparing release=web, chart=charts/web",
		"web, Deployment (apps) has changed:",
		"  some diff body",
		"Comparing release=api, chart=charts/api",
		"@@ turnip: exit 0 in 1.2s @@",
	}, "\n")

	assert.Equal(t, 1, parseChangedReleases(withTranscript),
		"api reported no body of its own; only the trailer followed it")
}
