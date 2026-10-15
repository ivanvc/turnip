package runner

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"pgregory.net/rapid"
)

// The real shape this exists for: helmfile quotes absolute paths inside a
// long wrapped error, twice over, and every one of them is turnip's
// workspace rather than anything the reviewer would recognize.
func TestStripWorkspacePath_RepositoryRelativePathsSurvive(t *testing.T) {
	const dir = "/turnip/src"
	in := `failed to load environment values file "` + dir + `/environments/secrets.yaml.gotmpl": ` +
		`failed to render [` + dir + `/environments/secrets.yaml.gotmpl]`

	got := stripWorkspacePath(dir, in)

	assert.NotContains(t, got, dir, "every occurrence is rewritten, not just the first")
	assert.Contains(t, got, `"environments/secrets.yaml.gotmpl"`)
	assert.Contains(t, got, `[environments/secrets.yaml.gotmpl]`)
}

func TestStripWorkspacePath_BareDirectoryBecomesDot(t *testing.T) {
	const dir = "/turnip/src"
	assert.Equal(t, "chdir .: permission denied", stripWorkspacePath(dir, "chdir "+dir+": permission denied"))
}

func TestStripWorkspacePath_EmptyDirIsANoop(t *testing.T) {
	assert.Equal(t, "unchanged /tmp/whatever", stripWorkspacePath("", "unchanged /tmp/whatever"))
}

func TestStripWorkspacePath_UnrelatedTextUntouched(t *testing.T) {
	const dir = "/turnip/src"
	in := "Error: cannot re-use a name that is still in use (release: my-app)"
	assert.Equal(t, in, stripWorkspacePath(dir, in))
}

// The temporary-directory fallback is stripped just the same — the
// function takes the directory as a parameter, so a fixed mount path and
// a per-run temporary directory behave identically.
func TestStripWorkspacePath_TemporaryFallbackDirectory(t *testing.T) {
	const dir = "/tmp/turnip-runner-2858245619"
	assert.Equal(t, "see environments/prod", stripWorkspacePath(dir, "see "+dir+"/environments/prod"))
}

// A different directory must not be rewritten: only this run's own
// workspace is turnip's to hide.
func TestStripWorkspacePath_OnlyThisRunsDirectory(t *testing.T) {
	got := stripWorkspacePath("/tmp/turnip-runner-1", "see /tmp/turnip-runner-2/environments")
	assert.Equal(t, "see /tmp/turnip-runner-2/environments", got)
}

func TestCleanForServer_Examples(t *testing.T) {
	const dir = "/turnip/src"
	tests := []struct {
		name, in, want string
	}{
		{"one invalid byte", "name: caf\xe9", "name: caf\uFFFD"},
		{"a run of invalid bytes", "a\x80\xff\xc3b", "a\uFFFDb"},
		{"valid text unchanged", "naïve 💡", "naïve 💡"},
		{"truncated multi-byte character", "snow \xe2\x98", "snow \uFFFD"},
		{"path stripped and bytes replaced", "see " + dir + "/env\xff", "see env\uFFFD"},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cleanForServer(dir, tt.in)
			assert.Equal(t, tt.want, got)
			assert.True(t, utf8.ValidString(got))
		})
	}
}

// Feature: fix-invalid-utf8-output, Property 1: What leaves for the Server is valid UTF-8
func TestProperty_CleanForServerIsValidUTF8(t *testing.T) {
	const dir = "/turnip/src"
	rapid.Check(t, func(rt *rapid.T) {
		// Any byte string: always valid UTF-8 out.
		raw := rapid.SliceOf(rapid.Byte()).Draw(rt, "raw")
		assert.True(rt, utf8.ValidString(cleanForServer(dir, string(raw))))

		// Valid text comes back with only the path stripped.
		valid := rapid.String().Draw(rt, "valid")
		withPath := valid + dir + "/x" + valid
		assert.Equal(rt, stripWorkspacePath(dir, withPath), cleanForServer(dir, withPath))

		// Valid segments split by runs of invalid bytes: each run becomes
		// exactly one U+FFFD. Lone continuation bytes and 0xFF are invalid
		// next to any neighbor, so runs never merge into characters.
		segments := rapid.SliceOfN(rapid.StringN(1, -1, -1), 1, 6).Draw(rt, "segments")
		invalid := rapid.SliceOfN(rapid.SampledFrom([]byte{0x80, 0xA0, 0xBF, 0xFF}), 1, 5)
		var in strings.Builder
		var want strings.Builder
		for i, seg := range segments {
			if i > 0 {
				in.Write(invalid.Draw(rt, "run"))
				want.WriteString("\uFFFD")
			}
			in.WriteString(seg)
			want.WriteString(stripWorkspacePath(dir, seg))
		}
		assert.Equal(rt, want.String(), cleanForServer(dir, in.String()))
	})
}
