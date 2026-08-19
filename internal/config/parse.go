package config

import (
	"regexp"
	"strconv"
	"strings"

	yaml "go.yaml.in/yaml/v3"
)

var lineRe = regexp.MustCompile(`line (\d+)`)

// Parse unmarshals and validates turnip.yaml content in one call. It
// returns *Config only when parsing AND validation both succeed; otherwise
// it returns a nil *Config and a non-nil error.
func Parse(data []byte) (*Config, error) {
	var c Config
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, newParseError(err)
	}

	applyDefaults(&c)

	if err := validate(&c); err != nil {
		return nil, err
	}

	return &c, nil
}

// applyDefaults fills in fields left unset by the caller. A Project with no
// name defaults to its directory (Requirement 1.6) — directory is required,
// so it's always available as a fallback identifier.
func applyDefaults(c *Config) {
	for i := range c.Projects {
		if c.Projects[i].Name == "" {
			c.Projects[i].Name = c.Projects[i].Directory
		}
	}
}

// newParseError converts a go.yaml.in/yaml/v3 error into a *ParseError,
// extracting the line number from the error message when the underlying
// decoder embeds one (yaml.v3 does not expose column information).
func newParseError(err error) *ParseError {
	message := err.Error()
	if te, ok := err.(*yaml.TypeError); ok {
		message = strings.Join(te.Errors, "; ")
	}

	line := 0
	if m := lineRe.FindStringSubmatch(message); m != nil {
		if n, convErr := strconv.Atoi(m[1]); convErr == nil {
			line = n
		}
	}

	return &ParseError{
		Line:    line,
		Message: message,
	}
}
