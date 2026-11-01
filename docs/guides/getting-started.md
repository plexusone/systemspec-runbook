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

The run refuses a blocked step, fills every variable before anything runs,
asks before each command (`--yes` to skip asking), waits for a person's
steps, stops at the first failure, records `started`, `failed` and `done`,
and then runs the checks and records `verified`. Another owner's action is
confirmed as done, never run.

## Schemas

`schema.Runbook()` and `schema.Events()` return the embedded JSON Schemas.
After changing the types, regenerate them with `go generate ./schema`.
