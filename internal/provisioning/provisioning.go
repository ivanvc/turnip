// Package provisioning names how a Runner Job obtains its IaC tool. It is
// the vocabulary a Plugin declares and internal/jobs implements, kept in a
// leaf package of its own so that a Plugin never imports anything that
// knows about Kubernetes, and internal/jobs never imports a Plugin.
package provisioning

// Strategy is how a Runner Job obtains its IaC tool.
//
// It is a per-tool property because tools differ in kind, not just in
// name: some are a single self-contained binary, and some are a runtime
// that needs helper executables, plugins and environment the vendor image
// provides. Each Strategy is implemented once, in internal/jobs, so a new
// tool picks one rather than reimplementing it.
type Strategy int

const (
	// CopyOut adds an init container running the tool's image that copies
	// the binary at BinaryPath onto a shared volume; the Job's main
	// container is turnip's own Runner image, which executes it from
	// there.
	CopyOut Strategy = iota
	// RunInImage makes the tool's image the Job's main container, with an
	// init container copying turnip's runner binary onto a shared volume,
	// so the tool runs with the image's own PATH, environment and HOME.
	RunInImage
)

// Spec is how one tool reaches a Runner Job.
type Spec struct {
	Strategy Strategy

	// Image is the tool's image repository, fully qualified with its
	// registry host. It carries no tag: the tag is the version the
	// Project's uses: line names, appended as written.
	Image string

	// BinaryPath is where the tool's binary lives inside Image, to be
	// copied out under CopyOut. It is empty under RunInImage, where
	// nothing is copied out of the tool's image.
	BinaryPath string
}
