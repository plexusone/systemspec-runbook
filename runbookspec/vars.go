package runbookspec

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"sort"
	"strings"
)

var (
	varRef  = regexp.MustCompile(`\{\{\s*([A-Za-z][A-Za-z0-9_.]*)\s*\}\}`)
	varName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.]*$`)
)

// References returns the variable names s references as {{name}}, in
// order of first use.
func References(s string) []string {
	var out []string
	for _, m := range varRef.FindAllStringSubmatch(s, -1) {
		if !slices.Contains(out, m[1]) {
			out = append(out, m[1])
		}
	}
	return out
}

// Placeholders shows each {{name}} as <name>, as a page or a preview
// shows a value not yet known.
func Placeholders(s string) string { return varRef.ReplaceAllString(s, "<$1>") }

// Source supplies variable values. It reports ok false for a name it does
// not know, so the next source is asked.
type Source func(ctx context.Context, name string) (value string, ok bool, err error)

// MapSource supplies fixed values, such as those given as flags.
func MapSource(values map[string]string) Source {
	return func(_ context.Context, name string) (string, bool, error) {
		v, ok := values[name]
		return v, ok, nil
	}
}

// UnfilledError names the variables no source could fill.
type UnfilledError struct {
	// Names are the variables, sorted.
	Names []string
	// Reasons say, for a variable declared with a command, why the command
	// gave no value.
	Reasons map[string]string
}

func (e *UnfilledError) Error() string {
	msg := "runbook: no value for " + strings.Join(e.Names, ", ") + "; give each with --var name=value or its environment variable"
	for _, n := range e.Names {
		if r, ok := e.Reasons[n]; ok {
			msg += fmt.Sprintf("; %s's command: %s", n, r)
		}
	}
	return msg
}

// Values fills variables from its sources, in order, then from each
// declared variable's environment variable, and then from its command.
type Values struct {
	// Root is the directory a variable's command's Dir is relative to;
	// "." when empty.
	Root string
	// Output runs a variable's command and returns its standard output;
	// nil runs it with os/exec, never through a shell.
	Output func(ctx context.Context, p Prepared) (string, error)

	sources   []Source
	declared  map[string]Variable
	lookupEnv func(string) (string, bool)
	cache     map[string]string
	reasons   map[string]string
	resolving map[string]bool
}

// NewValues fills the runbook's variables from the sources, in order, then
// from the environment, and then from their commands.
func NewValues(rb *Runbook, sources ...Source) *Values {
	v := &Values{sources: sources, declared: map[string]Variable{}, lookupEnv: os.LookupEnv,
		cache: map[string]string{}, reasons: map[string]string{}, resolving: map[string]bool{}}
	for _, d := range rb.Variables {
		v.declared[d.Name] = d
	}
	return v
}

// later reports whether a variable is declared as produced by an earlier
// command of a run.
func (v *Values) later(name string) bool { return v.declared[name].Later }

// Lookup returns a variable's value, and whether any source has one. A
// value no source has is not remembered, so a later lookup asks again.
func (v *Values) Lookup(ctx context.Context, name string) (string, bool, error) {
	if val, ok := v.cache[name]; ok {
		return val, true, nil
	}
	for _, src := range v.sources {
		val, ok, err := src(ctx, name)
		if err != nil {
			return "", false, fmt.Errorf("runbook: variable %s: %w", name, err)
		}
		if ok {
			v.cache[name] = val
			return val, true, nil
		}
	}
	d, ok := v.declared[name]
	if ok && d.Env != "" {
		if val, ok := v.lookupEnv(d.Env); ok && val != "" {
			v.cache[name] = val
			return val, true, nil
		}
	}
	if ok && d.From != nil {
		return v.fromCommand(ctx, name, *d.From)
	}
	return "", false, nil
}

// fromCommand runs a variable's command. A command that cannot be filled,
// fails or prints nothing leaves the variable unfilled, with the reason
// kept for the error that names it; it is not remembered, so it runs again
// the next time the value is needed.
func (v *Values) fromCommand(ctx context.Context, name string, e Exec) (string, bool, error) {
	if v.resolving[name] {
		return "", false, fmt.Errorf("runbook: variable %s: its command needs its own value", name)
	}
	v.resolving[name] = true
	defer delete(v.resolving, name)
	root := v.Root
	if root == "" {
		root = "."
	}
	p, err := prepareExec(ctx, v, root, e)
	var u *UnfilledError
	switch {
	case errors.As(err, &u):
		v.reasons[name] = "needs " + strings.Join(u.Names, ", ")
		return "", false, nil
	case err != nil:
		return "", false, err
	}
	run := v.Output
	if run == nil {
		run = commandOutput
	}
	out, err := run(ctx, p)
	line := strings.Join(p.Args, " ")
	if err != nil {
		v.reasons[name] = fmt.Sprintf("%s: %v", line, err)
		return "", false, nil
	}
	val := strings.TrimSpace(out)
	if val == "" {
		v.reasons[name] = line + ": printed nothing"
		return "", false, nil
	}
	delete(v.reasons, name)
	v.cache[name] = val
	return val, true, nil
}

// commandOutput runs a prepared command without a shell and returns its
// standard output; the error carries the first line of standard error.
func commandOutput(ctx context.Context, p Prepared) (string, error) {
	cmd := exec.CommandContext(ctx, p.Args[0], p.Args[1:]...) //nolint:gosec // G204: the runbook's own declared command
	cmd.Dir = p.Dir
	cmd.Env = append(os.Environ(), p.Env...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return "", fmt.Errorf("%w: %s", err, firstLine(msg))
		}
		return "", err
	}
	return stdout.String(), nil
}

// unfilled names variables without a value, with the reasons their
// commands gave.
func (v *Values) unfilled(names []string) *UnfilledError {
	e := &UnfilledError{Names: names}
	for _, n := range names {
		if r, ok := v.reasons[n]; ok {
			if e.Reasons == nil {
				e.Reasons = map[string]string{}
			}
			e.Reasons[n] = r
		}
	}
	return e
}

// Expand replaces every {{name}} in s, or returns an *UnfilledError
// naming each variable without a value.
func (v *Values) Expand(ctx context.Context, s string) (string, error) {
	var missing []string
	for _, name := range References(s) {
		if _, ok, err := v.Lookup(ctx, name); err != nil {
			return "", err
		} else if !ok {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return "", v.unfilled(missing)
	}
	return varRef.ReplaceAllStringFunc(s, func(m string) string {
		// Every name was looked up above without an error, and is cached.
		val := v.cache[varRef.FindStringSubmatch(m)[1]]
		return val
	}), nil
}

// ExpandAll expands each string, reporting every unfilled variable
// together.
func (v *Values) ExpandAll(ctx context.Context, in []string) ([]string, error) {
	out := make([]string, len(in))
	var missing []string
	for i, s := range in {
		e, err := v.Expand(ctx, s)
		var u *UnfilledError
		switch {
		case err == nil:
			out[i] = e
		case errors.As(err, &u):
			missing = append(missing, u.Names...)
		default:
			return nil, err
		}
	}
	if len(missing) > 0 {
		slices.Sort(missing)
		return nil, v.unfilled(slices.Compact(missing))
	}
	return out, nil
}

// strings returns every string of the step that may reference variables.
func (s *Step) strings() []string {
	out := []string{s.Summary, s.Done}
	out = append(out, s.Notes...)
	for _, a := range s.Actions {
		out = append(out, a.Text, a.Command)
		for _, e := range a.Exec {
			out = append(out, e.strings()...)
		}
	}
	for _, c := range s.Checks {
		out = append(out, c.strings()...)
	}
	return out
}

// strings returns the command's strings: arguments, directory,
// environment values and the command piped in.
func (e Exec) strings() []string {
	out := append([]string{e.Dir}, e.Args...)
	for _, v := range e.Env {
		out = append(out, v)
	}
	if e.Stdin != nil {
		out = append(out, e.Stdin.From...)
	}
	return out
}

func (c *Check) strings() []string {
	var out []string
	switch {
	case c.DNS != nil:
		out = append(append(out, c.DNS.Name), c.DNS.Contains...)
	case c.HTTP != nil:
		out = append(out, c.HTTP.URL, c.HTTP.Contains, c.HTTP.Location)
	case c.Unreachable != nil:
		out = append(out, c.Unreachable.Address, c.Unreachable.Timeout)
	case c.Command != nil:
		out = append(append(out, c.Command.Args...), c.Command.Contains)
	case c.Named != nil:
		for _, v := range c.Named.Args {
			out = append(out, v)
		}
	}
	return out
}
