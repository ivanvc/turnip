package plugin

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivanvc/turnip/internal/provisioning"
)

func TestRegistry_KeyedByName(t *testing.T) {
	registry := Registry()
	require.NotEmpty(t, registry)

	for name, p := range registry {
		assert.Equal(t, name, p.Name())
	}
}

func TestRegistry_FreshMapPerCall(t *testing.T) {
	first := Registry()
	delete(first, "helmfile")

	assert.Contains(t, Registry(), "helmfile",
		"a caller editing its map must not change what the next caller gets")
}

// TestRegistry_EveryPluginDeclaresItsProvisioning is what stops a Plugin
// being registered half-declared: a Runner Job for it would otherwise be
// built with no image, or with a strategy internal/jobs does not
// implement, and fail only once a Lock is held and a check is running.
func TestRegistry_EveryPluginDeclaresItsProvisioning(t *testing.T) {
	for name, p := range Registry() {
		t.Run(name, func(t *testing.T) {
			spec := p.Provisioning()

			assert.Contains(t, []provisioning.Strategy{provisioning.CopyOut, provisioning.RunInImage}, spec.Strategy,
				"unknown provisioning strategy")

			assert.True(t, isFullyQualified(spec.Image),
				"image %q must name its registry host, so a Job never depends on a node's default registry", spec.Image)
			assert.NotContains(t, spec.Image, "@", "image %q is a repository; the version supplies the tag", spec.Image)
			assert.NotContains(t, spec.Image[strings.LastIndex(spec.Image, "/")+1:], ":",
				"image %q is a repository; the version supplies the tag", spec.Image)

			if spec.Strategy == provisioning.CopyOut {
				assert.NotEmpty(t, spec.BinaryPath, "CopyOut needs the binary's path in the image")
			} else {
				assert.Empty(t, spec.BinaryPath, "only CopyOut copies a binary out of the image")
			}
		})
	}
}

// isFullyQualified reports whether image's first path component is a
// registry host: one containing a dot, or localhost (with or without a
// port). Anything else is a Docker Hub namespace, which a container
// runtime would resolve against whatever default registry the node has.
func isFullyQualified(image string) bool {
	host, _, found := strings.Cut(image, "/")
	if !found {
		return false
	}
	hostname, _, _ := strings.Cut(host, ":")
	return strings.Contains(hostname, ".") || hostname == "localhost"
}
