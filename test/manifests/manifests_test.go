// Package manifests renders the Kustomize bases and overlays under deploy/
// with kubectl and checks where each resource lands.
package manifests

import (
	"bytes"
	"io"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

type object struct {
	Kind     string `yaml:"kind"`
	Metadata struct {
		Name      string `yaml:"name"`
		Namespace string `yaml:"namespace"`
	} `yaml:"metadata"`
	Data     map[string]string `yaml:"data"`
	Subjects []struct {
		Kind      string `yaml:"kind"`
		Name      string `yaml:"name"`
		Namespace string `yaml:"namespace"`
	} `yaml:"subjects"`
	Spec struct {
		Template struct {
			Spec struct {
				Containers []struct {
					Env []struct {
						Name      string `yaml:"name"`
						Value     string `yaml:"value"`
						ValueFrom *struct {
							FieldRef *struct {
								FieldPath string `yaml:"fieldPath"`
							} `yaml:"fieldRef"`
							SecretKeyRef *struct {
								Name string `yaml:"name"`
							} `yaml:"secretKeyRef"`
						} `yaml:"valueFrom"`
					} `yaml:"env"`
				} `yaml:"containers"`
			} `yaml:"spec"`
		} `yaml:"template"`
	} `yaml:"spec"`
}

func render(t *testing.T, dir string) []object {
	t.Helper()
	if _, err := exec.LookPath("kubectl"); err != nil {
		t.Skip("kubectl not on PATH")
	}
	var stderr bytes.Buffer
	cmd := exec.Command("kubectl", "kustomize", dir)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	require.NoError(t, err, stderr.String())

	var objs []object
	dec := yaml.NewDecoder(bytes.NewReader(out))
	for {
		var o object
		err := dec.Decode(&o)
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		if o.Kind != "" {
			objs = append(objs, o)
		}
	}
	return objs
}

func find(t *testing.T, objs []object, kind, name string) object {
	t.Helper()
	for _, o := range objs {
		if o.Kind == kind && (o.Metadata.Name == name || ((kind == "ConfigMap" || kind == "Secret") && len(o.Metadata.Name) > len(name) && o.Metadata.Name[:len(name)+1] == name+"-")) {
			return o
		}
	}
	require.Failf(t, "resource not found", "%s %s", kind, name)
	return object{}
}

func TestRenderedNamespaces(t *testing.T) {
	cases := []struct {
		name, dir, server, runner string
		// secret is true where the kustomization generates the
		// turnip-github-app Secret, as the documented one does.
		secret bool
	}{
		{"base", "../../deploy/base", "turnip-system", "turnip-system", false},
		{"release", "../../deploy/overlays/release", "turnip-system", "turnip-system", false},
		{"kind", "../../deploy/overlays/kind", "turnip-system", "turnip-runners", false},
		{"operator", "testdata/operator", "turnip-system", "turnip-system", true},
		{"renamed", "testdata/renamed", "ops", "ops", true},
		{"separated", "testdata/separated", "ops", "ops-runners", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			objs := render(t, tc.dir)

			for _, r := range [][2]string{
				{"Deployment", "turnip-server"},
				{"Service", "turnip-server"},
				{"ServiceAccount", "turnip-server"},
				{"ConfigMap", "turnip-server-config"},
			} {
				assert.Equal(t, tc.server, find(t, objs, r[0], r[1]).Metadata.Namespace, "%s %s", r[0], r[1])
			}
			assert.Equal(t, tc.runner, find(t, objs, "Role", "turnip-server").Metadata.Namespace)
			rb := find(t, objs, "RoleBinding", "turnip-server")
			assert.Equal(t, tc.runner, rb.Metadata.Namespace)
			require.Len(t, rb.Subjects, 1)
			assert.Equal(t, tc.server, rb.Subjects[0].Namespace)

			crb := find(t, objs, "ClusterRoleBinding", "turnip-server-token-review")
			assert.Empty(t, crb.Metadata.Namespace)
			require.Len(t, crb.Subjects, 1)
			assert.Equal(t, tc.server, crb.Subjects[0].Namespace)
			assert.Empty(t, find(t, objs, "ClusterRole", "turnip-server-token-review").Metadata.Namespace)

			cm := find(t, objs, "ConfigMap", "turnip-server-config")
			// Unset unless the Runner namespace is separated: the Server
			// then uses its own namespace.
			if tc.runner == tc.server {
				assert.NotContains(t, cm.Data, "TURNIP_RUNNER_NAMESPACE")
			} else {
				assert.Equal(t, tc.runner, cm.Data["TURNIP_RUNNER_NAMESPACE"])
			}
			assert.NotContains(t, cm.Data, "TURNIP_RUNNER_SERVER_ADDR")

			dep := find(t, objs, "Deployment", "turnip-server")
			require.Len(t, dep.Spec.Template.Spec.Containers, 1)
			env := dep.Spec.Template.Spec.Containers[0].Env
			var names []string
			for _, e := range env {
				names = append(names, e.Name)
			}
			i := indexOf(names, "POD_NAMESPACE")
			require.GreaterOrEqual(t, i, 0, "POD_NAMESPACE missing")
			require.Less(t, i+1, len(env))
			require.NotNil(t, env[i].ValueFrom)
			require.NotNil(t, env[i].ValueFrom.FieldRef)
			assert.Equal(t, "metadata.namespace", env[i].ValueFrom.FieldRef.FieldPath)
			assert.Equal(t, "TURNIP_RUNNER_SERVER_ADDR", env[i+1].Name)
			assert.Equal(t, "turnip-server.$(POD_NAMESPACE).svc:9090", env[i+1].Value)

			if !tc.secret {
				return
			}
			secret := find(t, objs, "Secret", "turnip-github-app")
			assert.Equal(t, tc.server, secret.Metadata.Namespace)
			var refs int
			for _, e := range env {
				if e.ValueFrom != nil && e.ValueFrom.SecretKeyRef != nil {
					refs++
					assert.Equal(t, secret.Metadata.Name, e.ValueFrom.SecretKeyRef.Name, e.Name)
				}
			}
			assert.Equal(t, 2, refs, "secretKeyRef entries")
		})
	}
}

func indexOf(s []string, v string) int {
	for i, x := range s {
		if x == v {
			return i
		}
	}
	return -1
}
