package runner

import (
	"fmt"
	"os"
	"strings"
)

// gitConfigEntry is one key/value git should see for a single
// invocation.
type gitConfigEntry struct{ key, value string }

// gitConfigEnv renders entries as the GIT_CONFIG_COUNT/KEY_n/VALUE_n
// environment git reads per-invocation configuration from.
//
// Per-invocation, deliberately: nothing is written to a config file, so
// there is no file to clean up and none to leave behind on a failure
// path.
func gitConfigEnv(entries []gitConfigEntry) []string {
	if len(entries) == 0 {
		return nil
	}
	env := []string{fmt.Sprintf("GIT_CONFIG_COUNT=%d", len(entries))}
	for i, e := range entries {
		env = append(env,
			fmt.Sprintf("GIT_CONFIG_KEY_%d=%s", i, e.key),
			fmt.Sprintf("GIT_CONFIG_VALUE_%d=%s", i, e.value),
		)
	}
	return env
}

// credentialHelperEntry points git at this same binary, so that when git
// needs a credential it asks turnip for one instead of turnip having to
// put one somewhere git can find.
//
// The "!" prefix is git's "run this as a shell command" form, which is
// what lets a subcommand be passed. The path is single-quoted because
// git hands the value to a shell.
func credentialHelperEntry() (gitConfigEntry, error) {
	exe, err := os.Executable()
	if err != nil {
		return gitConfigEntry{}, fmt.Errorf("runner: locating own binary for the credential helper: %w", err)
	}
	return gitConfigEntry{
		key:   "credential.helper",
		value: "!" + shellSingleQuote(exe) + " credential",
	}, nil
}

// shellSingleQuote makes s safe inside single quotes, for the one place
// turnip hands a path to a shell it does not control.
func shellSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
