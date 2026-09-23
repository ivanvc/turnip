package orchestrator

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivanvc/turnip/internal/github"
)

// captureLogs points the process-wide slog default at a buffer for the
// rest of the test, restoring the previous default afterwards, and returns
// a function that decodes every record written so far.
//
// Safe only because no test in this package runs in parallel: the default
// logger is global.
func captureLogs(t *testing.T) func() []map[string]any {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	return func() []map[string]any {
		var records []map[string]any
		sc := bufio.NewScanner(bytes.NewReader(buf.Bytes()))
		for sc.Scan() {
			var rec map[string]any
			require.NoError(t, json.Unmarshal(sc.Bytes(), &rec))
			records = append(records, rec)
		}
		return records
	}
}

func findRecord(records []map[string]any, msg string) map[string]any {
	for _, r := range records {
		if r["msg"] == msg {
			return r
		}
	}
	return nil
}

// A Kubernetes API failure used to reach only the pull request comment.
func TestExecuteOne_RejectionIsLoggedWithItsReason(t *testing.T) {
	logs := captureLogs(t)
	jobsClient := &fakeJobCreator{t: t, createErr: errors.New("forbidden: jobs.batch")}
	o, _ := testOrchestrator(t, &fakeLockManager{}, jobsClient)

	result := o.executeOne(context.Background(), &fakeExecuteClient{}, testRepo, testPR, 1, testHelmfileTarget())
	require.False(t, result.Success)

	rec := findRecord(logs(), "operation rejected")
	require.NotNil(t, rec)
	assert.Equal(t, "WARN", rec["level"])
	assert.Equal(t, result.Output, rec["reason"], "the log carries the same reason the comment shows")
	assert.Contains(t, rec["reason"], "forbidden: jobs.batch")
	assert.Equal(t, testHelmfileTarget().Project.Name, rec["project"])
	assert.InDelta(t, float64(testPR.Number), rec["pr_number"], 0)
}

func TestExecuteOne_DispatchAndFinishAreLoggedAtInfo(t *testing.T) {
	logs := captureLogs(t)
	want := github.ProjectResult{ProjectName: "helm-a", Tool: "helmfile", Operation: "diff", Success: true, Output: "secret-looking plan output"}
	o, _ := testOrchestrator(t, &fakeLockManager{}, &fakeJobCreator{t: t, result: want})

	o.executeOne(context.Background(), &fakeExecuteClient{}, testRepo, testPR, 1, testHelmfileTarget())

	records := logs()
	created := findRecord(records, "runner job created")
	require.NotNil(t, created)
	assert.Equal(t, "INFO", created["level"])
	assert.NotEmpty(t, created["operation_id"])
	assert.NotEmpty(t, created["job"])

	finished := findRecord(records, "operation finished")
	require.NotNil(t, finished)
	assert.Equal(t, "INFO", finished["level"])
	assert.Equal(t, "success", finished["outcome"])
	assert.Equal(t, created["operation_id"], finished["operation_id"])

	assert.Nil(t, findRecord(records, "operation rejected"))
	for _, r := range records {
		b, err := json.Marshal(r)
		require.NoError(t, err)
		assert.NotContains(t, string(b), want.Output, "tool output must never reach the log")
	}
}
