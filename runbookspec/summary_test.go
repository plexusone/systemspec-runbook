package runbookspec

import (
	"strings"
	"testing"
	"time"
)

func done(l *Log, rb *Runbook, t *testing.T, step string, min int, typ EventType) {
	t.Helper()
	if err := l.Append(rb, Event{Step: step, Type: typ, At: at(min), By: OwnerAgent}); err != nil {
		t.Fatal(err)
	}
}

func TestStateOfARunOnceRunbook(t *testing.T) {
	rb := sample()
	l := &Log{Runbook: rb.ID}
	st := StateOf(rb, l)
	if st.Recurring || st.Steps != 3 || len(st.Runs) != 1 || st.Current.Outcome != OutcomeNotStarted || st.Current.Started != nil {
		t.Fatalf("empty: %+v", st)
	}
	done(l, rb, t, "0.1", 1, EventDone)
	done(l, rb, t, "0.2", 2, EventFailed)
	st = StateOf(rb, l)
	if st.Current.Outcome != OutcomeFailed || !st.Current.Started.Equal(at(1)) || !st.Current.LastEvent.Equal(at(2)) || st.Current.Events != 2 {
		t.Errorf("failed: %+v", st.Current)
	}
	done(l, rb, t, "0.2", 3, EventDone)
	if st = StateOf(rb, l); st.Current.Outcome != OutcomeInProgress || st.Current.States[StateDone] != 2 || st.Current.States[StateReady] != 1 {
		t.Errorf("in progress: %+v", st.Current)
	}
	done(l, rb, t, "1.1", 4, EventVerified)
	st = StateOf(rb, l)
	if st.Current.Outcome != OutcomeFinished || !st.Current.Finished.Equal(at(4)) || !st.LastFinished.Equal(at(4)) {
		t.Errorf("finished: %+v", st)
	}
	if err := l.Restart(rb, RunStart{At: at(5)}); err == nil || !strings.Contains(err.Error(), "run once") {
		t.Errorf("restarted a run-once runbook: %v", err)
	}
}

func TestRestartBeginsANewRun(t *testing.T) {
	rb := sample()
	rb.Recurring = true
	l := &Log{Runbook: rb.ID}
	for i, s := range []string{"0.1", "0.2", "1.1"} {
		done(l, rb, t, s, i+1, EventDone)
	}
	for _, m := range []int{2, 3} {
		if err := l.Restart(rb, RunStart{At: at(m), By: OwnerOperator}); err == nil {
			t.Errorf("a run began at minute %d, not after the last event", m)
		}
	}
	if got := l.NextRunStart(at(3)); !got.Equal(at(3).Add(time.Second)) {
		t.Errorf("next run start %v", got)
	}
	if err := l.Restart(rb, RunStart{At: at(10), By: OwnerOperator, Note: "v1.2.0"}); err != nil {
		t.Fatal(err)
	}
	// The new run's steps are ready or blocked again.
	var states []string
	for _, s := range Statuses(rb, l) {
		states = append(states, s.Step+" "+string(s.State))
	}
	if got := strings.Join(states, ", "); got != "0.1 ready, 0.2 ready, 1.1 blocked" {
		t.Errorf("after restart: %s", got)
	}
	done(l, rb, t, "0.1", 11, EventDone)
	st := StateOf(rb, l)
	if len(st.Runs) != 2 || st.Runs[0].Outcome != OutcomeFinished || st.Runs[0].Events != 3 {
		t.Fatalf("runs: %+v", st.Runs)
	}
	cur := st.Current
	if cur.N != 2 || cur.Outcome != OutcomeInProgress || !cur.Started.Equal(at(10)) || cur.By != OwnerOperator || cur.Note != "v1.2.0" || cur.Events != 1 {
		t.Errorf("current: %+v", cur)
	}
	// The last finished run is the first.
	if !st.LastFinished.Equal(at(3)) {
		t.Errorf("last finished %v", st.LastFinished)
	}
	if err := l.Restart(rb, RunStart{At: at(10)}); err == nil {
		t.Error("two runs began at once")
	}
	if err := l.Restart(rb, RunStart{At: at(12), Note: "github_pat_11AAAAAAAAAAAAAAAAAAAAAAAA"}); err == nil {
		t.Error("a note that looks like a secret was kept")
	}
	if err := l.Validate(rb); err != nil {
		t.Errorf("validate: %v", err)
	}
	rb.Recurring = false
	if err := l.Validate(rb); err == nil || !strings.Contains(err.Error(), "run once, but its log has 2 runs") {
		t.Errorf("validate a run-once runbook with runs: %v", err)
	}
}
