package runbookspec

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
)

// Failure is where a run stopped and why: the phase and step, the action
// and command that failed, its error and the end of its output, and the
// fixes whose patterns match. A failure before any command, such as a
// variable without a value, has no action or command.
type Failure struct {
	// Phase holds the step, or is the phase a phase run could not start.
	Phase *Phase
	// Step failed; nil when a phase run failed before its first step.
	Step *Step
	// Action and Command number the command that failed, from 1; zero
	// when no command ran.
	Action, Command int
	// Line is the failed command as a shell line.
	Line string
	// Err is the command's error, or why the run could not start.
	Err error
	// Output is the end of the command's output, its last lines.
	Output string
	// Fixes are the step's and then the runbook's fixes whose patterns
	// match the output or the error.
	Fixes []Fix

	msg string
}

// Error is the failure in one line, as the step's failed event notes it.
func (f *Failure) Error() string {
	if f.msg != "" {
		return "runbook: " + f.msg
	}
	return f.Err.Error()
}

// Unwrap returns the command's error.
func (f *Failure) Unwrap() error { return f.Err }

// tailLines is how many of a failed command's last output lines a report
// shows.
const tailLines = 20

// tail keeps the last bytes written to it, for a failure's report, while
// the output streams on to the terminal.
type tail struct {
	mu  sync.Mutex
	buf []byte
	max int
}

func (t *tail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if over := len(t.buf) - t.max; over > 0 {
		t.buf = t.buf[over:]
	}
	return len(p), nil
}

// lines returns the last n lines written, trimmed.
func (t *tail) lines(n int) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	ls := strings.Split(strings.TrimRight(string(t.buf), "\n"), "\n")
	if len(ls) > n {
		ls = ls[len(ls)-n:]
	}
	return strings.TrimSpace(strings.Join(ls, "\n"))
}

// command is how a report names the run command, such as "bin/infra
// runbook run"; "run" when the runner was not told.
func (r *Runner) command() string {
	if r.Command != "" {
		return r.Command
	}
	return "run"
}

// report shows a failure: where the run stopped, the command, its error
// and output, what to do, and how to go on.
func (r *Runner) report(ctx context.Context, f *Failure) {
	var b strings.Builder
	w := func(format string, args ...any) { fmt.Fprintf(&b, format, args...) }
	phase := ""
	if f.Phase != nil {
		phase = fmt.Sprintf("Phase %s (%s)", f.Phase.ID, f.Phase.Title)
	}
	switch {
	case f.Step == nil:
		w("\n%s could not start:\n", phase)
	case f.Command > 0:
		w("\n%s stopped at step %s %q, action %d, command %d:\n", phase, f.Step.ID, f.Step.Title, f.Action, f.Command)
		w("%s\n", indentLines(f.Line))
	default:
		w("\n%s stopped at step %s %q:\n", phase, f.Step.ID, f.Step.Title)
	}
	w("Error: %v\n", f.Err)
	var u *UnfilledError
	if errors.As(f.Err, &u) {
		for _, n := range u.Names {
			w("    %s\n", r.Values.describe(n))
		}
	}
	if f.Output != "" {
		w("%s\n", indentLines(f.Output))
	}
	if len(f.Fixes) > 0 {
		w("Fix:\n")
		for _, fx := range f.Fixes {
			w("  - %s\n", strings.ReplaceAll(r.show(ctx, strings.TrimSpace(fx.Fix)), "\n", "\n    "))
		}
	}
	if r.phase != "" {
		w("Resume the phase: %s %s\n", r.command(), r.phase)
	}
	if f.Step != nil {
		w("Retry the step:   %s %s\n", r.command(), f.Step.ID)
	}
	r.say("%s", strings.TrimRight(b.String(), "\n"))
}

// describe names a variable with where its value comes from, for a report
// of the variables without one.
func (v *Values) describe(name string) string {
	d, ok := v.declared[name]
	if !ok {
		return name
	}
	s := name + ": " + d.Description
	var from []string
	if d.Env != "" {
		from = append(from, "$"+d.Env)
	}
	from = append(from, "--var "+name+"=…")
	s += " (" + strings.Join(from, " or ")
	if d.Example != "" {
		s += ", such as " + d.Example
	}
	return s + ")"
}
