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

// cleanForServer prepares text for the Server: the workspace path is
// stripped (see stripWorkspacePath), then each run of bytes that is not
// valid UTF-8 is replaced with one U+FFFD.
//
// The protobuf text fields the Runner sends refuse invalid UTF-8, and a
// message that cannot be encoded fails on every attempt. Replacing the
// bytes here, in the one step all such text passes through, keeps that
// from ever reaching the wire. Whatever this returns is valid UTF-8.
//
// Like the path stripping, it is deliberately not applied to the Runner's
// own log or to the text the plugin reads its counts from, which keep the
// bytes the tool wrote.
func cleanForServer(dir, s string) string {
	return strings.ToValidUTF8(stripWorkspacePath(dir, s), "\uFFFD")
}
