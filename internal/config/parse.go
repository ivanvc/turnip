package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

	yaml "go.yaml.in/yaml/v3"
)

var lineRe = regexp.MustCompile(`line (\d+)`)

// unknownFieldRe pulls the offending key and its line out of one
// yaml.TypeError entry, which reads "line 7: field nope not found in type
// config.Project". Only the key and the line are taken: the rest names a
// Go type, which is an implementation detail no PR comment should show.
var unknownFieldRe = regexp.MustCompile(`^line (\d+): field (\S+) not found in type`)

// Parse unmarshals and validates turnip.yaml content in one call. It
// returns *Config only when parsing AND validation both succeed; otherwise
// it returns a nil *Config and a non-nil error.
//
// Decoding happens twice, which is what lets an unrecognized key be
// reported as a *validation* problem while malformed YAML stays a
// *ParseError. Both arrive from yaml.v3 as the same *yaml.TypeError, and
// telling them apart by matching the decoder's wording would tie turnip's
// error classification to another project's prose. Instead the lenient
// pass runs first: once it has succeeded, anything the strict pass
// objects to is, by construction, a key the schema does not define.
//
// The schemaVersion check sits between the two passes deliberately. A
// file written for the previous schema would otherwise report one
// unknown-key error per field it carries, burying the single fact that
// actually explains them.
//
// tools is the set of tool names a Project's uses: may name: the names of
// the Plugins this turnip has registered. It is passed in rather than
// known here so that this package depends on no Plugin, and error
// messages list it in the order given, so callers pass it sorted.
func Parse(data []byte, tools []string) (*Config, error) {
	var c Config
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, newParseError(err)
	}

	if err := validateSchemaVersion(&c); err != nil {
		return nil, err
	}

	if err := rejectUnknownFields(data); err != nil {
		return nil, err
	}

	applyDefaults(&c)

	if err := validate(&c, tools); err != nil {
		return nil, err
	}

	return &c, nil
}

// rejectUnknownFields re-decodes with strict field checking, reporting
// every unrecognized key rather than stopping at the first.
//
// YAML merge keys need no special handling here: yaml.v3 expands "<<"
// before matching fields, so an anchored document decodes normally under
// KnownFields. Configurations use anchors precisely to avoid the
// repetition a per-project schema forces, so breaking them would hurt the
// files that most need them.
func rejectUnknownFields(data []byte) error {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)

	var strict Config
	err := dec.Decode(&strict)
	switch {
	case err == nil, errors.Is(err, io.EOF):
		// Decode reports EOF for an empty document where Unmarshal
		// succeeds. An empty document has no schemaVersion and was already
		// rejected above, so this is belt-and-braces.
		return nil
	}

	var typeErr *yaml.TypeError
	if !errors.As(err, &typeErr) {
		// The lenient pass accepted this document, so a non-TypeError here
		// is not something the caller can act on as a schema problem.
		return newParseError(err)
	}

	errs := make(ValidationErrors, 0, len(typeErr.Errors))
	for _, entry := range typeErr.Errors {
		errs = append(errs, unknownFieldError(entry))
	}
	return errs
}

// unknownFieldError renders one decoder complaint as a ValidationError. If
// the message doesn't have the shape we expect — a yaml.v3 rewording, say
// — the line is still surfaced rather than the raw text, so a change
// upstream degrades the message instead of leaking Go type names.
func unknownFieldError(entry string) *ValidationError {
	if m := unknownFieldRe.FindStringSubmatch(entry); m != nil {
		return &ValidationError{
			ProjectRef: configFileRef,
			Field:      m[2],
			Message:    fmt.Sprintf("unrecognized field (line %s)", m[1]),
		}
	}

	field := "<unknown>"
	message := "unrecognized field"
	if m := lineRe.FindStringSubmatch(entry); m != nil {
		message = fmt.Sprintf("unrecognized field (line %s)", m[1])
	}
	return &ValidationError{ProjectRef: configFileRef, Field: field, Message: message}
}

// applyDefaults fills in fields derived from what the file said rather
// than written in it: a Project's tool and version come apart from `uses`,
// and a Project with no name falls back to its directory (directory is
// required, so it's always available as an identifier).
func applyDefaults(c *Config) {
	for i := range c.Projects {
		p := &c.Projects[i]

		p.Tool, p.ToolVersion = splitUses(p.Uses)

		if p.Name == "" {
			p.Name = p.Directory
		}
	}
}

// splitUses decomposes "<tool>@<version>" into its parts, keeping the
// version exactly as written: it becomes the image tag, and whether a tag
// carries a leading "v" is the vendor's convention, not turnip's. A
// reference with no "@" yields an empty version, which validation rejects.
//
// Nothing here rejects anything: an empty or malformed result is a
// validation concern, and validate reads the raw Uses string so it can
// tell "no version given" from "@" with nothing after it.
func splitUses(uses string) (tool, version string) {
	tool, version, _ = strings.Cut(uses, "@")
	return tool, version
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
