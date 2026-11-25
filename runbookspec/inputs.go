package runbookspec

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
)

// Input is something a run needs that is not a {{variable}}: a value in
// the environment that commands read themselves, such as an API token, or
// a precondition a command checks, such as a key loaded in the SSH agent.
// A step names the inputs it needs; a run checks them before it starts.
type Input struct {
	// Name names the input, as steps' needs and the page refer to it,
	// such as "CLOUDFLARE_API_TOKEN" or "ssh-agent-key".
	Name string `json:"name"`
	// Description says what the input is.
	Description string `json:"description"`
	// Env, when set, is the environment variable that must hold a value.
	Env string `json:"env,omitempty"`
	// Secret marks a value that is never shown, written or logged: a run
	// checks only that it is set.
	Secret bool `json:"secret,omitempty"`
	// HowTo says how to get or set it up, in Markdown, such as the
	// console page and the permissions a token needs.
	HowTo string `json:"howTo,omitempty"`
	// Check, when set, is a command that must succeed, and print Contains
	// when that is set, for the input to be in place. It only reads, and
	// may reference variables.
	Check *CommandCheck `json:"check,omitempty"`
}

// Input returns the input with the name, or nil.
func (rb *Runbook) Input(name string) *Input {
	for i := range rb.Inputs {
		if rb.Inputs[i].Name == name {
			return &rb.Inputs[i]
		}
	}
	return nil
}

// Requirement is a variable or an input that steps need, and whether it
// is in place. It never holds a value.
type Requirement struct {
	// Kind is "variable" or "input".
	Kind string `json:"kind"`
	// Name is the variable's or the input's name.
	Name string `json:"name"`
	// Env is the environment variable that supplies it, if any.
	Env string `json:"env,omitempty"`
	// Description, Example and HowTo describe it.
	Description string `json:"description"`
	Example     string `json:"example,omitempty"`
	HowTo       string `json:"howTo,omitempty"`
	// Secret marks a value never shown or written.
	Secret bool `json:"secret,omitempty"`
	// Steps need it, in order.
	Steps []string `json:"steps"`
	// State is set, missing, derived (a command gives it when first
	// needed) or later (an earlier command of the run produces it).
	State string `json:"state"`
	// Detail says where it comes from, or why it is missing; never the
	// value.
	Detail string `json:"detail,omitempty"`
}

// The states of a requirement.
const (
	RequirementSet     = "set"
	RequirementMissing = "missing"
	RequirementDerived = "derived"
	RequirementLater   = "later"
)

// Missing returns the requirements that are missing.
func Missing(reqs []Requirement) []Requirement {
	var out []Requirement
	for _, r := range reqs {
		if r.State == RequirementMissing {
			out = append(out, r)
		}
	}
	return out
}

// Remaining returns the steps a run of target would run: a phase's steps,
// or with "all" every phase's, not yet done or verified; or the one step
// named, whatever its state.
func (rb *Runbook) Remaining(l *Log, target string) ([]*Step, error) {
	if s := rb.Step(target); s != nil {
		return []*Step{s}, nil
	}
	var steps []*Step
	switch p := rb.Phase(target); {
	case p != nil:
		for i := range p.Steps {
			steps = append(steps, &p.Steps[i])
		}
	case target == "all":
		steps = rb.Steps()
	default:
		return nil, fmt.Errorf("runbook: no step or phase %s in %s", target, rb.ID)
	}
	return slices.DeleteFunc(steps, func(s *Step) bool { return statusOf(rb, l, s.ID).State.Finished() }), nil
}

