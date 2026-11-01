package runbookspec

import (
	"fmt"
	"strings"
	"time"
)

// Summary counts a step's check results.
func Summary(results []Result) (pass, fail, skip int) {
	for _, r := range results {
		switch r.Outcome {
		case Pass:
			pass++
		case Fail:
			fail++
		case Skip:
			skip++
		}
	}
	return pass, fail, skip
}

// VerifyEvent is what a verify run records for a step, given its status
// before the run: verified when every check passed; failed when a check
// failed on a step that was done or verified, since what was true is no
// longer. Otherwise, ok is false and nothing is recorded: a failing check
// on a step not yet done is expected, and skips prove nothing.
func VerifyEvent(st Status, results []Result, at time.Time, by Owner) (e Event, ok bool) {
	pass, fail, skip := Summary(results)
	switch {
	case pass > 0 && fail == 0 && skip == 0:
		return Event{Step: st.Step, Type: EventVerified, At: at, By: by, Note: fmt.Sprintf("%d of %d checks passed", pass, len(results))}, true
	case fail > 0 && st.State.Finished():
		var failed []string
		for _, r := range results {
			if r.Outcome == Fail {
				failed = append(failed, r.Check)
			}
		}
		return Event{Step: st.Step, Type: EventFailed, At: at, By: by, Note: "checks failed: " + strings.Join(failed, ", ")}, true
	}
	return Event{}, false
}
