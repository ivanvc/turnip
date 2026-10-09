package jobs

import (
	"math"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
	"pgregory.net/rapid"

	"github.com/ivanvc/turnip/internal/config"
)

const testRepositoryID = int64(123456789)

// testParamsWithRepository is testParams with a repository id, as the
// orchestrator passes it from the webhook.
func testParamsWithRepository(tool string) OperationParams {
	params := testParams(tool)
	params.RepositoryID = testRepositoryID
	return params
}

func testProjectWithKeys(tool string, keys ...string) config.Project {
	project := testProject(tool)
	project.Runner.Secrets = config.SecretKeys(keys)
	return project
}

func repositorySecretsVolumeOf(job *batchv1.Job) (corev1.Volume, bool) {
	for _, v := range job.Spec.Template.Spec.Volumes {
		if v.Name == repositorySecretsVolumeName {
			return v, true
		}
	}
	return corev1.Volume{}, false
}

// mountsRepositorySecrets reports whether c mounts the repository's
// Secret, by volume name or by a path at or under its directory.
func mountsRepositorySecrets(c corev1.Container) bool {
	for _, m := range c.VolumeMounts {
		if m.Name == repositorySecretsVolumeName ||
			m.MountPath == repositorySecretsMountPath ||
			strings.HasPrefix(m.MountPath, repositorySecretsMountPath+"/") {
			return true
		}
	}
	return false
}

func TestRepositorySecretName(t *testing.T) {
	for id, want := range map[int64]string{
		1:              "turnip-repository-1",
		123456789:      "turnip-repository-123456789",
		math.MaxInt64:  "turnip-repository-9223372036854775807",
		10_000_000_000: "turnip-repository-10000000000",
	} {
		assert.Equal(t, want, RepositorySecretName(id), "id %d", id)
	}
}

// The volume projects exactly the listed keys, in the order listed, each
// to a file named after its key, from the repository's own Secret. Not
// optional, so a missing Secret or key keeps the Pod from starting.
func TestBuildJob_RepositorySecretsVolume(t *testing.T) {
	for tool := range testSpecs {
		t.Run(tool, func(t *testing.T) {
			job, err := BuildJob(testProjectWithKeys(tool, "lab-kubeconfig", "cloud-key"), testParamsWithRepository(tool))
			require.NoError(t, err)

			v, ok := repositorySecretsVolumeOf(job)
			require.True(t, ok, "volumes: %v", volumeNames(job))
			assert.Equal(t, corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: "turnip-repository-123456789",
					Items: []corev1.KeyToPath{
						{Key: "lab-kubeconfig", Path: "lab-kubeconfig"},
						{Key: "cloud-key", Path: "cloud-key"},
					},
					DefaultMode: ptr.To(int32(0o444)),
					Optional:    ptr.To(false),
				},
			}, v.VolumeSource)
		})
	}
}

// Only the runner container mounts the keys, read-only, at the fixed
// directory. The clone and the provisioning containers never see them.
func TestBuildJob_RepositorySecretsMountedInRunnerOnly(t *testing.T) {
	for tool := range testSpecs {
		t.Run(tool, func(t *testing.T) {
			job, err := BuildJob(testProjectWithKeys(tool, "lab-kubeconfig"), testParamsWithRepository(tool))
			require.NoError(t, err)

			main := mainContainer(t, job)
			assert.Equal(t, runnerContainerName, main.Name)
			var mounts []corev1.VolumeMount
			for _, m := range main.VolumeMounts {
				if m.Name == repositorySecretsVolumeName {
					mounts = append(mounts, m)
				}
			}
			assert.Equal(t, []corev1.VolumeMount{{
				Name:      repositorySecretsVolumeName,
				MountPath: "/turnip/repository-secrets",
				ReadOnly:  true,
			}}, mounts)

			require.NotEmpty(t, job.Spec.Template.Spec.InitContainers)
			for _, c := range job.Spec.Template.Spec.InitContainers {
				assert.False(t, mountsRepositorySecrets(c), "init container %q mounts the repository's Secret", c.Name)
			}
		})
	}
}

