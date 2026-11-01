// Package runbook declares a runbook as data: phases of steps, each with an
// owner, its dependencies, the actions that do it, the sentence that says
// when it is done, and the checks that prove it. A runbook is built in Go,
// validated, rendered to Markdown, and its progress is an event log from
// which each step's state is derived.
//
// The package holds no facts about any deployment and imports nothing from
// the repository that uses it.
package runbookspec

// Owner is who does a step or an action.
type Owner string

// The owners.
const (
	// OwnerOperator is the person who holds the accounts: anything that
	// signs in to a console, handles a secret, changes a security setting
	// or cannot be undone.
	OwnerOperator Owner = "operator"
	// OwnerAgent is the coding agent: it prepares files, checks results,
	// records events and commits, and never handles a secret's value.
	OwnerAgent Owner = "agent"
	// OwnerBoth means the agent prepares, the operator runs, and the agent
	// checks.
	OwnerBoth Owner = "both"
)

// Owners are the valid owners, in the order they are described.
var Owners = []Owner{OwnerOperator, OwnerAgent, OwnerBoth}

// Runbook is an ordered procedure: phases of steps.
type Runbook struct {
	// ID names the runbook, such as "launch".
	ID string `json:"id"`
	// Title is the page's title.
	Title string `json:"title"`
	// Summary introduces the runbook, in Markdown.
	Summary string `json:"summary"`
	// Usage explains how to use the runbook, in Markdown.
	Usage string `json:"usage,omitempty"`
	// Variables are the names {{name}} may reference in text, commands
	// and checks. Every reference must be declared.
	Variables []Variable `json:"variables,omitempty"`
	// Phases are done in order.
	Phases []Phase `json:"phases"`
	// Later is work that follows the runbook, listed at the end.
	Later []Later `json:"later,omitempty"`
}

// Variable is a value known only when a step runs, such as the profile to
// use or an address a stack created. A variable never holds a secret.
type Variable struct {
	// Name is referenced as {{name}}: letters, digits, '.' and '_'.
	Name string `json:"name"`
	// Description says what the value is.
	Description string `json:"description"`
	// Env, when set, is the environment variable that supplies the value
	// when no flag or provider does.
	Env string `json:"env,omitempty"`
	// Example is shown on the rendered page, such as "us-west-2".
	Example string `json:"example,omitempty"`
	// Later marks a value an earlier command of the same run produces,
	// such as a stack's output after it is brought up. A run does not
	// require it before it starts, only when the command that uses it is
	// about to run.
	Later bool `json:"later,omitempty"`
	// From, when set, is a command whose standard output, trimmed, is the
	// value, such as a stack's output. It runs only when no flag, provider
	// or environment variable gives one, and only when the value is first
	// needed. A command that fails or prints nothing leaves the variable
	// unfilled, and runs again the next time it is needed. Its arguments,
	// directory and environment may reference other variables; it reads no
	// standard input.
	From *Exec `json:"from,omitempty"`
}

// Phase groups steps toward one result.
type Phase struct {
	// ID is the phase's number, such as "0".
	ID string `json:"id"`
	// Title names the phase, such as "Prepare".
	Title string `json:"title"`
	// Gives is what finishing the phase gives, for the summary table.
	Gives string `json:"gives"`
	// Summary introduces the phase, in Markdown.
	Summary string `json:"summary,omitempty"`
	// Steps are done in order, unless their dependencies say otherwise.
	Steps []Step `json:"steps"`
}

// Step is one unit of work, done by its owner and proven by its checks.
type Step struct {
	// ID is "<phase>.<n>", such as "1.1"; unique in the runbook.
	ID string `json:"id"`
	// Title names the step.
	Title string `json:"title"`
	// Owner does the step; an action may name another owner.
	Owner Owner `json:"owner"`
	// Summary introduces the step, in Markdown.
	Summary string `json:"summary,omitempty"`
	// DependsOn are the IDs of steps that must be done first. A step
	// depends on nothing implicitly, not even the step before it.
	DependsOn []string `json:"dependsOn,omitempty"`
	// Actions do the step, in order.
	Actions []Action `json:"actions,omitempty"`
	// Done is the sentence that says when the step is done, which its
	// checks implement as far as they can.
	Done string `json:"done"`
	// Checks prove the step is done. They only read.
	Checks []Check `json:"checks,omitempty"`
	// Credentials are the IDs of the credentials the step creates.
	Credentials []string `json:"credentials,omitempty"`
	// Gate marks a decision or condition rather than work.
	Gate bool `json:"gate,omitempty"`
	// Notes follow the actions, in Markdown.
	Notes []string `json:"notes,omitempty"`
}

// Action is one thing to do: a command to run, or an instruction.
type Action struct {
	// Owner does the action, when it is not the step's owner.
	Owner Owner `json:"owner,omitempty"`
	// Text says what to do, in Markdown.
	Text string `json:"text"`
	// Command is shown as a code block for a person to adapt and run; it
	// is never run by the runbook. Use Exec for what a run executes.
	Command string `json:"command,omitempty"`
	// Exec are the commands a guided run executes, in order, and the page
	// shows as a code block.
	Exec []Exec `json:"exec,omitempty"`
	// Link points into another page, resolved when the runbook is
	// rendered, and is shown after the text.
	Link *Link `json:"link,omitempty"`
}

// Exec is one command: a program and its arguments, never run through a
// shell, so it behaves the same on every platform. Its strings may
// reference variables.
type Exec struct {
	// Args are the program and its arguments.
	Args []string `json:"args"`
	// Dir is the working directory, relative to the runbook's root.
	Dir string `json:"dir,omitempty"`
	// Env are environment variables added for the command.
	Env map[string]string `json:"env,omitempty"`
	// Stdin, when set, feeds the command's standard input.
	Stdin *Stdin `json:"stdin,omitempty"`
}

// Stdin is what a command reads: another command's output, or a random
// value generated for it. Exactly one is set.
type Stdin struct {
	// From is a command, a program and its arguments, whose output is
	// piped in, as "a | b" would in a shell.
	From []string `json:"from,omitempty"`
	// RandomHex, when above zero, pipes in that many random bytes as
	// hexadecimal, generated in Go and never shown or logged: a new secret
	// that goes straight to the program that stores it.
	RandomHex int `json:"randomHex,omitempty"`
}

// Link is a link resolved when the runbook is rendered, so a section
// renumbered in a generated page cannot break it.
type Link struct {
	// Text is the link's text.
	Text string `json:"text"`
	// Key names the target for the renderer's resolver, such as
	// "plan:payments"; Href is used when it is empty.
	Key string `json:"key,omitempty"`
	// Href is a fixed target, relative to the rendered page.
	Href string `json:"href,omitempty"`
}

// Later is work after the runbook, for the closing table.
type Later struct {
	// What names the work.
	What string `json:"what"`
	// Needs says what it takes, in Markdown.
	Needs string `json:"needs"`
}

// Steps returns every step in order.
func (rb *Runbook) Steps() []*Step {
	var out []*Step
	for i := range rb.Phases {
		for j := range rb.Phases[i].Steps {
			out = append(out, &rb.Phases[i].Steps[j])
		}
	}
	return out
}

// Step returns the step with the ID, or nil.
func (rb *Runbook) Step(id string) *Step {
	for _, s := range rb.Steps() {
		if s.ID == id {
			return s
		}
	}
	return nil
}
