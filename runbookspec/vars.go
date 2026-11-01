package runbookspec

import (
	"context"
	"errors"
	"fmt"
	"os"
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
}

func (e *UnfilledError) Error() string {
	return "runbook: no value for " + strings.Join(e.Names, ", ") + "; give each with --var name=value or its environment variable"
}

// Values fills variables from its sources, in order, and then from each
// declared variable's environment variable.
type Values struct {
	sources   []Source
	declared  map[string]Variable
	lookupEnv func(string) (string, bool)
	cache     map[string]string
}

// NewValues fills the runbook's variables from the sources, in order, and
// then from the environment.
func NewValues(rb *Runbook, sources ...Source) *Values {
	v := &Values{sources: sources, declared: map[string]Variable{}, lookupEnv: os.LookupEnv, cache: map[string]string{}}
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
	if d, ok := v.declared[name]; ok && d.Env != "" {
		if val, ok := v.lookupEnv(d.Env); ok && val != "" {
			v.cache[name] = val
			return val, true, nil
		}
	}
	return "", false, nil
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
		return "", &UnfilledError{Names: missing}
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
		return nil, &UnfilledError{Names: slices.Compact(missing)}
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
