package runner

import "strings"

// stripWorkspacePath rewrites the Runner's own workspace directory out of
// text bound for the Server — and from there, a PR comment.
//
// A tool error quoting absolute paths is both noisy and unrecognizable: a
// reviewer knows "environments/secrets.yaml.gotmpl", not where turnip
// mounted its checkout. Stripping the prefix leaves exactly the
// repository-relative path they know, and shortens errors that quote the
// same prefix several times over.
//
// A bare mention of the directory with no trailing separator becomes ".",
// which is what it is from the repository's point of view.
//
// Deliberately not applied to the Runner's local stdout/stderr mirroring:
// `kubectl logs` is the one place the real absolute path is still worth
// having.
func stripWorkspacePath(dir, s string) string {
	if dir == "" {
		return s
	}
	s = strings.ReplaceAll(s, dir+"/", "")
	return strings.ReplaceAll(s, dir, ".")
}
