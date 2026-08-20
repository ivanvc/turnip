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

func TestParseTriggers_UnrecognizedToolStillParses(t *testing.T) {
	got, err := ParseTriggers("/deploy plan")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, &TriggerCommand{Tool: "deploy", Operation: "plan"}, got[0])
}

func TestParseTriggers_MultipleWellFormedLinesBatchIntoOneComment(t *testing.T) {
	got, err := ParseTriggers("/turnip plan project-1\n/turnip plan project-2")
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, []string{"project-1"}, got[0].Projects)
	assert.Equal(t, []string{"project-2"}, got[1].Projects)
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
