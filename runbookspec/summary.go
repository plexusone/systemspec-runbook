package runbookspec

import "time"

// RunOutcome is how a run stands.
type RunOutcome string

// The outcomes.
const (
	// OutcomeNotStarted: no step of the run has an event.
	OutcomeNotStarted RunOutcome = "not-started"
	// OutcomeInProgress: the run began and not every step is done.
	OutcomeInProgress RunOutcome = "in-progress"
	// OutcomeFailed: the run's last event is a failure.
	OutcomeFailed RunOutcome = "failed"
	// OutcomeFinished: every step is done or verified.
	OutcomeFinished RunOutcome = "finished"
)

// Run is one run of a runbook, as its events show it.
type Run struct {
	// N counts the runs from 1.
	N int `json:"n"`
	// Started is when the run began: its RunStart, or its first event.
	Started *time.Time `json:"started,omitempty"`
	// By is who began it, when a RunStart says.
	By Owner `json:"by,omitempty"`
	// Note is the RunStart's note.
	Note string `json:"note,omitempty"`
	// LastEvent is when its last event was recorded.
	LastEvent *time.Time `json:"lastEvent,omitempty"`
	// Finished is when its last step was done or verified, once every
	// step is.
	Finished *time.Time `json:"finished,omitempty"`
	Outcome  RunOutcome `json:"outcome"`
	// Events counts its events.
	Events int `json:"events"`
	// States counts its steps by state.
	States map[State]int `json:"states"`
}

// RunbookState is a runbook's state at a glance: its current run, every
// run before it, and when it last finished.
type RunbookState struct {
	Runbook   string `json:"runbook"`
	Title     string `json:"title"`
	Recurring bool   `json:"recurring"`
	// Steps counts the runbook's steps.
	Steps int `json:"steps"`
	// Current is the run whose events give the steps' states.
	Current Run `json:"current"`
	// Runs are every run, oldest first; the last is Current.
	Runs []Run `json:"runs"`
	// LastFinished is when the latest finished run finished.
	LastFinished *time.Time `json:"lastFinished,omitempty"`
}

// StateOf returns the runbook's state from its log.
func StateOf(rb *Runbook, l *Log) RunbookState {
	if l == nil {
		l = &Log{Runbook: rb.ID}
	}
	s := RunbookState{Runbook: rb.ID, Title: rb.Title, Recurring: rb.Recurring, Steps: len(rb.Steps())}
	for i, events := range l.runs() {
		r := run(rb, events, i+1)
		if i > 0 {
			rs := l.Runs[i-1]
			at := rs.At
			r.Started, r.By, r.Note = &at, rs.By, rs.Note
		}
		if r.Finished != nil {
			s.LastFinished = r.Finished
		}
		s.Runs = append(s.Runs, r)
	}
	s.Current = s.Runs[len(s.Runs)-1]
	return s
}

func run(rb *Runbook, events []Event, n int) Run {
	r := Run{N: n, Events: len(events), States: map[State]int{}, Outcome: OutcomeNotStarted}
	sts := statuses(rb, events)
	finished := len(sts) > 0
	for _, st := range sts {
		r.States[st.State]++
		finished = finished && st.State.Finished()
	}
	if len(events) == 0 {
		return r
	}
	first, last := events[0].At, events[len(events)-1].At
	r.Started, r.LastEvent = &first, &last
	switch {
	case finished:
		r.Outcome, r.Finished = OutcomeFinished, &last
	case events[len(events)-1].Type == EventFailed:
		r.Outcome = OutcomeFailed
	default:
		r.Outcome = OutcomeInProgress
	}
	return r
}