// Requirements returns every variable and input the steps need, and
// whether each is in place: variables their commands and checks reference
// in the actions this runner runs, then the inputs the steps name. It runs
// only inputs' checks: a variable given by a command is reported as
// derived, not run.
func (r *Runner) Requirements(ctx context.Context, rb *Runbook, steps []*Step) []Requirement {
	r.defaults()
	var vars, inputs []string
	usedBy := map[string][]string{}
	use := func(list *[]string, name, step string) {
		if !slices.Contains(*list, name) {
			*list = append(*list, name)
		}
		if !slices.Contains(usedBy[name], step) {
			usedBy[name] = append(usedBy[name], step)
		}
	}
	for _, s := range steps {
		for _, name := range r.stepVariables(s) {
			use(&vars, name, s.ID)
		}
		for _, n := range s.Needs {
			use(&inputs, n, s.ID)
		}
	}
	var out []Requirement
	declared := map[string]Variable{}
	for _, v := range rb.Variables {
		declared[v.Name] = v
	}
	for _, name := range vars {
		d := declared[name]
		req := Requirement{Kind: "variable", Name: name, Env: d.Env, Description: d.Description, Example: d.Example, Steps: usedBy[name]}
		req.State, req.Detail = r.Values.peek(ctx, d)
		out = append(out, req)
	}
	for _, name := range inputs {
		in := rb.Input(name)
		if in == nil {
			continue // Validate reports it
		}
		req := Requirement{Kind: "input", Name: name, Env: in.Env, Description: in.Description, HowTo: in.HowTo,
			Secret: in.Secret, Steps: usedBy[name]}
		req.State, req.Detail = r.inputState(ctx, in)
		out = append(out, req)
	}
	return out
}

// stepVariables are the variables the commands this runner runs, and the
// step's checks, reference.
func (r *Runner) stepVariables(s *Step) []string {
	var texts []string
	for _, a := range s.Actions {
		owner := a.Owner
		if owner == "" {
			owner = s.Owner
		}
		if !r.runs(owner) {
			continue
		}
		for _, e := range a.Exec {
			texts = append(texts, e.strings()...)
		}
	}
	for _, c := range s.Checks {
		texts = append(texts, c.strings()...)
	}
	var names []string
	for _, t := range texts {
		for _, n := range References(t) {
			if !slices.Contains(names, n) {
				names = append(names, n)
			}
		}
	}
	return names
}

// peek says whether a variable has a value without running its command:
// a flag, a source or the environment gives one, a command will, an earlier
// command of the run will, or none does.
func (v *Values) peek(ctx context.Context, d Variable) (string, string) {
	if _, ok := v.cache[d.Name]; ok {
		return RequirementSet, "given"
	}
	for _, src := range v.sources {
		if _, ok, err := src(ctx, d.Name); err == nil && ok {
			return RequirementSet, "given"
		}
	}
	if d.Env != "" {
		if val, ok := v.lookupEnv(d.Env); ok && val != "" {
			return RequirementSet, "from $" + d.Env
		}
	}
	switch {
	case d.Later:
		return RequirementLater, "an earlier command of the run produces it"
	case d.From != nil:
		return RequirementDerived, "from " + strings.Join(d.From.Args, " ") + ", when first needed"
	}
	return RequirementMissing, "no flag, source or environment variable gives it"
}

// inputState checks an input: its environment variable holds a value, and
// its check passes.
func (r *Runner) inputState(ctx context.Context, in *Input) (string, string) {
	var from []string
	if in.Env != "" {
		if val, ok := r.Values.lookupEnv(in.Env); !ok || val == "" {
			return RequirementMissing, "$" + in.Env + " is not set"
		}
		from = append(from, "$"+in.Env+" is set")
	}
	if in.Check != nil {
		checker := r.Checker
		if checker == nil {
			checker = &Checker{Values: r.Values}
		}
		get := func(s string) string {
			out, err := r.Values.Expand(ctx, s)
			if err != nil {
				return s
			}
			return out
		}
		if _, err := checker.command(ctx, in.Check, get); err != nil {
			return RequirementMissing, strings.Join(in.Check.Args, " ") + ": " + firstLine(strings.TrimSpace(err.Error()))
		}
		from = append(from, strings.Join(in.Check.Args, " ")+" passes")
	}
	return RequirementSet, strings.Join(from, "; ")
}

// MissingInputsError names the inputs a run needs that are not in place.
type MissingInputsError struct {
	// Inputs are the missing inputs.
	Inputs []Requirement
}

func (e *MissingInputsError) Error() string {
	names := make([]string, len(e.Inputs))
	for i, in := range e.Inputs {
		names[i] = in.Name
	}
	return "runbook: not in place: " + strings.Join(names, ", ") + "; see each one's how-to, or run prepare"
}

