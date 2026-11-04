package manifests

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

func kustomizeBytes(t *testing.T, dir string) []byte {
	t.Helper()
	var stderr bytes.Buffer
	cmd := exec.Command("kubectl", "kustomize", dir)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	require.NoError(t, err, stderr.String())
	return out
}

// TestHelmfileTwoPass renders deploy/base, then applies transformers to
// the rendered output, as a Helmfile release does: the transformers see
// a rendered base, not its generators.
func TestHelmfileTwoPass(t *testing.T) {
	if _, err := exec.LookPath("kubectl"); err != nil {
		t.Skip("kubectl not on PATH")
	}
	base, err := filepath.Abs("../../deploy/base")
	require.NoError(t, err)
	// The shipped replacement, copied beside the kustomization: a
	// release's transformers are inline objects, and Kustomize refuses
	// an absolute path to a directory.
	runnerNS, err := os.ReadFile("../../deploy/transformers/runner-namespace/runner-namespace.yaml")
	require.NoError(t, err)

	dir := t.TempDir()
	files := map[string]string{
		"all.yaml": string(kustomizeBytes(t, base)),
		"image.yaml": `apiVersion: builtin
kind: ImageTagTransformer
metadata:
  name: server-image
imageTag:
  name: ghcr.io/ivanvc/turnip-server
  newTag: v9.9.9
`,
		"role-namespace.yaml": `apiVersion: builtin
kind: PatchTransformer
metadata:
  name: role-namespace
target:
  kind: Role
  name: turnip-server
patch: |-
  - op: replace
    path: /metadata/namespace
    value: ops-runners
`,
		"runner-namespace.yaml": string(runnerNS),
		"kustomization.yaml": `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - all.yaml
transformers:
  - image.yaml
  - role-namespace.yaml
  - runner-namespace.yaml
`,
	}
	for name, body := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600))
	}

	out := kustomizeBytes(t, dir)
	var objs []object
	var images []string
	dec := yaml.NewDecoder(bytes.NewReader(out))
	for {
		var doc struct {
			Kind string `yaml:"kind"`
			Spec struct {
				Template struct {
					Spec struct {
						Containers []struct {
							Image string `yaml:"image"`
						} `yaml:"containers"`
					} `yaml:"spec"`
				} `yaml:"template"`
			} `yaml:"spec"`
		}
		var o object
		var node yaml.Node
		err := dec.Decode(&node)
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		require.NoError(t, node.Decode(&o))
		require.NoError(t, node.Decode(&doc))
		if o.Kind == "" {
			continue
		}
		objs = append(objs, o)
		if doc.Kind == "Deployment" {
			for _, c := range doc.Spec.Template.Spec.Containers {
				images = append(images, c.Image)
			}
		}
	}

	dep := find(t, objs, "Deployment", "turnip-server")
	require.Len(t, dep.Spec.Template.Spec.Containers, 1)
	found := 0
	for _, e := range dep.Spec.Template.Spec.Containers[0].Env {
		if e.Name == "TURNIP_RUNNER_NAMESPACE" {
			found++
			assert.Equal(t, "ops-runners", e.Value)
		}
	}
	require.Equal(t, 1, found, "TURNIP_RUNNER_NAMESPACE entry")
	assert.Equal(t, []string{"ghcr.io/ivanvc/turnip-server:v9.9.9"}, images)

	assert.Equal(t, "ops-runners", find(t, objs, "Role", "turnip-server").Metadata.Namespace)
	assert.Equal(t, "ops-runners", find(t, objs, "RoleBinding", "turnip-server").Metadata.Namespace)
	assert.Equal(t, "turnip-system", find(t, objs, "Deployment", "turnip-server").Metadata.Namespace)
}
