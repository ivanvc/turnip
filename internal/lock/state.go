package lock

// Event is something that happened to an Operation, as the Lock sees it.
//
// Plan and mutating timeouts are separate events because they move
// differently: a plan that timed out changes nothing, while a mutating
// Operation that timed out may have changed infrastructure and may still
// be running.
type Event string

const (
	// EventPlanDispatched is a plan starting, not finishing. It is applied
	// when the Lock is acquired for a plan, which is what invalidates a
	// stored plan at the push rather than minutes later when the Runner
	// reports.
	EventPlanDispatched Event = "plan_dispatched"

	// EventPlanApplicable is a successful plan that left something to
	// apply — it reported changes, or the Project's tool can act without
	// them.
	EventPlanApplicable Event = "plan_applicable"

	// EventPlanNothingToApply is a successful plan that left nothing to
	// apply: no changes, and a tool whose mutating Operations are inert
	// without them.
	EventPlanNothingToApply Event = "plan_nothing_to_apply"

	EventPlanFailed   Event = "plan_failed"
	EventPlanTimedOut Event = "plan_timed_out"

	EventMutatingSucceeded Event = "mutating_succeeded"
	EventMutatingFailed    Event = "mutating_failed"
	EventMutatingTimedOut  Event = "mutating_timed_out"

	// EventUnlocked and EventPullRequestClosed release from any state.
	// They decide nothing today, and they take this path anyway: two
	// callers that never consult the table are how the table stops being
	// the whole story.
	EventUnlocked          Event = "unlocked"
	EventPullRequestClosed Event = "pull_request_closed"
)

// Transition is what applying an Event did to a Lock. To is empty when
// Released, because a released Lock has no state — it has no key.
type Transition struct {
	From     LockState
	To       LockState
	Released bool
}

// edge identifies one cell of the table.
type edge struct {
	from  LockState
	event Event
}

// outcome is that cell's value.
type outcome struct {
	to       LockState
	released bool
}

// released is the outcome shared by every edge that ends a Lock's life.
var released = outcome{released: true}

// table is the whole of this slice's behavior.
//
// Four cells are deliberately absent: a plan *result* arriving in
// StatePlanReady. Every result is preceded by EventPlanDispatched, which
// moves StatePlanReady to StatePlanStale first, so those combinations do
// not arise in an ordinary sequence. They are absent rather than defined
// so that applyEvent reports them, instead of a wrong guess being made
// silently — see its "not expected" contract.
var table = map[edge]outcome{
	// Nothing has been established. A plan that fails or finds nothing
	// releases, because releasing takes nothing from anyone.
	{StatePlanning, EventPlanDispatched}:     {to: StatePlanning},
	{StatePlanning, EventPlanApplicable}:     {to: StatePlanReady},
	{StatePlanning, EventPlanNothingToApply}: released,
	{StatePlanning, EventPlanFailed}:         released,
	{StatePlanning, EventPlanTimedOut}:       {to: StatePlanning},
	{StatePlanning, EventMutatingSucceeded}:  released,
	// A mutating Operation cannot be admitted from StatePlanning, so these
	// two arrive only after a race — an unlock and re-acquire beneath an
	// Operation already in flight. They move to StatePlanStale rather than
	// staying put, because infrastructure may have been touched and a
	// later failed plan must not then release the Lock.
	{StatePlanning, EventMutatingFailed}:    {to: StatePlanStale},
	{StatePlanning, EventMutatingTimedOut}:  {to: StatePlanStale},
	{StatePlanning, EventUnlocked}:          released,
	{StatePlanning, EventPullRequestClosed}: released,

	// A plan is recorded and trustworthy. Dispatching another plan means a
	// new commit, so the stored one is superseded at once.
	{StatePlanReady, EventPlanDispatched}:    {to: StatePlanStale},
	{StatePlanReady, EventMutatingSucceeded}: released,
	// A failed apply leaves infrastructure partly changed, so the plan
	// describes a transition from a state that no longer exists. The Lock
	// stays held — another pull request must not apply on top of an
	// unknown state — but stops being appliable.
	{StatePlanReady, EventMutatingFailed}: {to: StatePlanStale},
	// A timed-out apply needs this more than a failed one: the Runner may
	// still be executing.
	{StatePlanReady, EventMutatingTimedOut}:  {to: StatePlanStale},
	{StatePlanReady, EventUnlocked}:          released,
	{StatePlanReady, EventPullRequestClosed}: released,

	// A plan was recorded and something invalidated it. A failure here
	// holds the Lock: this pull request established work, and evicting it
	// over a typo is what the whole state distinction exists to prevent.
	{StatePlanStale, EventPlanDispatched}:     {to: StatePlanStale},
	{StatePlanStale, EventPlanApplicable}:     {to: StatePlanReady},
	{StatePlanStale, EventPlanNothingToApply}: released,
	{StatePlanStale, EventPlanFailed}:         {to: StatePlanStale},
	{StatePlanStale, EventPlanTimedOut}:       {to: StatePlanStale},
	{StatePlanStale, EventMutatingSucceeded}:  released,
	{StatePlanStale, EventMutatingFailed}:     {to: StatePlanStale},
	{StatePlanStale, EventMutatingTimedOut}:   {to: StatePlanStale},
	{StatePlanStale, EventUnlocked}:           released,
	{StatePlanStale, EventPullRequestClosed}:  released,
}

// applyEvent looks ev up against from.
//
// The second return is false when the combination is not one the table
// expects, which a caller must treat as "change nothing" rather than as a
// reason to guess. That is not merely defensive: the four absent cells are
// reachable by a genuine race, where a result arrives for an Operation the
// Lock has already moved past.
func ApplyEvent(from LockState, ev Event) (Transition, bool) {
	out, ok := table[edge{from, ev}]
	if !ok {
		return Transition{From: from, To: from}, false
	}
	if out.released {
		return Transition{From: from, Released: true}, true
	}
	return Transition{From: from, To: out.to}, true
}

// AllStates and AllEvents exist so the table's test can enumerate every
// combination rather than the ones someone remembered to write down.
func AllStates() []LockState {
	return []LockState{StatePlanning, StatePlanReady, StatePlanStale}
}

func AllEvents() []Event {
	return []Event{
		EventPlanDispatched, EventPlanApplicable, EventPlanNothingToApply,
		EventPlanFailed, EventPlanTimedOut,
		EventMutatingSucceeded, EventMutatingFailed, EventMutatingTimedOut,
		EventUnlocked, EventPullRequestClosed,
	}
}
