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
		if idx := indexOf(rest, "--"); idx >= 0 {
			cmd.Projects = nonEmpty(rest[:idx])
			cmd.ExtraArgs = nonEmpty(rest[idx+1:])
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

func indexOf(tokens []string, target string) int {
	for i, t := range tokens {
		if t == target {
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
