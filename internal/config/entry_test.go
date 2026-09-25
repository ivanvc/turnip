package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var entryTools = []string{"helmfile", "terraform"}

func TestParseEntry_Valid(t *testing.T) {
	tests := []struct {
		line string
		want Entry
	}{
		{"helmfile:ghcr.io/org/helmfile-aws@*", Entry{Tool: "helmfile", Image: "ghcr.io/org/helmfile-aws", Glob: "*"}},
		{"helmfile:ghcr.io/org/helmfile-aws@latest", Entry{Tool: "helmfile", Image: "ghcr.io/org/helmfile-aws", Glob: "latest"}},
		{"helmfile:ghcr.io/helmfile/helmfile@v*.*.*", Entry{Tool: "helmfile", Image: "ghcr.io/helmfile/helmfile", Glob: "v*.*.*"}},
		{"helmfile:ghcr.io/helmfile/helmfile@sha256:*", Entry{Tool: "helmfile", Image: "ghcr.io/helmfile/helmfile", Glob: "sha256:*"}},
		{"terraform:docker.io/hashicorp/terraform@1.*.*", Entry{Tool: "terraform", Image: "docker.io/hashicorp/terraform", Glob: "1.*.*"}},
		// A registry port puts a ":" before the first "/"; only the text
		// before the first ":" is the tool.
		{"helmfile:registry.local:5000/team/helmfile@*", Entry{Tool: "helmfile", Image: "registry.local:5000/team/helmfile", Glob: "*"}},
		{"helmfile:localhost/helmfile@*", Entry{Tool: "helmfile", Image: "localhost/helmfile", Glob: "*"}},
		{"helmfile:localhost:5000/helmfile@1.?", Entry{Tool: "helmfile", Image: "localhost:5000/helmfile", Glob: "1.?"}},
		{"  helmfile:ghcr.io/org/helmfile@[0-9]*  ", Entry{Tool: "helmfile", Image: "ghcr.io/org/helmfile", Glob: "[0-9]*"}},
	}
	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			got, err := ParseEntry(tt.line, entryTools)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParseEntry_StringRoundTrip(t *testing.T) {
	line := "helmfile:registry.local:5000/team/helmfile@sha256:*"
	e, err := ParseEntry(line, entryTools)
	require.NoError(t, err)
	assert.Equal(t, line, e.String())
}

func TestParseEntry_Refused(t *testing.T) {
	tests := []struct {
		name    string
		line    string
		wantMsg string
	}{
		// The tool before the first ":" is not a registered Tool_Name.
		{"unknown tool", "pulumi:docker.io/pulumi/pulumi-base@*", `"pulumi" is not a tool turnip runs (one of: helmfile, terraform)`},
		{"registry port read as a tool", "registry.local:5000/team/helmfile@*", `"registry.local" is not a tool turnip runs`},
		{"image with no tool", "ghcr.io/org/helmfile-aws@*", `"ghcr.io/org/helmfile-aws@*"`},

		// Unqualified images.
		{"bare name", "helmfile:helmfile@*", `image "helmfile" is not fully qualified`},
		{"docker hub namespace", "helmfile:org/helmfile-aws@*", `image "org/helmfile-aws" is not fully qualified`},
		{"host alone", "helmfile:ghcr.io@*", `image "ghcr.io" is not fully qualified`},

		// An image carrying its own tag or digest.
		{"own tag", "helmfile:ghcr.io/org/helmfile-aws:1.7.4@*", `image "ghcr.io/org/helmfile-aws:1.7.4" carries a tag of its own`},
		{"own tag behind a port", "helmfile:registry.local:5000/helmfile:latest@*", `image "registry.local:5000/helmfile:latest" carries a tag of its own`},
		{"own digest", "helmfile:ghcr.io/org/helmfile-aws@sha256:" + hex64 + "@*", `carries a digest of its own`},

		// Malformed lines.
		{"empty", "", "must be tool:image@glob"},
		{"no colon", "helmfile", "must be tool:image@glob"},
		{"empty tool", ":ghcr.io/org/helmfile@*", "must be tool:image@glob"},
		{"no glob", "helmfile:ghcr.io/org/helmfile-aws", "name the Tag_Specs it allows after an @"},
		{"empty glob", "helmfile:ghcr.io/org/helmfile-aws@", "the glob is empty"},
		{"empty image", "helmfile:@*", "the image is empty"},
		{"bad glob", "helmfile:ghcr.io/org/helmfile-aws@[", `glob "[" is malformed`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseEntry(tt.line, entryTools)
			require.Error(t, err)
			var entryErr *EntryError
			require.ErrorAs(t, err, &entryErr)
			assert.Contains(t, err.Error(), tt.wantMsg)
			assert.Equal(t, tt.line, entryErr.Line, "the error names the offending line")
		})
	}
}

