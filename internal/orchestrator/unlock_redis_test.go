package orchestrator

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivanvc/turnip/internal/lock"
)

// disallowedImageYAML names an image the Server's catalog does not allow,
// so the configuration is invalid.
const disallowedImageYAML = `
schemaVersion: v1alpha3
projects:
  - name: web
    directory: web
    uses: ghcr.io/not-allowed/helmfile@latest
`

// Feature: fix-unlock-without-config, checkpoint 4.1. The pilot's case
// end to end: the real Redis lock manager on miniredis, a pull request
// whose configuration is invalid, and an unlock that still works.
func TestHandleIssueComment_UnlockWithInvalidConfigurationAgainstRedis(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{name: "bare turnip unlock", body: "/turnip unlock"},
		{name: "tool word with a name", body: "/helmfile unlock web"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			o := testCommentOrchestrator(t, nil)
			locks := lock.NewRedisLockManager(o.redis)
			o.locks = locks
			client := &fakeCommentEventClient{permission: "write", pr: openPR()}
			client.files = map[string][]byte{"turnip.yaml": []byte(disallowedImageYAML)}

			ok, _, err := locks.AcquireForPlan(ctx, "owner/repo/web", 42, "https://example.test/pull/42", "alice")
			require.NoError(t, err)
			require.True(t, ok)
			ok, _, err = locks.AcquireForPlan(ctx, "owner/repo/api", 7, "https://example.test/pull/7", "bob")
			require.NoError(t, err)
			require.True(t, ok)

			require.NoError(t, callHandleIssueComment(o, client, commentEvent(tc.body, "alice")))

			web, err := locks.GetLockStatus(ctx, "owner/repo/web")
			require.NoError(t, err)
			require.NotNil(t, web)
			assert.False(t, web.Locked, "web is released in Redis")
			api, err := locks.GetLockStatus(ctx, "owner/repo/api")
			require.NoError(t, err)
			require.NotNil(t, api)
			assert.True(t, api.Locked, "api is left alone")
			assert.Equal(t, 7, api.PRNumber)

			posted := client.postedComments()
			require.Len(t, posted, 1)
			assert.Equal(t, "Unlocked:\n- web\n", posted[0])
			assert.NotContains(t, posted[0], "configuration")
			assert.Empty(t, client.getFileCallLog(), "the configuration is never fetched")
			assertNothingDispatched(t, o)
		})
	}
}
