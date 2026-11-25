# Getting Started

## Install

```bash
go get github.com/plexusone/systemspec-runbook
```

## Declare a Runbook

A runbook has phases, and a phase has steps. Each step has:

| Field | Holds |
|---|---|
| `id` | `<phase>.<n>`, such as `1.2` |
| `owner` | `operator`, `agent` or `both`: the agent prepares, the operator runs, the agent checks |
| `dependsOn` | Steps that must be done first; nothing is implied by order |
| `actions` | What to do: Markdown text, an optional command, an optional owner and link |
| `done` | The sentence that says when the step is done |
| `checks` | Typed checks that prove it |
| `credentials` | IDs of the credentials the step creates |
| `gate` | Whether the step is a decision or condition rather than work |

The package example, `runbookspec/example_test.go`, declares a two-step
runbook, validates it, records an event, derives each step's state and
renders the page.

## Link Without Typing Anchors

In any Markdown text:

- `[[step:1.2]]` links to step 1.2, and `[[step:1.2|the cut-over]]` with
  your own text.
- `[[key|text]]` links to a target your resolver returns, such as a
  section of another generated page:

```go
page, err := rb.Markdown(runbookspec.RenderOptions{
	Resolve: func(key string) (string, error) {
		return sections[key], nil // e.g. "plan.md#3-payments"
	},
	VerifyCommand: "bin/ops verify {step}",
	Generator:     "make runbook",
})
```

`BrokenLinks(path, page)` then reports any relative link whose file or
heading does not exist, so a test can keep the page honest.

## Record Progress

```go
log, err := runbookspec.LoadLog("events.json", rb.ID) // missing is empty
err = log.Append(rb, runbookspec.Event{Step: "1.1", Type: runbookspec.EventDone, At: time.Now(), By: runbookspec.OwnerOperator})
data, err := log.Marshal() // commit it with the work
```

| Event | Leaves the step |
|---|---|
| `started`, `failed` | `in-progress` |
| `done`, `skipped` | `done` |
| `verified` | `verified`: its checks passed |
| `reopened` | as if it had no events |

A step with no events is `blocked` until its dependencies are done or
verified, then `ready`. A note that looks like a secret is refused.

## Recurring Runbooks and Their State

A launch is run once; a deploy is run every time. Mark a runbook that is
run again `recurring`, and begin each run with `restart`:

```go
rb.Recurring = true
err = log.Restart(rb, runbookspec.RunStart{At: log.NextRunStart(time.Now()), By: runbookspec.OwnerOperator, Note: "v1.2.0"})
```

The log keeps when each run began, and every event. A step's state comes
from the current run's events, so after a restart every step is `ready`
or `blocked` again, and the earlier runs stay as history. A runbook that
is not recurring refuses a restart; reopen a step instead.

`StateOf` returns the runbook's state at a glance:

| Field | What it says |
|---|---|
| `current` | The run whose events give the steps' states |
| `runs` | Every run, oldest first: when it started and by whom, its last event, when it finished, its outcome and its steps by state |
| `lastFinished` | When the latest finished run finished |

A run's outcome is `not-started`, `in-progress`, `failed` when its last
event is a failure, or `finished` once every step is done or verified.
`state` prints it, as text or with `--format json`.

## Variables

Declare each value known only at run time, and reference it as
`{{name}}` in text, commands and checks:

```go
rb.Variables = []runbookspec.Variable{
	{Name: "profile", Description: "the AWS profile", Env: "AWS_PROFILE"},
	{Name: "staticIp", Description: "the instance's address"},
}
```

Values come from `--var name=value`, then from sources the host registers,
then from the declared environment variable, then from the variable's own
command. The page shows each as `<name>`.

A variable can name the command that gives it, so a runbook kept as JSON
reads a value with no Go code:

```go
{Name: "staticIp", Description: "the instance's address", Later: true,
	From: &runbookspec.Exec{
		Args: []string{"pulumi", "stack", "output", "staticIp", "--stack", "prod"},
		Dir:  "stack",
		Env:  map[string]string{"AWS_PROFILE": "{{profile}}"},
	}},
```

The command runs only when the value is first needed, without a shell, in
`--root` or its `dir`; its trimmed output is the value. If it fails or
prints nothing, the variable stays unfilled, the error says why, and the
command runs again the next time. `Later` lets a run start before the
stack exists.

## Inputs

An input is something steps need that is not a `{{name}}`: a value in the
environment that commands read themselves, such as an API token, or a
precondition a command checks, such as a key loaded in the SSH agent.
Declare each with how to set it up, and name it in the steps that need it:

