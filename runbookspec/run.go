package runbookspec

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"
)

// ErrStopped means a run stopped before the step was done, because a
// command or a person's confirmation was declined. The step stays in
// progress; run it again to continue.
var ErrStopped = errors.New("runbook: stopped before the step was done; it stays in progress")

// Prompt asks a yes-or-no question.
type Prompt func(question string) (bool, error)

// LinePrompt asks on out and reads the answer from in, one byte at a time,
// so it never takes input meant for a command that runs after it. Only
// "y" or "yes" is yes; the end of input is no.
func LinePrompt(in io.Reader, out io.Writer) Prompt {
	return func(question string) (bool, error) {
		if _, err := fmt.Fprintf(out, "%s [y/N] ", question); err != nil {
			return false, err
		}
		var line []byte
		b := make([]byte, 1)
		for {
			n, err := in.Read(b)
			if n == 1 {
				if b[0] == '\n' {
					break
				}
				line = append(line, b[0])
			}
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return false, err
			}
		}
		a := strings.ToLower(strings.TrimSpace(string(line)))
		return a == "y" || a == "yes", nil
	}
}

// Prepared is a command with its variables filled, ready to run.
type Prepared struct {
	// Args are the program and its arguments.
	Args []string
	// Dir is the working directory; empty is the runner's root.
	Dir string
	// Env are KEY=value pairs added to the environment.
	Env []string
	// From, when set, is the command whose output is piped in.
	From []string
	// RandomHex, when above zero, pipes in that many random bytes as
	// hexadecimal, never shown.
	RandomHex int
	// Line is the command as a shell line, for showing.
	Line string
}

// Executor runs a prepared command, with stdin as its standard input when
// it has no other, streaming its output.
type Executor func(ctx context.Context, p Prepared, stdin io.Reader, stdout, stderr io.Writer) error

// Runner runs one step: it shows each action with its variables filled,
// confirms and runs commands, waits on what a person does, records
// started, failed and done, and then runs the step's checks.
type Runner struct {
	// Values fill variables.
	Values *Values
	// Checker runs the step's checks after it is done; nil runs none.
	Checker *Checker
	// Root is the directory a command's Dir is relative to.
	Root string
	// As is who runs: commands of this owner, or of "both", run; another
	// owner's actions are confirmed as done by that owner. Default
	// operator.
	As Owner
	// Yes runs commands without asking before each one. Steps a person
	// does are still confirmed.
	Yes bool
	// Force runs a step whose dependencies are not done; the started
	// event says so.
	Force bool
	// Prompt asks yes-or-no questions; nil asks on Out and reads Stdin.
	Prompt Prompt
	// Stdin is the commands' standard input; nil is os.Stdin.
	Stdin io.Reader
	// Out receives what the run shows and the commands' output; nil is
	// os.Stdout.
	Out io.Writer
	// Exec runs commands; nil uses os/exec, without a shell.
	Exec Executor
	// Save persists the log after each event is appended.
	Save func(*Log) error
	// Now is the clock; nil is time.Now.
	Now func() time.Time
}

