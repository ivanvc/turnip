package runner

import "strings"

// stripSandboxPath rewrites the Runner's own working directory out of
// text bound for the Server — and from there, a PR comment.
//
// The clone lives in an os.MkdirTemp directory whose name is random per
// run ("/tmp/turnip-runner-2858245619"), so a tool error quoting absolute
// paths is both noisy and unrecognizable: a reviewer knows
// "environments/secrets.yaml.gotmpl", not where turnip happened to put
// its checkout this time. Stripping the prefix leaves exactly the
// repository-relative path they know, and shortens errors that quote the
// same prefix several times.
//
// A bare mention of the directory with no trailing separator becomes
// ".", which is what it is from the repository's point of view.
//
// Deliberately not applied to the Runner's local stdout/stderr mirroring:
// `kubectl logs` is the one place the real absolute path is still worth
// having.
func stripSandboxPath(dir, s string) string {
	if dir == "" {
		return s
	}
	s = strings.ReplaceAll(s, dir+"/", "")
	return strings.ReplaceAll(s, dir, ".")
}
