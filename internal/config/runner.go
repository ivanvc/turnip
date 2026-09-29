package config

import (
	"fmt"
	"reflect"
)

// ServiceAccountSource says which block of turnip.yaml set a Project's
// Effective_Runner ServiceAccount. The gate reads it so that a refusal
// names the block the author has to edit, which need not be the Project's
// own.
type ServiceAccountSource string

const (
	// ServiceAccountSourceNone means neither level set a ServiceAccount;
	// the Server's default applies. It is the zero value.
	ServiceAccountSourceNone ServiceAccountSource = ""

	// ServiceAccountSourceProject means the Project's own `runner:` block
	// set it.
	ServiceAccountSourceProject ServiceAccountSource = "project"

	// ServiceAccountSourceTopLevel means the Project inherited it from the
	// top-level `runner:` block.
	ServiceAccountSourceTopLevel ServiceAccountSource = "top-level"
)

// mergeRule is how one RunnerSpec field combines its two levels.
type mergeRule int

const (
	// mergeReplaced takes the Project's value when it is set (non-zero),
	// and the top level's otherwise. The value is assigned as it is, which
	// is only safe for scalars: a replaced map or slice field would be
	// shared by every Project inheriting it, so the slice that adds one
	// must also copy it (and test that the copy is not aliased).
	mergeReplaced mergeRule = iota + 1

	// mergePerKey applies to maps: every top-level key, then every
	// Project key over it. A key present with an empty value is still
	// present, so it overrides.
	mergePerKey
)

// runnerMergeRules is the merge, keyed by RunnerSpec's Go field name.
// mergeRunner reads nothing else, so the rule declared here is the rule
// that runs. A field missing from this table fails
// TestRunnerMergeRules_CoverEveryField rather than inheriting whatever an
// unfamiliar type would happen to do.
var runnerMergeRules = map[string]mergeRule{
	"ServiceAccount": mergeReplaced,
	"Env":            mergePerKey,
}

// unruledFields returns the fields of struct type t that have no entry in
// rules, in declaration order. It takes the type rather than assuming
// RunnerSpec so that a test can show it reports a field that is missing.
func unruledFields(t reflect.Type, rules map[string]mergeRule) []string {
	var missing []string
	for i := range t.NumField() {
		name := t.Field(i).Name
		if _, ok := rules[name]; !ok {
			missing = append(missing, name)
		}
	}
	return missing
}

// mergeRunner returns the Effective_Runner: project over top, field by
// field, by runnerMergeRules. The result shares no map or slice with
// either input, so Projects that inherit the same top-level block cannot
// alter one another's settings.
func mergeRunner(top, project RunnerSpec) RunnerSpec {
	var out RunnerSpec
	tv := reflect.ValueOf(top)
	pv := reflect.ValueOf(project)
	ov := reflect.ValueOf(&out).Elem()

	for i := range ov.NumField() {
		name := ov.Type().Field(i).Name
		rule, ok := runnerMergeRules[name]
		if !ok {
			panic(fmt.Sprintf("config: RunnerSpec.%s has no merge rule", name))
		}
		switch rule {
		case mergeReplaced:
			src := tv.Field(i)
			if !pv.Field(i).IsZero() {
				src = pv.Field(i)
			}
			ov.Field(i).Set(src)
		case mergePerKey:
			ov.Field(i).Set(mergeMaps(tv.Field(i), pv.Field(i)))
		default:
			panic(fmt.Sprintf("config: RunnerSpec.%s has unknown merge rule %d", name, rule))
		}
	}
	return out
}

// mergeMaps builds a new map holding top's entries overlaid by project's.
// Both absent stays nil, so a file that sets no map anywhere reads the
// same as before the merge existed.
func mergeMaps(top, project reflect.Value) reflect.Value {
	if top.Kind() != reflect.Map {
		panic(fmt.Sprintf("config: merged per key needs a map, got %s", top.Kind()))
	}
	if top.IsNil() && project.IsNil() {
		return reflect.Zero(top.Type())
	}
	out := reflect.MakeMapWithSize(top.Type(), top.Len()+project.Len())
	for _, m := range []reflect.Value{top, project} {
		iter := m.MapRange()
		for iter.Next() {
			out.SetMapIndex(iter.Key(), iter.Value())
		}
	}
	return out
}

// serviceAccountSource reports which level supplied the Effective_Runner's
// ServiceAccount, following the same rule mergeRunner applies to it.
func serviceAccountSource(top, project RunnerSpec) ServiceAccountSource {
	switch {
	case project.ServiceAccount != "":
		return ServiceAccountSourceProject
	case top.ServiceAccount != "":
		return ServiceAccountSourceTopLevel
	default:
		return ServiceAccountSourceNone
	}
}

// applyRunnerDefaults merges the top-level `runner:` block into every
// Project, replacing each Project's Runner with its Effective_Runner. Parse
// runs it after validation, so each level has already been checked
// against its own path and a bad top-level value is reported once rather
// than once per Project that inherited it.
func applyRunnerDefaults(c *Config) {
	for i := range c.Projects {
		p := &c.Projects[i]
		p.ServiceAccountSource = serviceAccountSource(c.Runner, p.Runner)
		p.Runner = mergeRunner(c.Runner, p.Runner)
	}
}
