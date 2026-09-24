package orchestrator

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ivanvc/turnip/internal/github"
	"github.com/ivanvc/turnip/internal/rpc"
)

func TestTitles(t *testing.T) {
	scoped := []string{"-l", "name=api"}
	cases := map[string]struct{ got, want string }{
		"completed with changes":   {completedTitle(github.ChangeCounts{Add: 1, Change: 4, Destroy: 2}, nil), "+1 ~4 -2"},
		"completed with none":      {completedTitle(github.ChangeCounts{}, nil), "no changes"},
		"completed, scoped":        {completedTitle(github.ChangeCounts{Change: 1}, scoped), "+0 ~1 -0, -l name=api"},
		"running plan":             {runningTitle(nil), "running"},
		"running plan, scoped":     {runningTitle(scoped), "running, -l name=api"},
		"running recorded plan":    {runningRecordedPlanTitle(github.ChangeCounts{Add: 1, Change: 4, Destroy: 2}, nil), "running the recorded plan, +1 ~4 -2"},
		"running recorded, scoped": {runningRecordedPlanTitle(github.ChangeCounts{Change: 1}, scoped), "running the recorded plan, +0 ~1 -0, -l name=api"},
		"tool exited":              {failedTitle("helmfile", rpc.FailureToolExited, 1), "helmfile exited 1"},
		"clone failed":             {failedTitle("helmfile", rpc.FailureCloneFailed, -1), "clone failed"},
		"workspace failed":         {failedTitle("helmfile", rpc.FailureWorkspaceFailed, -1), "workspace could not be prepared"},
		"tool not started":         {failedTitle("helmfile", rpc.FailureToolNotStarted, -1), "helmfile could not be started"},
		"unspecified failure":      {failedTitle("helmfile", rpc.FailureUnspecified, -1), "failed"},
		"job not created":          {jobNotCreatedTitle(), "Runner Job could not be created"},
		"timeout":                  {timeoutTitle("Job x: container stuck (ImagePullBackOff)"), "Job x: container stuck (ImagePullBackOff)"},
		"aggregate":                {aggregateTitle(1, 2, 0), "1/2 projects up to date"},
		"aggregate with a failure": {aggregateTitle(1, 2, 1), "1/2 projects up to date, 1 failed"},
		"unsupported, one":         {unsupportedTitle("infra", "terraform", 0), "unsupported tool: infra uses terraform"},
		"unsupported, several":     {unsupportedTitle("infra", "terraform", 2), "unsupported tool: infra uses terraform, and 2 more"},
		"nothing affected":         {noProjectsAffectedTitle(), "no projects affected"},
		"invalid configuration":    {invalidConfigTitle(), "invalid turnip.yaml"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.got)
			assert.NotContains(t, tc.got, "·")
			assert.NotContains(t, tc.got, "-1", "turnip's own exit-code marker never reaches a title")
		})
	}
}

// A long scope is truncated the way the comment truncates it, and stays
// plain text.
func TestTitles_ScopeIsTruncatedAndPlain(t *testing.T) {
	title := runningTitle([]string{"-l", "name=<" + strings.Repeat("x", 100) + ">"})
	assert.True(t, strings.HasPrefix(title, "running, -l name=<xxx"))
	assert.True(t, strings.HasSuffix(title, "…"))
	assert.NotContains(t, title, "&lt;")
}
