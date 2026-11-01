package runbookspec

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

var (
	phaseIDPattern = regexp.MustCompile(`^[0-9]+$`)
	stepIDPattern  = regexp.MustCompile(`^([0-9]+)\.([0-9]+)$`)
)

// ValidateOptions add checks that need knowledge from outside the runbook.
type ValidateOptions struct {
	// KnownCredential, when set, reports whether a credential ID exists.
	KnownCredential func(id string) bool
	// KnownNamedCheck, when set, reports whether a named check is
	// registered.
	KnownNamedCheck func(name string) bool
}

// Validate checks the runbook's structure: IDs, owners, dependencies,
// checks and links. Every problem is reported together.
func (rb *Runbook) Validate(opts ValidateOptions) error {
	var errs []error
	add := func(format string, args ...any) { errs = append(errs, fmt.Errorf(format, args...)) }

	if rb.ID == "" {
		add("runbook: id is required")
	}
	if rb.Title == "" {
		add("runbook: title is required")
	}
	if len(rb.Phases) == 0 {
		add("runbook: no phases")
	}

	seen := map[string]bool{}
	phases := map[string]bool{}
	for _, p := range rb.Phases {
		where := "phase " + p.ID
		if !phaseIDPattern.MatchString(p.ID) {
			add("%s: id must be a number", where)
		}
		if phases[p.ID] {
			add("%s: duplicate id", where)
		}
		phases[p.ID] = true
		if p.Title == "" {
			add("%s: title is required", where)
		}
		if p.Gives == "" {
			add("%s: gives is required", where)
		}
		if len(p.Steps) == 0 {
			add("%s: no steps", where)
		}
		for _, s := range p.Steps {
			errs = append(errs, s.validate(p.ID, seen, opts)...)
			seen[s.ID] = true
		}
	}
	for _, l := range rb.Later {
		if l.What == "" || l.Needs == "" {
			add("later: what and needs are required")
		}
	}
	errs = append(errs, rb.validateVariables()...)
	return errors.Join(errs...)
}

// validateVariables checks the declarations, and that every reference is
// declared.
func (rb *Runbook) validateVariables() []error {
	var errs []error
	declared := map[string]bool{}
	for _, v := range rb.Variables {
		switch {
		case !varName.MatchString(v.Name):
			errs = append(errs, fmt.Errorf("variable %q: a name is letters, digits, '.' and '_', starting with a letter", v.Name))
		case declared[v.Name]:
			errs = append(errs, fmt.Errorf("variable %s: declared twice", v.Name))
		}
		if v.Description == "" {
			errs = append(errs, fmt.Errorf("variable %s: description is required", v.Name))
		}
		declared[v.Name] = true
	}
	check := func(where string, texts []string) {
		var undeclared []string
		for _, t := range texts {
			for _, name := range References(t) {
				if !declared[name] && !slices.Contains(undeclared, name) {
					undeclared = append(undeclared, name)
				}
			}
		}
		for _, name := range undeclared {
			errs = append(errs, fmt.Errorf("%s: references {{%s}}, which is not declared", where, name))
		}
	}
	page := []string{rb.Summary, rb.Usage}
	for _, p := range rb.Phases {
		page = append(page, p.Summary, p.Gives)
	}
	for _, l := range rb.Later {
		page = append(page, l.Needs)
	}
	check("runbook", page)
	for _, s := range rb.Steps() {
		check("step "+s.ID, s.strings())
	}
	for _, v := range rb.Variables {
		if v.From == nil {
			continue
		}
		where := "variable " + v.Name
		if len(v.From.Args) == 0 || v.From.Args[0] == "" {
			errs = append(errs, fmt.Errorf("%s: from names no program", where))
		}
		if v.From.Stdin != nil {
			errs = append(errs, fmt.Errorf("%s: from reads no standard input", where))
		}
		check(where, v.From.strings())
	}
	if name := commandCycle(rb.Variables); name != "" {
		errs = append(errs, fmt.Errorf("variable %s: its command needs its own value, through the variables it references", name))
	}
	return errs
}

// commandCycle returns a variable whose command references, directly or
// through other variables' commands, the variable itself; "" when none.
func commandCycle(vars []Variable) string {
	needs := map[string][]string{}
	for _, v := range vars {
		if v.From == nil {
			continue
		}
		for _, t := range v.From.strings() {
			needs[v.Name] = append(needs[v.Name], References(t)...)
		}
	}
	const (
		visiting = 1
		done     = 2
	)
	state := map[string]int{}
	var visit func(string) bool
	visit = func(n string) bool {
		switch state[n] {
		case visiting:
			return true
		case done:
			return false
		}
		state[n] = visiting
		for _, m := range needs[n] {
			if visit(m) {
				return true
			}
		}
		state[n] = done
		return false
	}
	for _, v := range vars {
		if visit(v.Name) {
			return v.Name
		}
	}
	return ""
}