// Run runs a step of rb, appending its events to l.
func (r *Runner) Run(ctx context.Context, rb *Runbook, l *Log, id string) error {
	r.defaults()
	s := rb.Step(id)
	if s == nil {
		return fmt.Errorf("runbook: no step %s in %s", id, rb.ID)
	}
	st := statusOf(rb, l, id)
	note := ""
	switch {
	case st.State == StateBlocked && !r.Force:
		return fmt.Errorf("runbook: step %s waits for %s; finish them first, or run with --force", id, strings.Join(st.Blockers, ", "))
	case st.State == StateBlocked:
		note = "forced; waited for " + strings.Join(st.Blockers, ", ")
	case st.State.Finished():
		again, err := r.Prompt(fmt.Sprintf("Step %s is already %s. Run it again?", id, st.State))
		if err != nil || !again {
			return err
		}
	}

	// Fill every command's variables before anything runs or is recorded.
	prepared, err := r.prepare(ctx, s)
	if err != nil {
		return err
	}

	r.say("%s %s (owner: %s)", s.ID, s.Title, s.Owner)
	if s.Summary != "" {
		r.say("\n%s", r.show(ctx, s.Summary))
	}
	if err := r.record(rb, l, Event{Step: id, Type: EventStarted, Note: note}); err != nil {
		return err
	}

	for i, a := range s.Actions {
		owner := a.Owner
		if owner == "" {
			owner = s.Owner
		}
		r.say("\n[%d/%d] %s: %s", i+1, len(s.Actions), owner, r.show(ctx, strings.TrimSpace(a.Text)))
		if a.Command != "" {
			r.say("\n%s", indentLines(r.show(ctx, strings.TrimSpace(a.Command)), "    "))
		}
		if len(a.Exec) > 0 && r.runs(owner) {
			for j, p := range prepared[i] {
				if p.Args == nil {
					// A command with a later variable, produced by an
					// earlier command of this run.
					var err error
					if p, err = r.prepareOne(ctx, a.Exec[j]); err != nil {
						msg := fmt.Sprintf("action %d, command %d: %v", i+1, j+1, err)
						if rerr := r.record(rb, l, Event{Step: id, Type: EventFailed, Note: Redact(msg)}); rerr != nil {
							return errors.Join(err, rerr)
						}
						return fmt.Errorf("runbook: %s", msg)
					}
				}
				r.say("\n%s", prompted(p.Line))
				if !r.Yes {
					ok, err := r.Prompt("  Run this command?")
					if err != nil {
						return err
					}
					if !ok {
						return ErrStopped
					}
				}
				if err := r.Exec(ctx, p, r.Stdin, r.Out, r.Out); err != nil {
					msg := fmt.Sprintf("action %d, command %d (%s) failed: %v", i+1, j+1, p.Args[0], err)
					if rerr := r.record(rb, l, Event{Step: id, Type: EventFailed, Note: Redact(msg)}); rerr != nil {
						return errors.Join(err, rerr)
					}
					return fmt.Errorf("runbook: %s", msg)
				}
			}
			continue
		}
		if len(a.Exec) > 0 {
			r.say("\n%s", indentLines(ShellLines(a.Exec), "    "))
		}
		question := "  Done?"
		if !r.runs(owner) {
			question = fmt.Sprintf("  Has the %s done this?", owner)
		}
		ok, err := r.Prompt(question)
		if err != nil {
			return err
		}
		if !ok {
			return ErrStopped
		}
	}

	r.say("\nDone when %s", r.show(ctx, s.Done))
	if err := r.record(rb, l, Event{Step: id, Type: EventDone}); err != nil {
		return err
	}
	if r.Checker == nil || len(s.Checks) == 0 {
		return nil
	}
	r.say("\nChecks:")
	results := r.Checker.Step(ctx, s)
	for _, res := range results {
		r.say("  %-4s %s: %s", res.Outcome, res.Description, res.Detail)
	}
	if e, ok := VerifyEvent(statusOf(rb, l, id), results, r.Now(), r.As); ok {
		return r.record(rb, l, e)
	}
	return nil
}

func (r *Runner) defaults() {
	if r.As == "" {
		r.As = OwnerOperator
	}
	if r.Out == nil {
		r.Out = os.Stdout
	}
	if r.Stdin == nil {
		r.Stdin = os.Stdin
	}
	if r.Prompt == nil {
		r.Prompt = LinePrompt(r.Stdin, r.Out)
	}
	if r.Exec == nil {
		r.Exec = ExecCommand
	}
	if r.Now == nil {
		r.Now = time.Now
	}
	if r.Values == nil {
		r.Values = &Values{declared: map[string]Variable{}, lookupEnv: os.LookupEnv, cache: map[string]string{}}
	}
}

// runs reports whether the runner does an owner's actions.
func (r *Runner) runs(owner Owner) bool {
	return owner == r.As || owner == OwnerBoth || r.As == OwnerBoth
}

func (r *Runner) say(format string, args ...any) {
	// What the run shows is best effort: a closed terminal does not stop
	// the step, and each event is still saved.
	_, _ = fmt.Fprintf(r.Out, format+"\n", args...) //nolint:errcheck // see above
}

// show fills the variables it can and shows the rest as <name>.
func (r *Runner) show(ctx context.Context, s string) string {
	return varRef.ReplaceAllStringFunc(s, func(m string) string {
		name := varRef.FindStringSubmatch(m)[1]
		if v, ok, err := r.Values.Lookup(ctx, name); err == nil && ok {
			return v
		}
		return "<" + name + ">"
	})
}

func (r *Runner) record(rb *Runbook, l *Log, e Event) error {
	if e.At.IsZero() {
		e.At = r.Now().UTC().Truncate(time.Second)
	}
	if e.By == "" {
		e.By = r.As
	}
	if err := l.Append(rb, e); err != nil {
		return err
	}
	if r.Save == nil {
		return nil
	}
	return r.Save(l)
}

