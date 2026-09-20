package github

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseTriggers_SingleCommandExamples(t *testing.T) {
	tests := []struct {
		body string
		want *TriggerCommand
	}{
		{"/turnip plan", &TriggerCommand{Tool: "turnip", Operation: "plan"}},
		{"/terraform apply vpc-project", &TriggerCommand{Tool: "terraform", Operation: "apply", Projects: []string{"vpc-project"}}},
		{"/helmfile sync", &TriggerCommand{Tool: "helmfile", Operation: "sync"}},
		{"/pulumi preview", &TriggerCommand{Tool: "pulumi", Operation: "preview"}},
		{"/turnip plan -- -destroy", &TriggerCommand{Tool: "turnip", Operation: "plan", ExtraArgs: []string{"-destroy"}}},
	}

	for _, tt := range tests {
		t.Run(tt.body, func(t *testing.T) {
			got, err := ParseTriggers(tt.body)
			require.NoError(t, err)
			require.Len(t, got, 1)
			assert.Equal(t, tt.want, got[0])
		})
	}
}

func TestParseTriggers_MultipleProjectsAndExtraArgs(t *testing.T) {
	got, err := ParseTriggers("/turnip apply proj-a proj-b -- -destroy -auto-approve")
	require.NoError(t, err)
	want := &TriggerCommand{
		Tool:      "turnip",
		Operation: "apply",
		Projects:  []string{"proj-a", "proj-b"},
		ExtraArgs: []string{"-destroy", "-auto-approve"},
	}
	require.Len(t, got, 1)
	assert.Equal(t, want, got[0])
}

func TestParseTriggers_LiteralDoubleDashInExtraArgs(t *testing.T) {
	got, err := ParseTriggers("/turnip plan -- -destroy -- extra")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, []string{"-destroy", "--", "extra"}, got[0].ExtraArgs)
}

func TestParseTriggers_ExplanatoryTextAroundTriggerLine(t *testing.T) {
	got, err := ParseTriggers("please review this\n/turnip apply\nthanks!")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "turnip", got[0].Tool)
	assert.Equal(t, "apply", got[0].Operation)
}

func TestParseTriggers_NonTriggerCommentReturnsErrNoTrigger(t *testing.T) {
	got, err := ParseTriggers("just a regular comment, looks good to me")
	require.ErrorIs(t, err, ErrNoTrigger)
	assert.Nil(t, got)
}

func TestParseTriggers_MissingOperationReturnsMalformed(t *testing.T) {
	got, err := ParseTriggers("/turnip")
	assert.Nil(t, got)
	require.ErrorIs(t, err, ErrMalformedTrigger)

	var malformed MalformedTriggerErrors
	require.ErrorAs(t, err, &malformed)
	require.Len(t, malformed, 1)
	assert.Equal(t, 1, malformed[0].Line)
	assert.Equal(t, "/turnip", malformed[0].Content)
}

// A slash command addressed to some other bot — or a pasted path, or a
// convention like /lgtm — is not turnip's business. Treating these as
// triggers made turnip authorize the author, fetch turnip.yaml, and reply
// to people who never addressed it.
func TestParseTriggers_CommandForAnotherBotIsNotATrigger(t *testing.T) {
	for _, body := range []string{
		"/jira create ATO-1",
		"/deploy plan",
		"/lgtm",
		"/cc @teammate",
		"/etc/hosts is the file I meant",
	} {
		got, err := ParseTriggers(body)
		require.ErrorIs(t, err, ErrNoTrigger, "body: %q", body)
		assert.Nil(t, got, "body: %q", body)
	}
}

func TestParseTriggers_EveryKnownToolIsRecognized(t *testing.T) {
	for _, tool := range []string{"turnip", "terraform", "pulumi", "helmfile"} {
		got, err := ParseTriggers("/" + tool + " diff")
		require.NoError(t, err, "tool: %s", tool)
		require.Len(t, got, 1)
		assert.Equal(t, tool, got[0].Tool)
	}
}

func TestParseTriggers_UnknownToolAlongsideRealTriggerIsIgnored(t *testing.T) {
	got, err := ParseTriggers("/jira comment something\n/turnip diff web")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "turnip", got[0].Tool)
	assert.Equal(t, []string{"web"}, got[0].Projects)
}

func TestParseTriggers_MultipleWellFormedLinesBatchIntoOneComment(t *testing.T) {
	got, err := ParseTriggers("/turnip plan project-1\n/turnip plan project-2")
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, []string{"project-1"}, got[0].Projects)
	assert.Equal(t, []string{"project-2"}, got[1].Projects)
}

// A tool's own flags begin with "-", so the first such token is where the
// Project names stop. That is what lets a scoped trigger be written the
// way it would be typed in a shell, where it previously failed with an
// unmatched-Project error that read as turnip not knowing the Project.
//
// The explicit "--" keeps working because it is found by the same scan —
// being exactly "--" is the only thing that makes it consumed rather than
// passed through.
func TestParseTriggers_ArgumentsNeedNoDelimiter(t *testing.T) {
	tests := []struct {
		name         string
		body         string
		wantProjects []string
		wantArgs     []string
	}{
		{"short flag, no delimiter", "/turnip diff web -l name=x", []string{"web"}, []string{"-l", "name=x"}},
		{"the delimiter form is equivalent", "/turnip diff web -- -l name=x", []string{"web"}, []string{"-l", "name=x"}},
		{"no projects and no delimiter", "/turnip diff -l name=x", nil, []string{"-l", "name=x"}},
		{"long flag", "/turnip diff --selector name=x", nil, []string{"--selector", "name=x"}},
		{"several projects before the first flag", "/turnip diff web api -l name=x", []string{"web", "api"}, []string{"-l", "name=x"}},
		{"projects only, no arguments", "/turnip diff web api", []string{"web", "api"}, nil},
		{"an explicit delimiter is consumed, not passed through", "/turnip diff -- --selector x", nil, []string{"--selector", "x"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseTriggers(tt.body)
			require.NoError(t, err)
			require.Len(t, got, 1)
			assert.Equal(t, tt.wantProjects, got[0].Projects)
			assert.Equal(t, tt.wantArgs, got[0].ExtraArgs)
		})
	}
}

func TestParseTriggers_MalformedLineDoesNotDiscardWellFormedOnes(t *testing.T) {
	got, err := ParseTriggers("/turnip plan project-1\n/turnip\n/turnip plan project-2")

	require.Len(t, got, 2)
	assert.Equal(t, []string{"project-1"}, got[0].Projects)
	assert.Equal(t, []string{"project-2"}, got[1].Projects)

	var malformed MalformedTriggerErrors
	require.ErrorAs(t, err, &malformed)
	require.Len(t, malformed, 1)
	assert.Equal(t, 2, malformed[0].Line)
}
