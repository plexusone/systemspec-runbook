package runbookspec

// State is where a step stands, derived from its events and its
// dependencies.
type State string

// The states.
const (
	// StateBlocked: a dependency is not done, and the step has not begun.
	StateBlocked State = "blocked"
	// StateReady: every dependency is done, and the step has not begun.
	StateReady State = "ready"
	// StateInProgress: the step began, or failed, and is not done.
	StateInProgress State = "in-progress"
	// StateDone: the step was done or skipped.
	StateDone State = "done"
	// StateVerified: the step's checks passed, and nothing failed since.
	StateVerified State = "verified"
)

// Status is a step's derived state and the events behind it.
type Status struct {
	// Step is the step's ID.
	Step string
	// State is derived.
	State State
	// Last is the step's last event, if any.
	Last *Event
	// Blockers are the dependencies not done, when blocked.
	Blockers []string
}

// Finished reports whether the state counts as done for dependencies.
func (s State) Finished() bool { return s == StateDone || s == StateVerified }

// Statuses derives every step's status from the log, in runbook order.
func Statuses(rb *Runbook, l *Log) []Status {
	byStep := map[string][]Event{}
	if l != nil {
		for _, e := range l.Events {
			byStep[e.Step] = append(byStep[e.Step], e)
		}
	}
	states := map[string]State{}
	var out []Status
	for _, s := range rb.Steps() {
		st := status(s, byStep[s.ID], states)
		states[s.ID] = st.State
		out = append(out, st)
	}
	return out
}

// status derives one step's status; states holds the earlier steps'. The
// events are replayed in order: started and failed mean in progress, done
// and skipped mean done, verified means verified (the checks prove the step
// whether or not done was recorded), and reopened returns the step to
// ready or blocked.
func status(s *Step, events []Event, states map[string]State) Status {
	st := Status{Step: s.ID}
	if len(events) > 0 {
		last := events[len(events)-1]
		st.Last = &last
	}
	var cur State
	for _, e := range events {
		switch e.Type {
		case EventStarted, EventFailed:
			cur = StateInProgress
		case EventDone, EventSkipped:
			cur = StateDone
		case EventVerified:
			cur = StateVerified
		case EventReopened:
			cur = ""
		}
	}
	if cur != "" {
		st.State = cur
		return st
	}
	for _, d := range s.DependsOn {
		if !states[d].Finished() {
			st.Blockers = append(st.Blockers, d)
		}
	}
	if len(st.Blockers) > 0 {
		st.State = StateBlocked
	} else {
		st.State = StateReady
	}
	return st
}