// prepare fills the variables of every command the runner will run,
// reporting every unfilled variable together.
func (r *Runner) prepare(ctx context.Context, s *Step) ([][]Prepared, error) {
	out := make([][]Prepared, len(s.Actions))
	var missing []string
	for i, a := range s.Actions {
		owner := a.Owner
		if owner == "" {
			owner = s.Owner
		}
		if !r.runs(owner) {
			continue
		}
		for _, e := range a.Exec {
			p, err := r.prepareOne(ctx, e)
			var u *UnfilledError
			switch {
			case errors.As(err, &u):
				missing = append(missing, u.Names...)
				// Filled again just before it runs.
				out[i] = append(out[i], Prepared{})
			case err != nil:
				return nil, err
			default:
				out[i] = append(out[i], p)
			}
		}
	}
	missing = slices.DeleteFunc(missing, r.Values.later)
	if len(missing) > 0 {
		sort.Strings(missing)
		return nil, r.Values.unfilled(compact(missing))
	}
	return out, nil
}

func (r *Runner) prepareOne(ctx context.Context, e Exec) (Prepared, error) {
	return prepareExec(ctx, r.Values, r.Root, e)
}

// prepareExec fills a command's variables, with its directory relative to
// root.
func prepareExec(ctx context.Context, v *Values, root string, e Exec) (Prepared, error) {
	args, err := v.ExpandAll(ctx, e.Args)
	if err != nil {
		return Prepared{}, err
	}
	dir, err := v.Expand(ctx, e.Dir)
	if err != nil {
		return Prepared{}, err
	}
	p := Prepared{Args: args}
	if dir != "" {
		p.Dir = filepath.Join(root, filepath.FromSlash(dir))
	} else {
		p.Dir = root
	}
	keys := make([]string, 0, len(e.Env))
	for k := range e.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	filled := Exec{Args: args, Dir: dir, Env: map[string]string{}}
	for _, k := range keys {
		val, err := v.Expand(ctx, e.Env[k])
		if err != nil {
			return Prepared{}, err
		}
		p.Env = append(p.Env, k+"="+val)
		filled.Env[k] = val
	}
	if in := e.Stdin; in != nil {
		if p.From, err = v.ExpandAll(ctx, in.From); err != nil {
			return Prepared{}, err
		}
		p.RandomHex = in.RandomHex
		filled.Stdin = &Stdin{From: p.From, RandomHex: in.RandomHex}
	}
	p.Line = ShellLines([]Exec{filled})
	return p, nil
}

func compact(sorted []string) []string {
	var out []string
	for _, s := range sorted {
		if len(out) == 0 || out[len(out)-1] != s {
			out = append(out, s)
		}
	}
	return out
}

// prompted shows a command's shell lines as typed at a prompt.
func prompted(lines string) string {
	return "  $ " + strings.ReplaceAll(lines, "\n", "\n  $ ")
}

func indentLines(s, pad string) string {
	return pad + strings.ReplaceAll(s, "\n", "\n"+pad)
}

func statusOf(rb *Runbook, l *Log, id string) Status {
	for _, s := range Statuses(rb, l) {
		if s.Step == id {
			return s
		}
	}
	return Status{Step: id}
}

// ExecCommand runs a prepared command with os/exec, never through a shell.
// Standard input is the command piped in, the random value, or stdin.
func ExecCommand(ctx context.Context, p Prepared, stdin io.Reader, stdout, stderr io.Writer) error {
	cmd := exec.CommandContext(ctx, p.Args[0], p.Args[1:]...) //nolint:gosec // G204: the runbook's own declared command, confirmed by the operator
	cmd.Dir = p.Dir
	cmd.Env = append(os.Environ(), p.Env...)
	cmd.Stdout, cmd.Stderr = stdout, stderr
	switch {
	case p.RandomHex > 0:
		b := make([]byte, p.RandomHex)
		if _, err := rand.Read(b); err != nil {
			return fmt.Errorf("generating a random value: %w", err)
		}
		cmd.Stdin = strings.NewReader(hex.EncodeToString(b))
	case len(p.From) > 0:
		src := exec.CommandContext(ctx, p.From[0], p.From[1:]...) //nolint:gosec // G204: as above
		src.Dir, src.Env, src.Stderr = p.Dir, cmd.Env, stderr
		pipe, err := src.StdoutPipe()
		if err != nil {
			return err
		}
		cmd.Stdin = pipe
		if err := src.Start(); err != nil {
			return fmt.Errorf("%s: %w", p.From[0], err)
		}
		runErr := cmd.Run()
		waitErr := src.Wait()
		if runErr != nil {
			return runErr
		}
		if waitErr != nil {
			return fmt.Errorf("%s: %w", p.From[0], waitErr)
		}
		return nil
	default:
		cmd.Stdin = stdin
	}
	return cmd.Run()
}