// missingInputs checks the inputs the steps need.
func (r *Runner) missingInputs(ctx context.Context, rb *Runbook, steps []*Step) *MissingInputsError {
	var missing []Requirement
	for _, req := range r.Requirements(ctx, rb, steps) {
		if req.Kind == "input" && req.State == RequirementMissing {
			missing = append(missing, req)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return &MissingInputsError{Inputs: missing}
}

// LoadEnvFile sets environment variables from a file of NAME=value lines,
// as a person keeps the values a runbook needs in one place: blank lines
// and lines starting with # are skipped, "export " is allowed, and a value
// may be quoted. A variable the environment already sets is left as it is.
// It returns the names it set, never the values.
func LoadEnvFile(path string) ([]string, error) {
	data, err := os.ReadFile(path) //nolint:gosec // G304: the file the person named
	if err != nil {
		return nil, fmt.Errorf("runbook: env file: %w", err)
	}
	var set []string
	sc := bufio.NewScanner(bytes.NewReader(data))
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		name, val, ok := strings.Cut(line, "=")
		name = strings.TrimSpace(name)
		if !ok || name == "" || strings.ContainsAny(name, " \t") {
			return set, fmt.Errorf("runbook: env file %s, line %d: want NAME=value", path, n)
		}
		val = strings.TrimSpace(val)
		if len(val) >= 2 && (val[0] == '"' || val[0] == '\'') && val[len(val)-1] == val[0] {
			val = val[1 : len(val)-1]
		}
		if val == "" {
			continue
		}
		if cur, ok := os.LookupEnv(name); ok && cur != "" {
			continue
		}
		if err := os.Setenv(name, val); err != nil {
			return set, fmt.Errorf("runbook: env file %s, line %d: %w", path, n, err)
		}
		set = append(set, name)
	}
	return set, sc.Err()
}

// EnvTemplate is an env file for LoadEnvFile with a line for each
// requirement set by an environment variable, never with a value: an
// export to fill in for each value, and a commented one for a secret,
// which is better set in the shell than kept in a file.
func EnvTemplate(title string, reqs []Requirement) []byte {
	var b bytes.Buffer
	fmt.Fprintf(&b, "# What %s needs. Fill in each value; keep this file out of version control.\n", title)
	seen := map[string]bool{}
	for _, r := range reqs {
		if r.Env == "" || seen[r.Env] {
			continue
		}
		seen[r.Env] = true
		fmt.Fprintf(&b, "\n# %s: %s", r.Name, r.Description)
		if r.Example != "" {
			fmt.Fprintf(&b, " (such as %s)", r.Example)
		}
		fmt.Fprintf(&b, "; steps %s\n", strings.Join(r.Steps, ", "))
		if r.Secret {
			fmt.Fprintf(&b, "# secret: export it in the shell instead, or fill this in only if the file stays private\n# export %s=\n", r.Env)
			continue
		}
		fmt.Fprintf(&b, "export %s=\n", r.Env)
	}
	return b.Bytes()
}

// validateInputs checks the inputs' declarations and the steps' needs.
func (rb *Runbook) validateInputs() []error {
	var errs []error
	names := map[string]bool{}
	for _, in := range rb.Inputs {
		where := "input " + in.Name
		switch {
		case in.Name == "":
			errs = append(errs, errors.New("input: name is required"))
		case names[in.Name]:
			errs = append(errs, fmt.Errorf("%s: declared twice", where))
		}
		names[in.Name] = true
		if in.Description == "" {
			errs = append(errs, fmt.Errorf("%s: description is required", where))
		}
		if in.Env == "" && in.Check == nil {
			errs = append(errs, fmt.Errorf("%s: needs an env or a check", where))
		}
		if in.Check != nil && (len(in.Check.Args) == 0 || in.Check.Args[0] == "") {
			errs = append(errs, fmt.Errorf("%s: check names no program", where))
		}
	}
	for _, s := range rb.Steps() {
		for _, n := range s.Needs {
			if !names[n] {
				errs = append(errs, fmt.Errorf("step %s: needs %s, which is not a declared input", s.ID, n))
			}
		}
	}
	return errs
}