// validate checks a step; seen holds the IDs of the steps before it.
func (s *Step) validate(phaseID string, seen map[string]bool, opts ValidateOptions) []error {
	var errs []error
	where := "step " + s.ID
	add := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf(where+": "+format, args...))
	}

	m := stepIDPattern.FindStringSubmatch(s.ID)
	switch {
	case m == nil:
		add("id must be <phase>.<n>")
	case m[1] != phaseID:
		add("id is not in phase %s", phaseID)
	}
	if seen[s.ID] {
		add("duplicate id")
	}
	if s.Title == "" {
		add("title is required")
	}
	if !validOwner(s.Owner) {
		add("owner %q is not one of %v", s.Owner, Owners)
	}
	if s.Done == "" {
		add("done is required")
	}
	for _, d := range s.DependsOn {
		if !seen[d] {
			add("depends on %s, which is not an earlier step", d)
		}
	}
	for i, a := range s.Actions {
		if a.Text == "" && a.Command == "" && len(a.Exec) == 0 {
			add("action %d has no text, command or exec", i+1)
		}
		if a.Owner != "" && !validOwner(a.Owner) {
			add("action %d: owner %q is not one of %v", i+1, a.Owner, Owners)
		}
		if a.Link != nil && (a.Link.Text == "" || (a.Link.Key == "") == (a.Link.Href == "")) {
			add("action %d: a link needs text and exactly one of key and href", i+1)
		}
		for j, e := range a.Exec {
			if len(e.Args) == 0 || e.Args[0] == "" {
				add("action %d, command %d: needs a program", i+1, j+1)
			}
			if in := e.Stdin; in != nil {
				switch {
				case (len(in.From) > 0) == (in.RandomHex > 0):
					add("action %d, command %d: stdin needs exactly one of from and randomHex", i+1, j+1)
				case in.RandomHex > 0 && (in.RandomHex < 16 || in.RandomHex > 1024):
					add("action %d, command %d: randomHex is 16 to 1024 bytes", i+1, j+1)
				}
			}
		}
	}
	checks := map[string]bool{}
	for _, c := range s.Checks {
		if c.ID == "" {
			add("a check has no id")
		} else if checks[c.ID] {
			add("check %s: duplicate id", c.ID)
		}
		checks[c.ID] = true
		for _, e := range c.validate(opts) {
			add("check %s: %v", c.ID, e)
		}
	}
	for _, id := range s.Credentials {
		if opts.KnownCredential != nil && !opts.KnownCredential(id) {
			add("credential %s is not in the registry", id)
		}
	}
	return errs
}

// validate checks that the kind's parameters, and only those, are set.
func (c *Check) validate(opts ValidateOptions) []error {
	var errs []error
	add := func(format string, args ...any) { errs = append(errs, fmt.Errorf(format, args...)) }
	if c.Description == "" {
		add("description is required")
	}
	set := map[CheckKind]bool{
		KindDNS:         c.DNS != nil,
		KindHTTP:        c.HTTP != nil,
		KindUnreachable: c.Unreachable != nil,
		KindCommand:     c.Command != nil,
		KindNamed:       c.Named != nil,
	}
	if !slices.Contains(CheckKinds, c.Kind) {
		add("kind %q is not one of %v", c.Kind, CheckKinds)
		return errs
	}
	for k, isSet := range set {
		switch {
		case k == c.Kind && !isSet:
			add("kind %s needs its %s parameters", c.Kind, c.Kind)
		case k != c.Kind && isSet:
			add("kind %s must not set %s parameters", c.Kind, k)
		}
	}
	switch c.Kind {
	case KindDNS:
		if d := c.DNS; d != nil {
			if d.Name == "" || len(d.Contains) == 0 {
				add("dns needs a name and what the answers contain")
			}
			if !slices.Contains([]string{"A", "AAAA", "CNAME", "MX", "NS", "TXT"}, strings.ToUpper(d.Type)) {
				add("dns type %q is not supported", d.Type)
			}
		}
	case KindHTTP:
		if h := c.HTTP; h != nil && (h.URL == "" || h.Status < 100 || h.Status > 599) {
			add("http needs a url and a status")
		}
	case KindUnreachable:
		if u := c.Unreachable; u != nil && u.Address == "" {
			add("unreachable needs an address")
		}
	case KindCommand:
		if cm := c.Command; cm != nil && len(cm.Args) == 0 {
			add("command needs its arguments")
		}
	case KindNamed:
		if n := c.Named; n != nil {
			if n.Name == "" {
				add("named needs a name")
			} else if opts.KnownNamedCheck != nil && !opts.KnownNamedCheck(n.Name) {
				add("named check %s is not registered", n.Name)
			}
		}
	}
	return errs
}

func validOwner(o Owner) bool { return slices.Contains(Owners, o) }

// Unchecked returns the IDs of the steps that have no checks.
func (rb *Runbook) Unchecked() []string {
	var out []string
	for _, s := range rb.Steps() {
		if len(s.Checks) == 0 {
			out = append(out, s.ID)
		}
	}
	return out
}
