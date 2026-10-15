package orchestrator

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivanvc/turnip/internal/github"
)

// teamClient answers as GitHub did for an App with no organization
// permission and an author whose access comes through a team: the
// collaborator answer leaves them out, the permission answer reports their
// role.
func teamClient(role string) *fakeCommentEventClient {
	notListed := false
	return &fakeCommentEventClient{
		permission:   role,
		collaborator: &notListed,
		files:        map[string][]byte{"turnip.yaml": []byte(twoReplicaYAML)},
		pr:           openPR(),
	}
}

func teamEvent(body string, private bool) *github.WebhookEvent {
	event := commentEvent(body, "alice")
	event.Repository.Private = private
	return event
}

// Task 4.1: the gate, the write check and the replies together. Each piece
// can be right on its own while an author with team access is still
// refused; this takes the comments through the orchestrator.
func TestTeamCollaborator_WriterOnPrivateRepositoryPlansThenApplies(t *testing.T) {
	rc := newTestRedisClient(t)
	jobsClient := &fakeJobCreator{t: t}
	client := teamClient("write")
	o := newReplica(t, rc, &fakeLockManager{}, jobsClient, client)
	ctx := context.Background()

	require.NoError(t, o.HandleIssueComment(ctx, teamEvent("/turnip diff web", true)))
	diffID := awaitOperation(t, o, "web", "diff")
	collaborator, permission := client.questionCounts()
	assert.Equal(t, 1, collaborator, "the diff asks the collaborator answer once")
	assert.Equal(t, 1, permission, "the diff asks the permission answer once")
	deliver(t, o, diffID, "diff output")
	awaitNoTrigger(t, o)

	require.NoError(t, o.HandleIssueComment(ctx, teamEvent("/helmfile apply web", true)))
	applyID := awaitOperation(t, o, "web", "apply")
	deliver(t, o, applyID, "apply output")
	awaitNoTrigger(t, o)

	collaborator, permission = client.questionCounts()
	assert.Equal(t, 2, collaborator, "one collaborator question per comment")
	assert.Equal(t, 2, permission,
		"one permission question per comment: the apply's write check reads the gate's answer")
	assert.Equal(t, 2, jobsClient.createCount())
	posted := client.postedComments()
	require.Len(t, posted, 2)
	assert.Contains(t, posted[0], "diff output")
	assert.Contains(t, posted[1], "apply output")
}

func TestTeamCollaborator_ReadOnPublicRepositoryIsAnOutsider(t *testing.T) {
	rc := newTestRedisClient(t)
	jobsClient := &fakeJobCreator{t: t}
	client := teamClient("read")
	o := newReplica(t, rc, &fakeLockManager{}, jobsClient, client)

	require.NoError(t, o.HandleIssueComment(context.Background(), teamEvent("/turnip diff web", false)))

	require.Equal(t, []string{"@alice does not have permission to trigger operations on this repository."}, client.postedComments())
	assert.Zero(t, jobsClient.createCount())
	assert.Empty(t, client.getFileCallLog(), "a refused trigger must not read the configuration")
}

func TestTeamCollaborator_TriageOnPublicRepositoryPlansButCannotApply(t *testing.T) {
	rc := newTestRedisClient(t)
	jobsClient := &fakeJobCreator{t: t}
	client := teamClient("triage")
	o := newReplica(t, rc, &fakeLockManager{}, jobsClient, client)
	ctx := context.Background()

	require.NoError(t, o.HandleIssueComment(ctx, teamEvent("/turnip diff web", false)))
	diffID := awaitOperation(t, o, "web", "diff")
	deliver(t, o, diffID, "diff output")
	awaitNoTrigger(t, o)
	require.Equal(t, 1, jobsClient.createCount())

	require.NoError(t, o.HandleIssueComment(ctx, teamEvent("/helmfile apply web", false)))
	require.Eventually(t, func() bool { return len(client.postedComments()) >= 2 }, 5*time.Second, time.Millisecond)

	posted := client.postedComments()
	assert.Contains(t, posted[0], "diff output")
	assert.Contains(t, posted[1], `write permission is required to run "apply"`)
	assert.NotContains(t, posted[1], "could not check")
	assert.Equal(t, 1, jobsClient.createCount(), "the apply did not run")
}

// A failed write lookup is asked once per command, not once per Project,
// and its error reaches the Server's log, which the reply sends the reader
// to.
func TestTeamCollaborator_WriteLookupErrorIsAskedOnceAndLogged(t *testing.T) {
	logs := captureLogs(t)
	rc := newTestRedisClient(t)
	jobsClient := &fakeJobCreator{t: t}
	listed := true
	client := &fakeCommentEventClient{
		collaborator:  &listed,
		permissionErr: errors.New("rate limited"),
		files:         map[string][]byte{"turnip.yaml": []byte(twoReplicaYAML)},
		pr:            openPR(),
	}
	o := newReplica(t, rc, &fakeLockManager{}, jobsClient, client)

	require.NoError(t, o.HandleIssueComment(context.Background(), teamEvent("/turnip apply web api", true)))
	require.Eventually(t, func() bool { return len(client.postedComments()) >= 1 }, 5*time.Second, time.Millisecond)

	_, permission := client.questionCounts()
	assert.Equal(t, 1, permission, "two Projects, one permission question")
	posted := client.postedComments()
	require.Len(t, posted, 1)
	assert.Equal(t, 2, strings.Count(posted[0], `could not check write permission to run "apply"; comment again to retry`))
	assert.Zero(t, jobsClient.createCount())

	record := findRecord(logs(), "checking write permission")
	require.NotNil(t, record, "the lookup's error is logged")
	assert.Equal(t, "rate limited", record["error"])
	assert.Equal(t, "alice", record["actor"])
	assert.Equal(t, "apply", record["operation"])
}