```go
rb.Inputs = []runbookspec.Input{
	{Name: "CLOUDFLARE_API_TOKEN", Description: "a token for the zones",
		Env: "CLOUDFLARE_API_TOKEN", Secret: true,
		HowTo: "At [API Tokens](https://dash.cloudflare.com/profile/api-tokens): Zone · DNS · Edit, TTL ending tomorrow."},
	{Name: "key-in-agent", Description: "the instance's key, loaded in the SSH agent",
		Check: &runbookspec.CommandCheck{Args: []string{"ssh-add", "-l"}, Contains: "SHA256:…"},
		HowTo: "`ssh-add ~/.ssh/fleet`"},
}
step.Needs = []string{"CLOUDFLARE_API_TOKEN", "key-in-agent"}
```

An input is in place when its environment variable holds a value and its
check passes. A secret's value is only checked for, never shown or
written. Both pages open with Before You Start: each variable and input,
how to set it up, and the steps that use it; a step links the inputs it
needs.

## Prepare, Then Run a Phase

```bash
runbookspec prepare 2 --runbook runbook.json --write-env .local/phase-2.env
# fill in .local/phase-2.env; export any secret in the shell
runbookspec prepare 2 --runbook runbook.json --env-file .local/phase-2.env
runbookspec run 2     --runbook runbook.json --env-file .local/phase-2.env
```

`prepare <phase|step|all>` lists what the remaining steps need, each
variable and input `set`, `missing`, `derived` (its command gives it when
first needed) or `later` (an earlier command of the run produces it), then
how to set up each one missing. It only reads, never shows a value, and
fails while anything is missing. `--write-env` writes an `export NAME=`
line for each environment variable, with no values and a secret's line
commented, and never overwrites a file. `--env-file` loads such a file
first, as `source` would, expanding `$NAME`, without replacing what the
environment already sets.

`run <phase>` checks every variable and input before the first step, and
refuses to start with the full list if any is missing. It runs the steps
in order, skipping those already done or verified, and stops at the first
that fails, is stopped, or waits for a step outside the phase; run it again
to resume. A step whose checks fail after its actions are done fails too:
`run` exits non-zero and says which checks failed. Fix what they found,
then `verify <step> --record` records it verified.

## Troubleshooting

A failed run says where it stopped and what to do:

```text
Phase 1 (Provision) stopped at step 1.2 "Up", action 1, command 1:
    pulumi up
Error: exit status 255
    ssh: connect to host 192.0.2.1 port 22: Operation timed out
Fix:
  - Add your address to the admin CIDRs.
Resume the phase: runbookspec run 1
Retry the step:   runbookspec run 1.2
```

The fixes come from the runbook's and the step's `Troubleshoot`: each is
shown when its pattern, a Go regular expression, matches the failed
command's output or error, the step's before the runbook's:

```go
rb.Troubleshoot = []runbookspec.Fix{
	{Match: `port 22: Operation timed out`, Fix: "Add your address to the admin CIDRs."},
}
```

The pages list them too: the runbook's in a Troubleshooting section, and a
step's after its checks.

## Commands a Run Executes

`Command` is shown for a person to adapt; `Exec` is what `run` executes:
a program and its arguments, never through a shell.

```go
Action{
	Owner: runbookspec.OwnerOperator,
	Text:  "Create the stack and set the master password.",
	Exec: []runbookspec.Exec{
		{Args: []string{"pulumi", "stack", "init", "prod"}, Dir: "stack",
			Env: map[string]string{"AWS_PROFILE": "{{profile}}"}},
		{Args: []string{"pulumi", "config", "set", "--secret", "db:password"}, Dir: "stack",
			Env:   map[string]string{"AWS_PROFILE": "{{profile}}"},
			Stdin: &runbookspec.Stdin{RandomHex: 32}},
	},
}
```

`Stdin.RandomHex` generates the password in Go and pipes it in, so it is
never shown and never in a process's arguments; `Stdin.From` pipes another
command's output in, as `a | b` would. The page shows the shell lines a
person would type: `cd stack`, `export AWS_PROFILE=<profile>`,
`openssl rand -hex 32 | pulumi config set --secret db:password`.

## Checks

```go
checker := &runbookspec.Checker{
	Values: values,
	Named: map[string]runbookspec.NamedFunc{
		"aws.bucket": func(ctx context.Context, args map[string]string) (string, error) {
			// return nil when so, an error wrapping ErrSkip when you cannot tell
		},
	},
}
results := checker.Step(ctx, rb.Step("1.1"))
```

`dns`, `http`, `unreachable` and `command` checks need no registration.
`VerifyEvent` turns results into `verified`, or `failed` for a done step
that no longer passes.

## Run a Step

```bash
runbookspec run 1.2 --runbook runbook.json --var profile=ops
```

A step's run checks its own inputs first.

The run refuses a blocked step, fills every variable before anything runs,
asks before each command (`--yes` to skip asking), waits for a person's
steps, stops at the first failure, records `started`, `failed` and `done`,
and then runs the checks and records `verified`. Another owner's action is
confirmed as done, never run.

## Schemas

`schema.Runbook()` and `schema.Events()` return the embedded JSON Schemas.
After changing the types, regenerate them with `go generate ./schema`.
