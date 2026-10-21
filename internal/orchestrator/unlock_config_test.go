package orchestrator

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivanvc/turnip/internal/github"
)

// invalidTurnipYAML fails validation: a Project with no directory.
const invalidTurnipYAML = `
schemaVersion: v1alpha3
projects:
  - name: web
    uses: helmfile@v1.7.4
`

// assertNothingDispatched checks that no Job was created and no trigger
// was stored, which is what starting a trigger leaves behind.
func assertNothingDispatched(t *testing.T, o *Orchestrator) {
	t.Helper()
	assert.Zero(t, o.jobs.(*fakeJobCreator).createCount(), "no Runner Job is created")
	keys, err := o.redis.Keys(context.Background(), "trigger:*").Result()
	require.NoError(t, err)
	assert.Empty(t, keys, "no trigger is started")
}

// An unlock-only comment on a pull request whose configuration is invalid
// does not ask GitHub for the configuration and gets only the unlock's own
// reply.
func TestHandleIssueComment_UnlockOnlyFetchesNoConfiguration(t *testing.T) {
	_, client, o, released := unlockFixture(t, map[string]int{"web": 42})
	client.files = map[string][]byte{"turnip.yaml": []byte(invalidTurnipYAML)}

	require.NoError(t, callHandleIssueComment(o, client, commentEvent("/turnip unlock\n/helmfile unlock web", "alice")))

	assert.Empty(t, client.getFileCallLog(), "no configuration request is made to GitHub")
	assert.Equal(t, []string{"web"}, *released)
	posted := client.postedComments()
	require.Len(t, posted, 2)
	assert.Equal(t, "Unlocked:\n- web\n", posted[0])
	assert.Equal(t, "No projects matched this unlock command.", posted[1])
	for _, body := range posted {
		assert.NotContains(t, body, "configuration")
	}
	assertNothingDispatched(t, o)
}

// The configuration is fetched once, however many other commands the
// comment holds.
func TestHandleIssueComment_ConfigurationFetchedOnce(t *testing.T) {
	_, client, o, _ := unlockFixture(t, map[string]int{})
	client.files = map[string][]byte{"turnip.yaml": []byte(invalidTurnipYAML)}

	require.NoError(t, callHandleIssueComment(o, client, commentEvent("/turnip diff\n/turnip unlock\n/turnip apply", "alice")))

	// One request per accepted location until the file is found:
	// .turnip/config.yaml misses, then turnip.yaml is read.
	assert.Equal(t, []string{".turnip/config.yaml", "turnip.yaml"}, client.getFileCallLog())
}

// unlock beside diff with an invalid configuration: the unlock runs, the
// configuration reply is posted once for the diff, in command order, and
// nothing is dispatched.
func TestHandleIssueComment_UnlockBesideDiffWithInvalidConfiguration(t *testing.T) {
	const unlocked = "Unlocked:\n- web\n"
	for _, tc := range []struct {
		name        string
		body        string
		unlockIndex int
		configIndex int
	}{
		{name: "unlock first", body: "/turnip unlock\n/turnip diff\n/turnip diff web", unlockIndex: 0, configIndex: 1},
		{name: "diff first", body: "/turnip diff\n/turnip unlock\n/turnip diff web", unlockIndex: 1, configIndex: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, client, o, released := unlockFixture(t, map[string]int{"web": 42})
			client.files = map[string][]byte{"turnip.yaml": []byte(invalidTurnipYAML)}

			require.NoError(t, callHandleIssueComment(o, client, commentEvent(tc.body, "alice")))

			assert.Equal(t, []string{"web"}, *released)
			posted := client.postedComments()
			require.Len(t, posted, 2, "the configuration reply is posted once")
			assert.Equal(t, unlocked, posted[tc.unlockIndex])
			assert.Contains(t, posted[tc.configIndex], "The turnip configuration is invalid, so no operations ran.")
			assertNothingDispatched(t, o)
		})
	}
}

// A missing configuration answers the other command with the
// missing-configuration warning, and the unlock still runs.
func TestHandleIssueComment_UnlockBesideDiffWithMissingConfiguration(t *testing.T) {
	_, client, o, released := unlockFixture(t, map[string]int{"web": 42})

	require.NoError(t, callHandleIssueComment(o, client, commentEvent("/turnip unlock\n/turnip diff", "alice")))

	assert.Equal(t, []string{"web"}, *released)
	posted := client.postedComments()
	require.Len(t, posted, 2)
	assert.Equal(t, "Unlocked:\n- web\n", posted[0])
	assert.Contains(t, posted[1], "No turnip configuration was found")
	assertNothingDispatched(t, o)
}

// A malformed line and an invalid configuration post both replies, the
// malformed line's first.
func TestHandleIssueComment_MalformedLineAndInvalidConfiguration(t *testing.T) {
	_, client, o, _ := unlockFixture(t, map[string]int{})
	client.files = map[string][]byte{"turnip.yaml": []byte(invalidTurnipYAML)}

	require.NoError(t, callHandleIssueComment(o, client, commentEvent("/turnip\n/turnip diff", "alice")))

	posted := client.postedComments()
	require.Len(t, posted, 2)
	assert.Contains(t, posted[0], "couldn't be parsed")
	assert.Contains(t, posted[1], "The turnip configuration is invalid, so no operations ran.")
	assertNothingDispatched(t, o)
}

// The guards before the fetch are unchanged for unlock: a fork's pull
// request gets no reply and releases nothing, and a closed one gets the
// closed reply and releases nothing.
func TestHandleIssueComment_UnlockGuardsUnchanged(t *testing.T) {
	t.Run("fork", func(t *testing.T) {
		_, client, o, released := unlockFixture(t, map[string]int{"web": 42})
		client.pr.HeadRepo = github.Repository{Owner: "contributor", Name: "repo"}

		err := callHandleIssueComment(o, client, commentEvent("/turnip unlock", "alice"))

		require.ErrorIs(t, err, github.ErrRefused)
		assert.Empty(t, client.postedComments())
		assert.Empty(t, *released)
	})
	t.Run("closed", func(t *testing.T) {
		_, client, o, released := unlockFixture(t, map[string]int{"web": 42})
		client.pr = closedPR()

		err := callHandleIssueComment(o, client, commentEvent("/turnip unlock", "alice"))

		require.ErrorIs(t, err, github.ErrRefused)
		require.Equal(t, []string{closedPullRequestComment()}, client.postedComments())
		assert.Empty(t, *released)
	})
}