const hex64 = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestImageConflicts(t *testing.T) {
	helmfileAlias := []Entry{
		{Tool: "helmfile", Image: "ghcr.io/helmfile/helmfile", Glob: "v*.*.*"},
		{Tool: "helmfile", Image: "ghcr.io/helmfile/helmfile", Glob: "sha256:*"},
	}

	t.Run("none", func(t *testing.T) {
		c := Catalog{
			Aliases: map[string][]Entry{"helmfile": helmfileAlias},
			Allowed: []Entry{
				// The same tool may list an image again, overlapping freely.
				{Tool: "helmfile", Image: "ghcr.io/helmfile/helmfile", Glob: "latest"},
				{Tool: "helmfile", Image: "ghcr.io/org/helmfile-aws", Glob: "*"},
				{Tool: "helmfile", Image: "ghcr.io/org/helmfile-aws", Glob: "1.*"},
				{Tool: "terraform", Image: "docker.io/hashicorp/terraform", Glob: "1.*.*"},
			},
		}
		assert.Empty(t, ImageConflicts(c))
	})

	t.Run("empty catalog", func(t *testing.T) {
		assert.Empty(t, ImageConflicts(Catalog{}))
	})

	t.Run("access list against an alias", func(t *testing.T) {
		c := Catalog{
			Aliases: map[string][]Entry{"helmfile": helmfileAlias},
			Allowed: []Entry{{Tool: "terraform", Image: "ghcr.io/helmfile/helmfile", Glob: "latest"}},
		}
		assert.Equal(t, []ImageConflict{
			{Image: "ghcr.io/helmfile/helmfile", Tools: []string{"helmfile", "terraform"}},
		}, ImageConflicts(c))
	})

	t.Run("every conflict reported, sorted", func(t *testing.T) {
		c := Catalog{
			Aliases: map[string][]Entry{"helmfile": helmfileAlias},
			Allowed: []Entry{
				{Tool: "terraform", Image: "ghcr.io/org/multi", Glob: "tf-*"},
				{Tool: "helmfile", Image: "ghcr.io/org/multi", Glob: "hf-*"},
				{Tool: "pulumi", Image: "ghcr.io/org/multi", Glob: "pu-*"},
				{Tool: "pulumi", Image: "docker.io/org/a", Glob: "*"},
				{Tool: "terraform", Image: "docker.io/org/a", Glob: "*"},
				{Tool: "terraform", Image: "docker.io/org/b", Glob: "*"},
			},
		}
		got := ImageConflicts(c)
		assert.Equal(t, []ImageConflict{
			{Image: "docker.io/org/a", Tools: []string{"pulumi", "terraform"}},
			{Image: "ghcr.io/org/multi", Tools: []string{"helmfile", "pulumi", "terraform"}},
		}, got)
		require.Len(t, got, 2)
		assert.EqualError(t, got[0], `config: image "docker.io/org/a" is listed for more than one tool (pulumi, terraform)`)
	})
}