// The strategy-specific mounts are untouched: the keys add one mount to
// the runner container and change nothing else about either shape.
func TestBuildJob_RepositorySecretsAddOnlyTheirMount(t *testing.T) {
	for tool := range testSpecs {
		t.Run(tool, func(t *testing.T) {
			without, err := BuildJob(testProject(tool), testParamsWithRepository(tool))
			require.NoError(t, err)
			with, err := BuildJob(testProjectWithKeys(tool, "lab-kubeconfig"), testParamsWithRepository(tool))
			require.NoError(t, err)

			assert.Equal(t, without.Spec.Template.Spec.InitContainers, with.Spec.Template.Spec.InitContainers)
			assert.Equal(t,
				append(mainContainer(t, without).VolumeMounts, corev1.VolumeMount{
					Name: repositorySecretsVolumeName, MountPath: repositorySecretsMountPath, ReadOnly: true,
				}),
				mainContainer(t, with).VolumeMounts)
		})
	}
}

// An absent list and an empty one both mean no keys: no volume, no
// mount, and no need for a repository id.
func TestBuildJob_NoKeysNoRepositorySecretsVolume(t *testing.T) {
	for tool := range testSpecs {
		for name, keys := range map[string]config.SecretKeys{
			"absent": nil,
			"empty":  {},
		} {
			for _, id := range []int64{0, testRepositoryID} {
				t.Run(tool+"/"+name+"/id="+strconv.FormatInt(id, 10), func(t *testing.T) {
					project := testProject(tool)
					project.Runner.Secrets = keys
					params := testParams(tool)
					params.RepositoryID = id

					job, err := BuildJob(project, params)
					require.NoError(t, err)

					_, ok := repositorySecretsVolumeOf(job)
					assert.False(t, ok, "volumes: %v", volumeNames(job))
					spec := job.Spec.Template.Spec
					for _, c := range append(slices.Clone(spec.InitContainers), spec.Containers...) {
						assert.False(t, mountsRepositorySecrets(c), "container %q", c.Name)
					}
				})
			}
		}
	}
}

// Without a repository id there is no Secret that could be the
// repository's, so a Project listing keys is refused rather than mounted
// from "turnip-repository-0".
func TestBuildJob_KeysWithoutRepositoryIDAreRefused(t *testing.T) {
	for tool := range testSpecs {
		t.Run(tool, func(t *testing.T) {
			params := testParams(tool)
			params.RepositoryID = 0

			job, err := BuildJob(testProjectWithKeys(tool, "lab-kubeconfig"), params)
			require.Error(t, err)
			assert.Nil(t, job)
			assert.NotContains(t, err.Error(), "turnip-repository-0")
			assert.NotContains(t, err.Error(), "lab-kubeconfig", "the message stays short and names no key")
		})
	}
}

// Feature: repository-secrets, Property 3: Secret name comes from the repository id alone
//
// Nothing a pull request controls (the Project's name, directory, env or
// the keys it lists) takes part in the Secret's name: it is always
// RepositorySecretName of the id the webhook supplied.
func TestProperty_RepositorySecretNameFromIDAlone(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		project := genProject(t)
		keys := rapid.SliceOfNDistinct(rapid.StringMatching(`[-._a-zA-Z0-9]{1,20}`), 1, 5, rapid.ID[string]).
			Filter(func(ks []string) bool {
				for _, k := range ks {
					if k == "." || strings.HasPrefix(k, "..") {
						return false
					}
				}
				return true
			}).Draw(t, "keys")
		project.Runner.Secrets = config.SecretKeys(keys)
		project.Runner.Env = map[string]string{
			"SECRET_NAME": rapid.StringMatching(`turnip-repository-[0-9]{1,12}`).Draw(t, "envSecretName"),
		}
		params := genOperationParams(t, project.Tool)
		params.RepositoryID = rapid.Int64Range(1, math.MaxInt64).Draw(t, "repositoryID")

		job, err := BuildJob(project, params)
		require.NoError(t, err)

		v, ok := repositorySecretsVolumeOf(job)
		require.True(t, ok)
		require.NotNil(t, v.Secret)
		require.Equal(t, RepositorySecretName(params.RepositoryID), v.Secret.SecretName)
		require.Equal(t, "turnip-repository-"+strconv.FormatInt(params.RepositoryID, 10), v.Secret.SecretName)

		got := make([]string, 0, len(v.Secret.Items))
		for _, item := range v.Secret.Items {
			require.Equal(t, item.Key, item.Path)
			got = append(got, item.Key)
		}
		require.Equal(t, keys, got, "exactly the listed keys, in order")
	})
}
