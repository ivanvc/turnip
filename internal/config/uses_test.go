package config

import (
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testDigest is a well-formed sha256 Digest, which every test Alias allows.
var testDigest = "sha256:" + strings.Repeat("0123456789abcdef", 4)

// usesYAML is a one-Project turnip.yaml whose uses: line is uses, quoted so
// that "@" and ":" reach the resolver rather than the YAML parser.
func usesYAML(uses string) []byte {
	return []byte("schemaVersion: v1alpha3\nprojects:\n  - name: p\n    directory: d\n    uses: " + strconv.Quote(uses) + "\n")
}

// resolve parses a one-Project file against catalog and returns the
// resolved Project.
func resolve(t *testing.T, uses string, catalog Catalog) Project {
	t.Helper()
	c, err := Parse(usesYAML(uses), catalog)
	require.NoError(t, err)
	require.Len(t, c.Projects, 1)
	return c.Projects[0]
}

// refusal parses a one-Project file against catalog and returns the one
// uses: error it must produce.
func refusal(t *testing.T, uses string, catalog Catalog) string {
	t.Helper()
	_, err := Parse(usesYAML(uses), catalog)
	require.Error(t, err)
	verrs := asValidationErrors(t, err)
	require.Len(t, verrs, 1, "one uses: line reports one problem: %v", verrs)
	assert.Equal(t, "uses", verrs[0].Field)
	return verrs[0].Message
}

// One case per row of the design's resolution table, each with the
// substrings its message must carry.
func TestResolveUses_EachRow(t *testing.T) {
	tests := []struct {
		name string
		uses string
		want []string
	}{
		// Row 1: no "@".
		{"no @, Alias", "helmfile", []string{"names no version", `"helmfile@v1.7.4"`}},
		{"no @, image", "ghcr.io/org/helmfile-aws", []string{"names no version"}},

		// Row 2: a pasted ":tag", corrected to the "@" form.
		{"pasted tag, image", "ghcr.io/org/helmfile-aws:1.7.4", []string{`"ghcr.io/org/helmfile-aws@1.7.4"`}},
		{"pasted tag, Alias", "helmfile:v1.7.4", []string{`"helmfile@v1.7.4"`}},
		{"pasted tag behind a registry port", "registry.local:5000/team/helmfile:1.7.4", []string{`"registry.local:5000/team/helmfile@1.7.4"`}},
		{"pasted tag with a digest after @", "ghcr.io/org/helmfile-aws:1.7.4@" + testDigest, []string{`"ghcr.io/org/helmfile-aws@` + testDigest + `"`}},

		// Row 3: not an Alias, and unqualified.
		{"unqualified with a path", "org/helmfile-aws@1.7.4", []string{`unsupported tool "org/helmfile-aws"`, `one of "helmfile", "pulumi", "terraform"`, "registry host"}},
		{"unqualified single name", "cloudformation@1.0.0", []string{`unsupported tool "cloudformation"`, `one of "helmfile", "pulumi", "terraform"`}},

		// Row 4: a digest that is not sha256, or not 64 hex characters.
		{"other algorithm", "helmfile@sha512:" + strings.Repeat("ab", 64), []string{`"sha512"`, "only sha256"}},
		{"other algorithm, full reference", "ghcr.io/helmfile/helmfile@md5:abc", []string{`"md5"`, "only sha256"}},
		{"short sha256", "helmfile@sha256:abc", []string{"exactly 64"}},
		{"long sha256", "helmfile@" + testDigest + "0", []string{"exactly 64"}},
		{"non-hex sha256", "helmfile@sha256:" + strings.Repeat("g", 64), []string{"exactly 64"}},
		{"uppercase sha256", "helmfile@" + strings.ToUpper(testDigest[:7]) + strings.ToUpper(testDigest[7:]), []string{"only sha256"}},
		{"uppercase hex", "helmfile@sha256:" + strings.ToUpper(testDigest[7:]), []string{"exactly 64"}},

		// Row 5: no Entry matches.
		{"image no Entry lists", "ghcr.io/org/helmfile-aws@1.7.4", []string{`"ghcr.io/org/helmfile-aws@1.7.4"`, "TURNIP_ALLOWED_IMAGES"}},
		{"vendor image, tag its Alias refuses", "ghcr.io/helmfile/helmfile@latest", []string{`"ghcr.io/helmfile/helmfile@latest"`, "TURNIP_ALLOWED_IMAGES"}},
		{"Alias, tag it refuses", "helmfile@1.7.4", []string{`"1.7.4"`, `"v*.*.*"`, `"sha256:*"`, `"helmfile@v1.7.4"`, `"ghcr.io/helmfile/helmfile@1.7.4"`, "TURNIP_ALLOWED_IMAGES"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := refusal(t, tt.uses, testCatalog)
			for _, w := range tt.want {
				assert.Contains(t, msg, w)
			}
		})
	}
}

// The checks run in the table's order and only the first that fails is
// reported.
func TestResolveUses_EarliestRowWins(t *testing.T) {
	tests := []struct {
		name, uses, want string
	}{
		{"pasted tag before unqualified", "org/x:1.0", `"org/x@1.0"`},
		{"pasted tag before digest", "ghcr.io/org/x:1.0@sha512:ab", `"ghcr.io/org/x@sha512:ab"`},
		{"unqualified before digest", "org/x@sha512:ab", `unsupported tool "org/x"`},
		{"digest before no Entry", "ghcr.io/org/x@sha512:ab", "only sha256"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Contains(t, refusal(t, tt.uses, testCatalog), tt.want)
		})
	}
}

