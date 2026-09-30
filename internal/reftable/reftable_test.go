package reftable

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Both failure shapes a Reference test reports, shown on synthetic input
// independent of any schema or documentation page: a name the code has
// but the table lacks, and a row naming something the code does not have.
func TestDiff_DetectsMissingAndExtra(t *testing.T) {
	missing, extra := Diff([]string{"a", "b"}, []string{"a", "b", "c"})
	assert.Equal(t, []string{"c"}, missing, "a name present in want but absent from got is reported missing")
	assert.Empty(t, extra)

	missing, extra = Diff([]string{"a", "b", "d"}, []string{"a", "b"})
	assert.Empty(t, missing)
	assert.Equal(t, []string{"d"}, extra, "a name present in got but absent from want is reported extra")

	missing, extra = Diff([]string{"a", "a"}, []string{"a"})
	assert.Empty(t, missing)
	assert.Empty(t, extra, "a duplicate counts once")
}

// Only the first table's first column is read, every backticked name in
// it counts, and an escaped pipe stays inside its cell.
func TestFirstColumnNames(t *testing.T) {
	content := "# Page\n" +
		"\n" +
		"| Name | Meaning |\n" +
		"|---|---|\n" +
		"| `A` or `B` | `not-first-column` |\n" +
		"| `x \\| y` | escaped pipe |\n" +
		"| plain | no name |\n" +
		"\n" +
		"| Other | Table |\n" +
		"|---|---|\n" +
		"| `C` | ignored |\n"

	path := filepath.Join(t.TempDir(), "page.md")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	names, err := FirstColumnNames(path)
	require.NoError(t, err)
	assert.Equal(t, []string{"A", "B", "x | y"}, names)
}

func TestFirstColumnNames_NoTable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "page.md")
	require.NoError(t, os.WriteFile(path, []byte("# Page\n\nno table\n"), 0o600))

	_, err := FirstColumnNames(path)
	require.Error(t, err)
}
