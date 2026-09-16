package github

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/ivanvc/turnip/internal/config"
)

// tokenPattern generates safe, non-whitespace tokens with no "-" characters
// (so they can never collide with the "--" delimiter) for use as tool,
// operation, project, or extra-arg tokens in generated trigger lines.
const tokenPattern = "[a-zA-Z0-9]+"

func genToken(t *rapid.T, label string) string {
	return rapid.StringMatching(tokenPattern).Draw(t, label)
}

// knownToolNames mirrors parser.go's knownTools. Trigger-line generators
// draw from these rather than from arbitrary tokens: a line naming any
// other tool is deliberately not a trigger at all (Requirement 4.2a), so
// an arbitrary token can no longer stand in for "some tool name".
var knownToolNames = []string{"turnip", config.ToolTerraform, config.ToolPulumi, config.ToolHelmfile}

func genKnownTool(t *rapid.T, label string) string {
	return rapid.SampledFrom(knownToolNames).Draw(t, label)
}

func genUnknownTool(t *rapid.T, label string) string {
	return rapid.StringMatching(tokenPattern).
		Filter(func(s string) bool { return !slices.Contains(knownToolNames, s) }).
		Draw(t, label)
}

// Feature: multi-iac-automation-platform, Property 7: Comment Trigger Pattern Recognition
func TestProperty_CommentTriggerPatternRecognition(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		tool := genKnownTool(t, "tool")
		operation := genToken(t, "operation")
		extraArgs := rapid.SliceOfN(rapid.StringMatching(tokenPattern), 0, 5).Draw(t, "extraArgs")

		body := "/" + tool + " " + operation
		if len(extraArgs) > 0 {
			body += " -- " + strings.Join(extraArgs, " ")
		}

		got, err := ParseTriggers(body)
		require.NoError(t, err)
		require.Len(t, got, 1)
		require.Equal(t, tool, got[0].Tool)
		require.Equal(t, operation, got[0].Operation)
		if len(extraArgs) == 0 {
			require.Empty(t, got[0].ExtraArgs)
		} else {
			require.Equal(t, extraArgs, got[0].ExtraArgs)
		}
	})
}

// Feature: multi-iac-automation-platform, Property 8: Selective Project Triggering from Comments
func TestProperty_SelectiveProjectTriggeringFromComments(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		project := genToken(t, "project")

		got, err := ParseTriggers("/turnip apply " + project)
		require.NoError(t, err)
		require.Len(t, got, 1)
		require.Equal(t, []string{project}, got[0].Projects)
	})
}

// Feature: multi-iac-automation-platform, Property 15: Consolidated Comment Per PR
func TestProperty_ConsolidatedCommentPerPR(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.IntRange(1, 5).Draw(t, "n")
		results := make([]ProjectResult, n)
		for i := range results {
			results[i] = ProjectResult{
				ProjectName: genToken(t, "name"),
				Operation:   genToken(t, "operation"),
				Success:     rapid.Bool().Draw(t, "success"),
				Output:      rapid.StringN(0, 200, 200).Draw(t, "output"),
			}
		}

		bodies := BuildConsolidatedComment(results)
		require.Len(t, bodies, 1, "total content stays well under maxCommentLength")
	})
}

// Feature: multi-iac-automation-platform, Property 17: Comment Contains All Project Results
func TestProperty_CommentContainsAllProjectResults(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.IntRange(1, 10).Draw(t, "n")
		results := make([]ProjectResult, n)
		for i := range results {
			results[i] = ProjectResult{
				ProjectName: genToken(t, "name"),
				Operation:   genToken(t, "operation"),
				Success:     rapid.Bool().Draw(t, "success"),
				Output:      rapid.StringN(0, 2000, 2000).Draw(t, "output"),
			}
		}

		all := strings.Join(BuildConsolidatedComment(results), "")
		for _, r := range results {
			require.Containsf(t, all, r.ProjectName, "project missing from concatenated bodies")
		}
	})
}

type triggerLine struct {
	text       string
	wellFormed bool
	project    string
}

// Property (slice-local, Requirements 4.2/4.6/4.8 — no corresponding
// global-design property number, since the global CommentParser sketch
// only ever returns one TriggerCommand): Multi-Line Trigger Ordering and
// Partial-Failure Isolation.
func TestProperty_MultiLineTriggerOrderingAndPartialFailureIsolation(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.IntRange(0, 5).Draw(t, "numWellFormed")
		m := rapid.IntRange(0, 5).Draw(t, "numMalformed")
		if n == 0 && m == 0 {
			return
		}

		lines := make([]triggerLine, 0, n+m)
		for range n {
			tool := genKnownTool(t, "tool")
			op := genToken(t, "op")
			proj := genToken(t, "proj")
			lines = append(lines, triggerLine{text: "/" + tool + " " + op + " " + proj, wellFormed: true, project: proj})
		}
		for range m {
			tool := genKnownTool(t, "badtool")
			lines = append(lines, triggerLine{text: "/" + tool})
		}

		shuffled := rapid.Permutation(lines).Draw(t, "shuffled")

		var bodyLines []string
		var wantProjects []string
		for _, l := range shuffled {
			bodyLines = append(bodyLines, l.text)
			if l.wellFormed {
				wantProjects = append(wantProjects, l.project)
			}
		}
		body := strings.Join(bodyLines, "\n")

		got, err := ParseTriggers(body)

		if n == 0 {
			require.Nil(t, got, "no well-formed lines")
		} else {
			require.Len(t, got, n)
			for i, cmd := range got {
				require.Lenf(t, cmd.Projects, 1, "command %d", i)
				require.Equalf(t, wantProjects[i], cmd.Projects[0], "order must match the body's well-formed lines, command %d", i)
			}
		}

		if m == 0 {
			require.NoError(t, err, "no malformed lines")
			return
		}

		var malformed MalformedTriggerErrors
		require.ErrorAs(t, err, &malformed)
		require.Len(t, malformed, m)
	})
}

// Property (slice-local, Requirement 4.2a): a line naming a tool turnip
// doesn't know is not a trigger — neither a TriggerCommand nor a
// malformed-line error. This is the property the "turnip replies to other
// bots' slash commands" bug violated.
func TestProperty_UnknownToolIsNeverATrigger(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		tool := genUnknownTool(t, "tool")
		operation := genToken(t, "operation")
		project := genToken(t, "project")

		got, err := ParseTriggers("/" + tool + " " + operation + " " + project)
		require.ErrorIs(t, err, ErrNoTrigger)
		require.Nil(t, got)
	})
}