// Requirement 2.1, 2.2 and 4.7: an Alias and its vendor's image written in
// full resolve alike, the Tag_Spec kept verbatim and the tool the Entry's.
func TestResolveUses_AliasAndVendorImageInFull(t *testing.T) {
	want := Project{Tool: "helmfile", Image: "ghcr.io/helmfile/helmfile", ToolVersion: "v1.7.4"}
	for _, uses := range []string{"helmfile@v1.7.4", "ghcr.io/helmfile/helmfile@v1.7.4"} {
		t.Run(uses, func(t *testing.T) {
			p := resolve(t, uses, testCatalog)
			assert.Equal(t, want, Project{Tool: p.Tool, Image: p.Image, ToolVersion: p.ToolVersion})
			assert.Equal(t, uses, p.Uses, "uses: itself is kept as written")
		})
	}
}

func TestResolveUses_DigestKeptVerbatim(t *testing.T) {
	for _, uses := range []string{"helmfile@" + testDigest, "ghcr.io/helmfile/helmfile@" + testDigest} {
		p := resolve(t, uses, testCatalog)
		assert.Equal(t, "helmfile", p.Tool)
		assert.Equal(t, "ghcr.io/helmfile/helmfile", p.Image)
		assert.Equal(t, testDigest, p.ToolVersion)
	}
}

// Requirement 3.5: an Alias tries only its own Entries, so helmfile@latest
// is refused even while the Access_List allows the vendor's latest, which
// runs only written out in full.
func TestResolveUses_AliasLatestRefusedWhileVendorLatestInFullAllowed(t *testing.T) {
	catalog := catalogFor(testTools, Entry{Tool: "helmfile", Image: "ghcr.io/helmfile/helmfile", Glob: "latest"})

	msg := refusal(t, "helmfile@latest", catalog)
	assert.Contains(t, msg, `"ghcr.io/helmfile/helmfile@latest"`, "the refusal shows the line written in full")

	p := resolve(t, "ghcr.io/helmfile/helmfile@latest", catalog)
	assert.Equal(t, "helmfile", p.Tool)
	assert.Equal(t, "ghcr.io/helmfile/helmfile", p.Image)
	assert.Equal(t, "latest", p.ToolVersion)
}

// Requirement 4.7: the tool is the one the matching Entry declares, not
// anything the image's name suggests.
func TestResolveUses_ToolFromTheEntry(t *testing.T) {
	catalog := catalogFor(testTools, Entry{Tool: "terraform", Image: "registry.local:5000/team/iac", Glob: "*"})

	p := resolve(t, "registry.local:5000/team/iac@2026-09", catalog)
	assert.Equal(t, "terraform", p.Tool)
	assert.Equal(t, "registry.local:5000/team/iac", p.Image)
	assert.Equal(t, "2026-09", p.ToolVersion)
}

// Requirement 4.3: each glob example, allowed and refused.
func TestResolveUses_GlobExamples(t *testing.T) {
	const image = "ghcr.io/org/helmfile-aws"
	tests := []struct {
		glob    string
		allowed []string
		refused []string
	}{
		{"*", []string{"1.7.4", "v1.7.4", "latest", testDigest}, nil},
		{"1.*.*", []string{"1.16.4", "1.0.0-rc1"}, []string{"1.16", "latest", "2.0.0", "v1.16.4", testDigest}},
		{"latest", []string{"latest"}, []string{"latest2", "1.7.4", testDigest}},
		{"sha256:*", []string{testDigest}, []string{"latest", "1.7.4"}},
		{"1.?", []string{"1.7"}, []string{"1.16", "1."}},
		{"[0-9]*", []string{"1.7.4"}, []string{"v1.7.4", "latest"}},
	}
	for _, tt := range tests {
		catalog := catalogFor(testTools, Entry{Tool: "helmfile", Image: image, Glob: tt.glob})
		for _, tag := range tt.allowed {
			t.Run(tt.glob+" allows "+tag, func(t *testing.T) {
				assert.Equal(t, tag, resolve(t, image+"@"+tag, catalog).ToolVersion)
			})
		}
		for _, tag := range tt.refused {
			t.Run(tt.glob+" refuses "+tag, func(t *testing.T) {
				assert.Contains(t, refusal(t, image+"@"+tag, catalog), "TURNIP_ALLOWED_IMAGES")
			})
		}
	}
}

