package plugin

import "testing"

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
			if got := parseChangedReleases(tt.output); got != tt.want {
				t.Errorf("parseChangedReleases() = %d, want %d", got, tt.want)
			}
		})
	}
}
