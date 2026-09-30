// Package reftable reads the Reference tables in turnip's documentation,
// so the tests that keep each Reference complete (the turnip.yaml schema
// in internal/config, the Server settings in internal/orchestrator) share
// one table reader and one comparison instead of a copy each.
package reftable

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
)

var (
	backtickPattern = regexp.MustCompile("`([^`]+)`")
	headerSeparator = regexp.MustCompile(`^\s*\|[\s:|-]+\|\s*$`)
	tableRow        = regexp.MustCompile(`^\s*\|.*\|\s*$`)
)

// FirstColumnNames reads the Markdown file at path and returns every
// `backticked` name in the first column of the first table it contains,
// across every data row (the header and its "---" separator excluded).
// A row's first column may name more than one (e.g. "`A` or `B`"), and
// every one counts. An escaped pipe (`\|`) inside a cell does not end it.
// It is an error for the file to contain no table.
func FirstColumnNames(path string) ([]string, error) {
	data, err := os.ReadFile(path) //nolint:gosec // reading a repo-local documentation page
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	return firstColumnNames(string(data), path)
}

func firstColumnNames(content, path string) ([]string, error) {
	lines := strings.Split(content, "\n")

	var names []string
	inTable := false
	for i := 0; i < len(lines); i++ {
		line := lines[i]

		if !inTable {
			if tableRow.MatchString(line) && i+1 < len(lines) && headerSeparator.MatchString(lines[i+1]) {
				inTable = true
				i++ // skip the separator row too
			}
			continue
		}

		if !tableRow.MatchString(line) {
			break // the table ended
		}

		cells := strings.Split(strings.ReplaceAll(line, `\|`, "\x00"), "|")
		if len(cells) < 2 {
			return nil, fmt.Errorf("malformed table row in %s: %q", path, line)
		}
		for _, m := range backtickPattern.FindAllStringSubmatch(cells[1], -1) {
			names = append(names, strings.ReplaceAll(m[1], "\x00", "|"))
		}
	}

	if !inTable {
		return nil, fmt.Errorf("no Markdown table found in %s", path)
	}
	return names, nil
}

// Diff reports, sorted, every name in want but not in got ("missing") and
// every name in got but not in want ("extra"). Duplicates count once.
func Diff(got, want []string) (missing, extra []string) {
	gotSet, wantSet := toSet(got), toSet(want)
	for name := range wantSet {
		if !gotSet[name] {
			missing = append(missing, name)
		}
	}
	for name := range gotSet {
		if !wantSet[name] {
			extra = append(extra, name)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	return missing, extra
}

func toSet(names []string) map[string]bool {
	set := make(map[string]bool, len(names))
	for _, n := range names {
		set[n] = true
	}
	return set
}
