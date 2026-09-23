package orchestrator

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"pgregory.net/rapid"
)

func statusWith(entries map[string]Outcome) prStatus {
	st := prStatus{Projects: map[string]ProjectEntry{}}
	for name, outcome := range entries {
		st.Projects[name] = ProjectEntry{Outcome: outcome, Operation: "diff", Tool: "helmfile"}
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
			status: "in_progress", title: "1/2 projects applied",
		},
		{
			name:   "a Project waiting on another pull request's Lock is in progress, not red",
			st:     statusWith(map[string]Outcome{"web": OutcomeNotPlanned, "api": OutcomeApplied}),
			status: "in_progress", title: "1/2 projects applied",
		},
		{
			name:   "applied and nothing to apply are both done",
			st:     statusWith(map[string]Outcome{"web": OutcomeApplied, "api": OutcomeNothingToApply}),
			status: "completed", conclusion: "success", title: "2/2 projects applied",
		},
		{
			name:   "a failed apply fails",
			st:     statusWith(map[string]Outcome{"web": OutcomeApplyFailed, "api": OutcomeApplied}),
			status: "completed", conclusion: "failure", title: "1/2 projects applied; an apply failed",
		},
		{
			name:   "an unsupported tool fails, naming the Project and tool",
			st:     prStatus{Projects: map[string]ProjectEntry{"infra": {Outcome: OutcomeUnsupported, Tool: "terraform"}}},
			status: "completed", conclusion: "failure", title: "unsupported tool: infra uses terraform",
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
			status: "in_progress", title: "0/0 projects applied",
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

	// Unsupported outranks a failed apply, which outranks success.
	st = statusWith(map[string]Outcome{"web": OutcomeApplyFailed})
	st.Projects["infra"] = ProjectEntry{Outcome: OutcomeUnsupported, Tool: "pulumi"}
	assert.Equal(t, "unsupported tool: infra uses pulumi", verdictFor(st).Title)
}

func TestVerdictFor_SummaryNamesEachProjectCheck(t *testing.T) {
	st := prStatus{Projects: map[string]ProjectEntry{
		"web":   {Outcome: OutcomeApplied, Operation: "sync"},
		"api":   {Outcome: OutcomeAwaitingApply, Operation: "diff"},
		"infra": {Outcome: OutcomeUnsupported, Tool: "terraform"},
	}}
	assert.Equal(t,
		"- `api`: planned, awaiting apply (`turnip/diff/api`)\n"+
			"- `infra`: tool `terraform` is not supported by this server — fix turnip.yaml\n"+
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
		OutcomeApplied, OutcomeApplyFailed, OutcomeUnsupported,
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
