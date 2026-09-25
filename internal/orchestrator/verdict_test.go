package orchestrator

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"pgregory.net/rapid"
)

func statusWith(entries map[string]Outcome) prStatus {
	st := prStatus{Projects: map[string]ProjectEntry{}}
	for name, outcome := range entries {
		st.Projects[name] = ProjectEntry{Outcome: outcome, Operation: "diff"}
		if outcome.mutating() {
			st.Mutated = true
		}
	}
	return st
}

func TestVerdictFor(t *testing.T) {
	cases := []struct {
		name       string
		st         prStatus
		status     string
		conclusion string
		title      string
	}{
		{
			name:   "awaiting apply is in progress, not red",
			st:     statusWith(map[string]Outcome{"web": OutcomeAwaitingApply, "api": OutcomeApplied}),
			status: "in_progress", title: "1/2 projects up to date",
		},
		{
			name:   "a Project waiting on another pull request's Lock is in progress, not red",
			st:     statusWith(map[string]Outcome{"web": OutcomeNotPlanned, "api": OutcomeApplied}),
			status: "in_progress", title: "1/2 projects up to date",
		},
		{
			name:   "applied and nothing to apply are both done",
			st:     statusWith(map[string]Outcome{"web": OutcomeApplied, "api": OutcomeNothingToApply}),
			status: "completed", conclusion: "success", title: "2/2 projects up to date",
		},
		{
			name:   "a failed apply fails",
			st:     statusWith(map[string]Outcome{"web": OutcomeApplyFailed, "api": OutcomeApplied}),
			status: "completed", conclusion: "failure", title: "1/2 projects up to date, 1 failed",
		},
		{
			name:   "a refused override fails, naming the Project and setting",
			st:     prStatus{Projects: map[string]ProjectEntry{"web": {Outcome: OutcomeRefused, Setting: "runner.serviceAccount"}}},
			status: "completed", conclusion: "failure", title: "not permitted: web sets runner.serviceAccount",
		},
		{
			name:   "nothing affected is skipped",
			st:     prStatus{Empty: true, Projects: map[string]ProjectEntry{}},
			status: "completed", conclusion: "skipped", title: "no projects affected",
		},
		{
			name:   "an invalid configuration fails",
			st:     prStatus{ConfigInvalid: true, Projects: map[string]ProjectEntry{}},
			status: "completed", conclusion: "failure", title: "invalid turnip.yaml",
		},
		{
			name:   "an empty record is not success",
			st:     prStatus{Projects: map[string]ProjectEntry{}},
			status: "in_progress", title: "0/0 projects up to date",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := verdictFor(tc.st)
			assert.Equal(t, tc.status, v.Status)
			assert.Equal(t, tc.conclusion, v.Conclusion)
			assert.Equal(t, tc.title, v.Title)
		})
	}
}

func TestVerdictFor_Precedence(t *testing.T) {
	// A configuration problem outranks everything.
	st := statusWith(map[string]Outcome{"web": OutcomeApplied})
	st.ConfigInvalid = true
	assert.Equal(t, "invalid turnip.yaml", verdictFor(st).Title)

	// A Project planned by name after an empty match stops the skip
	// (Requirement 3.3).
	st = statusWith(map[string]Outcome{"web": OutcomeAwaitingApply})
	st.Empty = true
	assert.Equal(t, "in_progress", verdictFor(st).Status)

	// A refusal outranks a failed apply, which outranks success.
	st = statusWith(map[string]Outcome{"web": OutcomeApplyFailed, "api": OutcomeApplied})
	st.Projects["db"] = ProjectEntry{Outcome: OutcomeRefused, Setting: overrideCloneSubmodules}
	v := verdictFor(st)
	assert.Equal(t, "failure", v.Conclusion)
	assert.Equal(t, "not permitted: db sets clone.submodules", v.Title)

	delete(st.Projects, "db")
	v = verdictFor(st)
	assert.Equal(t, "failure", v.Conclusion)
	assert.Equal(t, "1/2 projects up to date, 1 failed", v.Title)

	delete(st.Projects, "web")
	assert.Equal(t, "success", verdictFor(st).Conclusion)
}

