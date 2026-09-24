package orchestrator

import (
	"fmt"
	"sort"
	"strings"
)

// aggregateCheckName is the Aggregate_Check's name: the one check an
// operator requires. It names no Operation, because Operations are named
// in each tool's own vocabulary and the verdict spans tools
// (aggregate-check-run Requirement 1).
const aggregateCheckName = "turnip"

// aggregateVerdict is what the Aggregate_Check shows. Conclusion is empty
// while the verdict is not completed.
type aggregateVerdict struct {
	Status     string
	Conclusion string
	Title      string
	Summary    string
}

func (v aggregateVerdict) completed() bool {
	return v.Status == "completed"
}

// verdictFor is a pure function of the record. The rows are evaluated in
// order, and the order is the precedence: a configuration problem
// outranks everything, and any failure outranks success.
//
// `failure` is reserved for something the pull request's author can fix
// by changing it (Requirements 5.6, 7): a failed apply, an invalid
// turnip.yaml, an affected Project whose tool this Server cannot run, or
// one asking for an override this Server does not permit. A
// plan awaiting its apply, a Project waiting on another pull request's
// Lock, a plan that has not yet succeeded — none of those is red, because
// reviewers skip pull requests showing a failure.
func verdictFor(st prStatus) aggregateVerdict {
	names := make([]string, 0, len(st.Projects))
	for name := range st.Projects {
		names = append(names, name)
	}
	sort.Strings(names)

	done, failed := 0, 0
	var unsupported, refused []string
	for _, name := range names {
		switch o := st.Projects[name].Outcome; {
		case o.done():
			done++
		case o == OutcomeUnsupported:
			unsupported = append(unsupported, name)
		case o == OutcomeRefused:
			refused = append(refused, name)
		case o == OutcomeApplyFailed:
			failed++
		}
	}
	summary := verdictSummary(st, names)

	switch {
	case st.ConfigInvalid:
		return aggregateVerdict{
			Status: "completed", Conclusion: "failure",
			Title:   invalidConfigTitle(),
			Summary: "The turnip configuration on this commit is invalid, so nothing could be planned. The pull request comment has the details.",
		}
	case st.Empty && len(names) == 0:
		return aggregateVerdict{
			Status: "completed", Conclusion: "skipped",
			Title:   noProjectsAffectedTitle(),
			Summary: "No project's files changed in this pull request, so there is nothing to plan or apply.",
		}
	case len(unsupported) > 0:
		// The first by name, which is also the first line of the summary
		// beneath it.
		first := unsupported[0]
		return aggregateVerdict{
			Status: "completed", Conclusion: "failure",
			Title:   unsupportedTitle(first, st.Projects[first].Tool, len(unsupported)-1),
			Summary: summary,
		}
	case len(refused) > 0:
		// After unsupported only so one Title is chosen when both occur:
		// each is a configuration failure, and either Title is true.
		first := refused[0]
		return aggregateVerdict{
			Status: "completed", Conclusion: "failure",
			Title:   refusedTitle(first, st.Projects[first].Setting, len(refused)-1),
			Summary: summary,
		}
	case failed > 0:
		return aggregateVerdict{
			Status: "completed", Conclusion: "failure",
			Title:   aggregateTitle(done, len(names), failed),
			Summary: summary,
		}
	case len(names) > 0 && done == len(names):
		return aggregateVerdict{
			Status: "completed", Conclusion: "success",
			Title:   aggregateTitle(done, len(names), 0),
			Summary: summary,
		}
	default:
		return aggregateVerdict{
			Status:  "in_progress",
			Title:   aggregateTitle(done, len(names), 0),
			Summary: summary,
		}
	}
}

// shouldPublish decides whether the Aggregate_Check appears at all
// (Requirement 6.1). Before any apply, a pull request under review shows
// GitHub's "Expected" for a required `turnip` — blocking, and not red —
// which is quieter and just as honest as an in-progress check. Once
// published, it is kept current.
func shouldPublish(st prStatus, v aggregateVerdict) bool {
	return st.CheckRunID != 0 || st.Mutated || v.completed()
}

func verdictSummary(st prStatus, names []string) string {
	var b strings.Builder
	for _, name := range names {
		e := st.Projects[name]
		fmt.Fprintf(&b, "- `%s`: %s", name, outcomeText(e))
		if e.Operation != "" {
			fmt.Fprintf(&b, " (`%s`)", checkRunName(name, e.Operation))
		}
		b.WriteString("\n")
	}
	return b.String()
}

func outcomeText(e ProjectEntry) string {
	switch e.Outcome {
	case OutcomeAwaitingApply:
		return "planned, awaiting apply"
	case OutcomeNothingToApply:
		return "planned, nothing to apply"
	case OutcomeNotPlanned:
		if e.BlockedBy != 0 {
			return fmt.Sprintf("not planned, locked by PR #%d", e.BlockedBy)
		}
		return "not planned"
	case OutcomeApplied:
		return "applied"
	case OutcomeApplyFailed:
		return "apply failed"
	case OutcomeUnsupported:
		return fmt.Sprintf("tool `%s` is not supported by this server; fix turnip.yaml", e.Tool)
	case OutcomeRefused:
		return fmt.Sprintf("%s is not permitted; change turnip.yaml, or permit it in TURNIP_ALLOWED_OVERRIDES", e.Setting)
	default:
		return string(e.Outcome)
	}
}