// An Access_List Entry for the vendor's image adds to what the full
// reference allows; the Alias Entries still apply to it.
func TestResolveUses_FullReferenceTriesAliasAndAllowedEntries(t *testing.T) {
	catalog := catalogFor(testTools, Entry{Tool: "helmfile", Image: "ghcr.io/helmfile/helmfile", Glob: "latest"})

	assert.Equal(t, "v1.7.4", resolve(t, "ghcr.io/helmfile/helmfile@v1.7.4", catalog).ToolVersion)
	assert.Equal(t, "latest", resolve(t, "ghcr.io/helmfile/helmfile@latest", catalog).ToolVersion)
	assert.Contains(t, refusal(t, "ghcr.io/helmfile/helmfile@edge", catalog), "TURNIP_ALLOWED_IMAGES")
}

// An empty Catalog allows nothing: no Alias, no image.
func TestResolveUses_EmptyCatalog(t *testing.T) {
	assert.Contains(t, refusal(t, "helmfile@v1.7.4", Catalog{}), "none are registered")
	assert.Contains(t, refusal(t, "ghcr.io/helmfile/helmfile@v1.7.4", Catalog{}), "TURNIP_ALLOWED_IMAGES")
}

// Every Project is resolved, and every refusal reported in one pass.
func TestResolveUses_EveryProjectReported(t *testing.T) {
	data := []byte(`
schemaVersion: v1alpha3
projects:
  - name: a
    directory: a
    uses: helmfile@latest
  - name: b
    directory: b
    uses: "ghcr.io/org/x:1.0"
  - name: c
    directory: c
    uses: helmfile@v1.7.4
`)
	_, err := Parse(data, testCatalog)
	require.Error(t, err)
	verrs := asValidationErrors(t, err)
	require.Len(t, verrs, 2)
	assert.Equal(t, "a", verrs[0].ProjectRef)
	assert.Equal(t, "b", verrs[1].ProjectRef)
}

// Requirement 2.4: uses: takes one exact Tag, not a pattern or a range, even
// where the pattern would match an Entry's glob (a glob's metacharacters
// match themselves, and "*" matches anything). The Catalog allows every
// tag of ghcr.io/org/x, so only the exact-tag check can refuse these.
func TestResolveUses_PatternOrRangeRefused(t *testing.T) {
	catalog := catalogFor(testTools, Entry{Tool: "helmfile", Image: "ghcr.io/org/x", Glob: "*"})
	tests := []struct {
		name, uses, tag string
	}{
		{"Alias, its own glob", "helmfile@v*.*.*", "v*.*.*"},
		{"Alias, partial glob", "helmfile@v1.*.*", "v1.*.*"},
		{"Alias, trailing whitespace", "helmfile@v1.2.3 ", "v1.2.3 "},
		{"image, star", "ghcr.io/org/x@*", "*"},
		{"image, tilde range", "ghcr.io/org/x@~1.2", "~1.2"},
		{"image, bracket class", "ghcr.io/org/x@[a-z]", "[a-z]"},
		{"image, question mark", "ghcr.io/org/x@v1.?", "v1.?"},
		{"image, inner whitespace", "ghcr.io/org/x@a b", "a b"},
		{"image, leading dot", "ghcr.io/org/x@.1", ".1"},
		{"image, over 128 characters", "ghcr.io/org/x@" + strings.Repeat("a", 129), strings.Repeat("a", 129)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := refusal(t, tt.uses, catalog)
			assert.Contains(t, msg, strconv.Quote(tt.tag)+" is not an exact tag")
			assert.Contains(t, msg, "not a pattern or a range")
		})
	}

	// Exact tags the same Entry allows still resolve.
	for _, tag := range []string{"latest", "1.2.3", "v1.2.3", "_x", strings.Repeat("a", 128)} {
		assert.Equal(t, tag, resolve(t, "ghcr.io/org/x@"+tag, catalog).ToolVersion)
	}
}
