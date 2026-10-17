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

// updatedHeader and deletedHeader are Helmfile's real tabwriter-rendered
// header rows for each table shape.
const (
	updatedHeader = "NAME   NAMESPACE   CHART   VERSION   DURATION"
	deletedHeader = "NAME   NAMESPACE   DURATION"
)

// table joins a banner line, a header row and its release rows into one
// Release_Table, framed by a blank line after: the shape Helmfile's
// logger writes each table as (design's "What Helmfile prints").
func table(banner, header string, rows ...string) string {
	lines := append([]string{banner, header}, rows...)
	return strings.Join(lines, "\n") + "\n\n"
}

// fixedBanner and centeredBanner are the two "="-framed widths Helmfile
// has printed from v1.6.0 on: a fixed frame (v1.6.0 - v1.7.2) and one
// centered on the table (v1.7.3 on).
func fixedBanner(title string) string    { return "========== " + title + " ==========" }
func centeredBanner(title string) string { return "==== " + title + " ====" }

// sgr wraps text in the bold-blue ANSI SGR sequence Helmfile's logger uses
// for the banner, unless color is off.
func sgr(text string) string { return "\x1b[1;34m" + text + "\x1b[0m" }

func TestParseReleaseTables(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   ChangeSummary
	}{
		{
			name:   "no table",
			output: "Building dependency release=web, chart=charts/web\nUpgraded release=web\n",
			want:   ChangeSummary{},
		},
		{
			name:   "one updated table, fixed-width banner",
			output: table(fixedBanner("Updated Releases"), updatedHeader, "web   default   example.com/web   1.2.3   1s"),
			want:   ChangeSummary{Change: 1},
		},
		{
			name:   "one updated table, centered banner",
			output: table(centeredBanner("Updated Releases"), updatedHeader, "web   default   example.com/web   1.2.3   1s"),
			want:   ChangeSummary{Change: 1},
		},
		{
			name: "several tables, one per sub-helmfile, summed",
			output: table(fixedBanner("Updated Releases"), updatedHeader, "web   default   example.com/web   1.2.3   1s") +
				table(fixedBanner("Updated Releases"), updatedHeader,
					"api   default   example.com/api   4.5.6   2s",
					"queue   default   example.com/queue   7.8.9   1s"),
			want: ChangeSummary{Change: 3},
		},
		{
			name:   "a deleted table",
			output: table(fixedBanner("Deleted Releases"), deletedHeader, "web   default   1s"),
			want:   ChangeSummary{Destroy: 1},
		},
		{
			name:   "a reinstalled table counts as changed",
			output: table(fixedBanner("Reinstalled Releases"), updatedHeader, "web   default   example.com/web   1.2.3   1s"),
			want:   ChangeSummary{Change: 1},
		},
		{
			name: "a failed table beside an updated one: only the updated rows count",
			output: table(fixedBanner("Updated Releases"), updatedHeader, "web   default   example.com/web   1.2.3   1s") +
				table(fixedBanner("Failed Releases"), updatedHeader, "api   default   example.com/api   4.5.6   1s"),
			want: ChangeSummary{Change: 1},
		},
		{
			name: "a failed-to-delete table is not counted",
			output: table(fixedBanner("Deleted Releases"), deletedHeader, "web   default   1s") +
				table(fixedBanner("Failed to Delete Releases"), deletedHeader, "api   default   1s"),
			want: ChangeSummary{Destroy: 1},
		},
		{
			name:   "fixed banner with color codes",
			output: table(sgr(fixedBanner("Updated Releases")), updatedHeader, "web   default   example.com/web   1.2.3   1s"),
			want:   ChangeSummary{Change: 1},
		},
		{
			name:   "centered banner with color codes",
			output: table(sgr(centeredBanner("Updated Releases")), updatedHeader, "web   default   example.com/web   1.2.3   1s"),
			want:   ChangeSummary{Change: 1},
		},
		{
			name:   "the older upper-case banner is not the framed banner and counts nothing",
			output: "UPDATED RELEASES:\n" + updatedHeader + "\nweb   default   example.com/web   1.2.3   1s\n\n",
			want:   ChangeSummary{},
		},
		{
			name: "a failed table right after an updated one, with no blank line between: its rows are not the updated table's",
			output: fixedBanner("Updated Releases") + "\n" + updatedHeader + "\n" +
				"web   default   example.com/web   1.2.3   1s\n" +
				fixedBanner("Failed Releases") + "\n" + updatedHeader + "\n" +
				"api   default   example.com/api   4.5.6   1s\n\n",
			want: ChangeSummary{Change: 1},
		},
		{
			name:   "the older reinstalled banner counts nothing",
			output: "REINSTALLED RELEASES:\n" + updatedHeader + "\nweb   default   example.com/web   1.2.3   1s\n\n",
			want:   ChangeSummary{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, parseReleaseTables(tt.output))
		})
	}
}

func TestBannerTitle(t *testing.T) {
	title, ok := bannerTitle("========== Updated Releases ==========")
	assert.True(t, ok)
	assert.Equal(t, "Updated Releases", title)

	_, ok = bannerTitle("UPDATED RELEASES:")
	assert.False(t, ok, "the older banner carries no = frame")

	_, ok = bannerTitle("")
	assert.False(t, ok)
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

func TestParseAffectedReleases(t *testing.T) {
	const header = "Affected releases are:\n"
	tests := []struct {
		name   string
		output string
		want   int
	}{
		{name: "no summary", output: "Building dependency release=web\n", want: 0},
		{name: "one removal", output: header + "  web (example/web) DELETED\n\n", want: 1},
		{
			name:   "two removals",
			output: header + "  api (example/api) DELETED\n  web (example/web) DELETED\n\n",
			want:   2,
		},
		{
			name: "two summaries are summed",
			output: header + "  web (example/web) DELETED\n\n" +
				"Building dependency release=api\n" +
				header + "  api (example/api) DELETED\n\n",
			want: 2,
		},
		{
			name:   "updated lines are not counted",
			output: header + "  api (example/api) UPDATED\n  web (example/web) DELETED\n\n",
			want:   1,
		},
		{
			name:   "only updated lines",
			output: header + "  api (example/api) UPDATED\n\n",
			want:   0,
		},
		{
			name:   "a summary ends at another log line",
			output: header + "  web (example/web) DELETED\nsome other log line\n  api (example/api) DELETED\n",
			want:   1,
		},
		{
			name:   "release lines without a header are not counted",
			output: "  web (example/web) DELETED\n",
			want:   0,
		},
		{
			name:   "color codes",
			output: sgr(header) + "  web (example/web) " + sgr("DELETED") + "\n",
			want:   1,
		},
		{name: "empty summary", output: header + "\n", want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, parseAffectedReleases(tt.output))
		})
	}
}
