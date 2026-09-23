package github

import (
	"strings"

	"github.com/ivanvc/turnip/internal/config"
)

// knownTools are the names ParseTriggers accepts after the leading "/":
// the IaC tools, plus "turnip" itself for "every Project regardless of
// tool". Sourced from internal/config so the tool vocabulary has one
// definition.
//
// A line starting with any other "/word" belongs to someone else — a
// different bot's command (`/jira`, `/lgtm`), a file path pasted at the
// start of a line — and is skipped silently. Treating those as triggers
// made every such comment run the whole authorize-and-fetch-config flow
// and reply to people who never addressed turnip at all.
var knownTools = map[string]bool{
	"turnip":             true,
	config.ToolTerraform: true,
	config.ToolPulumi:    true,
	config.ToolHelmfile:  true,
}

// ParseTriggers scans body line by line, extracting one TriggerCommand per
// well-formed trigger line — not stopping at the first match, so a single
// comment can batch several actions. See errors.go's ErrNoTrigger and
// MalformedTriggerErrors for how the error cases are reported.
func ParseTriggers(body string) ([]*TriggerCommand, error) {
	var commands []*TriggerCommand
	var malformed MalformedTriggerErrors

	for i, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		tokens := strings.Fields(trimmed)
		if len(tokens) == 0 {
			continue
		}
		if len(tokens[0]) < 2 || tokens[0][0] != '/' {
			continue
		}
		// Not addressed to turnip: not a trigger, and not malformed
		// either — reporting it would be just as noisy as acting on it.
		if !knownTools[tokens[0][1:]] {
			continue
		}

		lineNum := i + 1
		if len(tokens) == 1 {
			malformed = append(malformed, &MalformedTriggerError{Line: lineNum, Content: trimmed})
			continue
		}

		cmd := &TriggerCommand{
			Tool:      tokens[0][1:],
			Operation: tokens[1],
		}

		rest := tokens[2:]
		if idx := indexOfArgStart(rest); idx >= 0 {
			cmd.Projects = nonEmpty(rest[:idx])
			// An explicit "--" is consumed as the delimiter; any other
			// "-"-prefixed token is itself the first argument, which is
			// what lets a scoped trigger be written without a delimiter.
			if rest[idx] == "--" {
				cmd.ExtraArgs = nonEmpty(rest[idx+1:])
			} else {
				cmd.ExtraArgs = nonEmpty(rest[idx:])
			}
		} else {
			cmd.Projects = nonEmpty(rest)
		}

		commands = append(commands, cmd)
	}

	switch {
	case len(commands) == 0 && len(malformed) == 0:
		return nil, ErrNoTrigger
	case len(malformed) == 0:
		return commands, nil
	default:
		return commands, malformed
	}
}

// indexOfArgStart returns the index of the token that begins a trigger
// line's trailing arguments, or -1 when the line has none.
//
// A tool's own flags start with "-", so the first such token is where the
// Project names stop — which is what lets "/turnip diff web -l name=x"
// work without a delimiter, where it previously failed as an unmatched
// Project name. An explicit "--" is found by this same scan because it
// also begins with "-"; the caller consumes that one instead of passing
// it through, which is the only difference between the two cases.
//
// The scan stops at the first match and nothing after it is re-examined,
// so a second "--" further along is an ordinary argument — unchanged from
// the behavior this rule replaces.
func indexOfArgStart(tokens []string) int {
	for i, t := range tokens {
		if strings.HasPrefix(t, "-") {
			return i
		}
	}
	return -1
}

func nonEmpty(tokens []string) []string {
	if len(tokens) == 0 {
		return nil
	}
	return tokens
}
