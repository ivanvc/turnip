package config

import (
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivanvc/turnip/internal/reftable"
)

// turnipYAMLReferencePath is the Reference table checked against the
// schema. Relative to this package's directory.
const turnipYAMLReferencePath = "../../docs/using/turnip-yaml.md"

// schemaYAMLPaths walks Config by reflection, building the same dotted
// paths the Reference table's first column names: a nested struct's own
// path (e.g. "clone") is included alongside its fields ("clone.submodules"),
// and a slice of structs contributes "path[]" as the prefix for its
// element's fields (e.g. "projects[].uses"). A field tagged yaml:"-" is
// skipped. A map (such as `with` or `runner.env`) and any other scalar are
// leaves: their keys belong to whoever reads them, not to this schema.
func schemaYAMLPaths() map[string]bool {
	paths := map[string]bool{}
	collectYAMLPaths(reflect.TypeOf(Config{}), "", paths)
	return paths
}

func collectYAMLPaths(t reflect.Type, prefix string, paths map[string]bool) {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return
	}

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if field.PkgPath != "" {
			continue // unexported
		}

		name, ok := yamlFieldName(field.Tag.Get("yaml"))
		if !ok {
			continue // yaml:"-"
		}

		path := name
		if prefix != "" {
			path = prefix + "." + name
		}
		paths[path] = true

		ft := field.Type
		switch {
		case ft.Kind() == reflect.Struct:
			collectYAMLPaths(ft, path, paths)
		case ft.Kind() == reflect.Slice && ft.Elem().Kind() == reflect.Struct:
			collectYAMLPaths(ft.Elem(), path+"[]", paths)
		}
		// Maps and every other scalar kind are leaves: nothing more to walk.
	}
}

// yamlFieldName returns the field's YAML key and whether it participates
// in the schema at all (false for yaml:"-").
func yamlFieldName(tag string) (string, bool) {
	if tag == "-" {
		return "", false
	}
	name, _, _ := strings.Cut(tag, ",")
	return name, true
}

// Feature: docs-onboarding, Requirement 4.1/4.3: the turnip.yaml Reference
// lists every field the schema accepts, and gaining a field without a row
// (or a row for a field that no longer exists) fails the build.
func TestTurnipYAMLReference_MatchesSchema(t *testing.T) {
	var schema []string
	for path := range schemaYAMLPaths() {
		schema = append(schema, path)
	}
	docs, err := reftable.FirstColumnNames(turnipYAMLReferencePath)
	require.NoError(t, err)

	missing, extra := reftable.Diff(docs, schema)
	assert.Empty(t, missing, "%s is missing a row for: %v", turnipYAMLReferencePath, missing)
	assert.Empty(t, extra, "%s has a row naming a field that does not exist in config.Config: %v", turnipYAMLReferencePath, extra)
}