// The refused Title names the first refused Project by name and counts the
// rest (check-run-refusals Requirement 3.4).
func TestVerdictFor_RefusedTitleNamesTheFirstAndCountsTheRest(t *testing.T) {
	st := prStatus{Projects: map[string]ProjectEntry{
		"web":   {Outcome: OutcomeRefused, Setting: "runner.serviceAccount"},
		"api":   {Outcome: OutcomeRefused, Setting: overrideCloneSubmodules},
		"cache": {Outcome: OutcomeRefused, Setting: "runner.serviceAccount"},
		"db":    {Outcome: OutcomeApplied},
	}}
	v := verdictFor(st)
	assert.Equal(t, "not permitted: api sets clone.submodules, and 2 more", v.Title)
	assert.Contains(t, v.Summary, "`cache`", "the summary still lists every one")
	assert.Contains(t, v.Summary, "`web`")
}

func TestVerdictFor_SummaryNamesTheBlockerAndTheSetting(t *testing.T) {
	st := prStatus{Projects: map[string]ProjectEntry{
		"web":   {Outcome: OutcomeNotPlanned, Operation: "diff", BlockedBy: 5},
		"api":   {Outcome: OutcomeRefused, Operation: "diff", Setting: "runner.serviceAccount"},
		"infra": {Outcome: OutcomeNotPlanned, Operation: "plan"},
	}}
	assert.Equal(t,
		"- `api`: runner.serviceAccount is not permitted; change turnip.yaml, or permit it in TURNIP_ALLOWED_OVERRIDES (`turnip/diff/api`)\n"+
			"- `infra`: not planned (`turnip/plan/infra`)\n"+
			"- `web`: not planned, locked by PR #5 (`turnip/diff/web`)\n",
		verdictFor(st).Summary)
}

func TestOutcomeRefused_IsNeitherDoneNorMutating(t *testing.T) {
	assert.False(t, OutcomeRefused.done())
	assert.False(t, OutcomeRefused.mutating())
}

func TestVerdictFor_SummaryNamesEachProjectCheck(t *testing.T) {
	st := prStatus{Projects: map[string]ProjectEntry{
		"web": {Outcome: OutcomeApplied, Operation: "sync"},
		"api": {Outcome: OutcomeAwaitingApply, Operation: "diff"},
	}}
	assert.Equal(t,
		"- `api`: planned, awaiting apply (`turnip/diff/api`)\n"+
			"- `web`: applied (`turnip/sync/web`)\n",
		verdictFor(st).Summary)
}

func TestShouldPublish(t *testing.T) {
	awaiting := statusWith(map[string]Outcome{"web": OutcomeAwaitingApply})
	assert.False(t, shouldPublish(awaiting, verdictFor(awaiting)), "plans under review publish nothing")

	mutated := statusWith(map[string]Outcome{"web": OutcomeApplied, "api": OutcomeAwaitingApply})
	assert.True(t, shouldPublish(mutated, verdictFor(mutated)), "the first apply publishes")

	nothing := statusWith(map[string]Outcome{"web": OutcomeNothingToApply})
	assert.True(t, shouldPublish(nothing, verdictFor(nothing)), "a completed verdict publishes with no apply to come")

	published := statusWith(map[string]Outcome{"web": OutcomeAwaitingApply})
	published.CheckRunID = 9
	assert.True(t, shouldPublish(published, verdictFor(published)), "once published, kept current")
}

// Feature: aggregate-check-run, Property 3: No Outcome but applied or
// nothing-to-apply lets the verdict succeed
func TestProperty_OnlyDoneOutcomesSucceed(t *testing.T) {
	outcomes := []Outcome{
		OutcomeAwaitingApply, OutcomeNothingToApply, OutcomeNotPlanned,
		OutcomeApplied, OutcomeApplyFailed, OutcomeRefused,
	}
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.IntRange(1, 6).Draw(t, "n")
		st := prStatus{Projects: map[string]ProjectEntry{}}
		allDone := true
		for i := range n {
			o := rapid.SampledFrom(outcomes).Draw(t, "outcome")
			st.Projects[string(rune('a'+i))] = ProjectEntry{Outcome: o}
			allDone = allDone && o.done()
		}
		assert.Equal(t, allDone, verdictFor(st).Conclusion == "success")
	})
}
