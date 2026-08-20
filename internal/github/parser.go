package github

import "strings"

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
