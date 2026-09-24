package github

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestChangeText(t *testing.T) {
	assert.Equal(t, "+1 ~4 -2", ChangeText(ChangeCounts{Add: 1, Change: 4, Destroy: 2}))
	assert.Equal(t, "+0 ~1 -0", ChangeText(ChangeCounts{Change: 1}))
	assert.Equal(t, "no changes", ChangeText(ChangeCounts{}))
}

// A check run title is not HTML, so the plain form must not escape what
// the comment's <summary> form does.
func TestScopeText_IsPlainAndTruncated(t *testing.T) {
	assert.Empty(t, ScopeText(nil))
	assert.Equal(t, "-l name=<api>&", ScopeText([]string{"-l", "name=<api>&"}))

	long := ScopeText([]string{"-l", strings.Repeat("x", 100)})
	assert.Len(t, []rune(long), scopeMarkerWidth)
	assert.True(t, strings.HasSuffix(long, "…"))

	assert.Equal(t, "<code>-l name=&lt;api&gt;&amp;</code>", scopeMarker([]string{"-l", "name=<api>&"}),
		"the comment keeps its escaping, applied to the plain text")
}
