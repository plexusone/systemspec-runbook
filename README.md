# systemspec-runbook

[![Go CI][go-ci-svg]][go-ci-url]
[![Go Lint][go-lint-svg]][go-lint-url]
[![Go SAST][go-sast-svg]][go-sast-url]
[![Docs][docs-godoc-svg]][docs-godoc-url]
[![License][license-svg]][license-url]

 [go-ci-svg]: https://github.com/plexusone/systemspec-runbook/actions/workflows/go-ci.yaml/badge.svg?branch=main
 [go-ci-url]: https://github.com/plexusone/systemspec-runbook/actions/workflows/go-ci.yaml
 [go-lint-svg]: https://github.com/plexusone/systemspec-runbook/actions/workflows/go-lint.yaml/badge.svg?branch=main
 [go-lint-url]: https://github.com/plexusone/systemspec-runbook/actions/workflows/go-lint.yaml
 [go-sast-svg]: https://github.com/plexusone/systemspec-runbook/actions/workflows/go-sast-codeql.yaml/badge.svg?branch=main
 [go-sast-url]: https://github.com/plexusone/systemspec-runbook/actions/workflows/go-sast-codeql.yaml
 [docs-godoc-svg]: https://pkg.go.dev/badge/github.com/plexusone/systemspec-runbook
 [docs-godoc-url]: https://pkg.go.dev/github.com/plexusone/systemspec-runbook
 [license-svg]: https://img.shields.io/badge/license-MIT-blue.svg
 [license-url]: https://github.com/plexusone/systemspec-runbook/blob/main/LICENSE

A runbook as data: phases of steps, each with an owner, its dependencies,
the actions that do it, the sentence that says when it is done, and typed
checks that prove it. A Go program declares the runbook from its own facts;
systemspec-runbook validates it, renders it as a Markdown page, records what
was done in an event log, and derives where each step stands.

## Features

- **Owners.** Each step and action is the operator's, an agent's, or both:
  the agent prepares, the operator runs, the agent checks.
- **Dependencies and state.** A step is `blocked`, `ready`, `in-progress`,
  `done` or `verified`, derived from its dependencies and the event log.
- **Typed checks.** `dns`, `http`, `unreachable`, `command`, or a check
  registered in Go by name, for what needs an SDK. Checks only read, and
  report `pass`, `fail` with what they saw, or `skip` with why.
- **Guided runs.** `run <step>` shows each action with its variables
  filled, asks before each command and runs it without a shell, waits for
  a person's steps, stops at the first failure, records the step, then
  runs its checks. A new secret is generated in Go and piped straight into
  the program that stores it.
- **Phase runs.** `run <phase>` runs a phase's steps in order, skipping
  those already done, and resumes where it stopped. A failure says where:
  the phase, step, action and command, the end of its output, the fixes
  whose patterns match, and the commands that resume the phase or retry the
  step.
- **Inputs and preparing.** Besides variables, a runbook declares inputs:
  a value in the environment, such as an API token, marked secret so it is
  never shown, or a precondition a command checks, such as a key in the
  SSH agent, each with how to set it up. Steps name the inputs they need.
  `prepare <phase|step|all>` lists every variable and input a run needs,
  which are in place and which are missing, and a run checks them all
  before its first step. `--env-file` loads a file of `NAME=value` lines,
  and `prepare --write-env` writes one to fill in, never with a value.
- **Troubleshooting.** A runbook and its steps list fixes: a pattern a
  failure's output matches, and what to do.
- **Variables.** `{{name}}` in text, commands and checks, declared and
  filled from flags, the host's providers, the environment, or a command
  the variable declares, such as `pulumi stack output staticIp`, so a
  runbook kept as JSON needs no Go to read a stack's outputs.
- **A generated page.** Markdown or HTML with each step's owner, actions,
  commands, credentials, when it is done and its checks. The HTML is built
  from the runbook with [templ](https://templ.guide) components: a
  full-width page styled for light and dark themes, each step a section
  with its owner's badge, a copy button on every command and a new-tab
  icon after every link to another page, or its body alone for a host's
  own page. Both open with Before You Start, every variable and input
  with how to set it up and the steps that use it, and list the
  troubleshooting fixes. Text
  fields are Markdown, converted without passing raw HTML through, and
  step anchors match the Markdown page's. Text links to a step with
  `[[step:1.2]]` and to a resolved target with `[[key|text]]`, so no anchor
  or generated section number is typed by hand.
- **Link checks.** `BrokenLinks` reports relative links whose file or
  heading does not exist, with headings named as MkDocs names them.
- **An event log** of what happened to each step, by whom and when. It
  refuses notes that look like secrets.
- **Recurring runbooks and their state.** A runbook marked `recurring`,
  such as a deploy, is run again from its first step: `restart` begins a
  new run and keeps the earlier ones as history. `state` reports each
  run's start, last event, finish and outcome, and when the runbook last
  finished.
- **JSON Schemas** of a runbook and its log, generated from the Go types.

## Command Line

```bash
go install github.com/plexusone/systemspec-runbook/cmd/runbookspec@latest
runbookspec list   --runbook runbook.json --events events.json
runbookspec prepare 2 --runbook runbook.json --env-file ops.env
runbookspec run 2   --runbook runbook.json --env-file ops.env   # the whole phase
runbookspec run 1.2 --runbook runbook.json --var profile=ops
runbookspec verify --all --record --runbook runbook.json --root .
runbookspec restart --note v1.2.0 --runbook deploy.json --events deploy.events.json
runbookspec state --format json --runbook deploy.json --events deploy.events.json
```

`--root` is the directory commands run in, and their `dir` is relative
to; it defaults to the current directory.

A host tool mounts the same commands over its own runbook with
`cli.New(cli.Options{Load: …, EventsPath: …, Named: …, Sources: …})`.

## Use from Go

```go
rb := &runbookspec.Runbook{ID: "launch", Title: "Launch", Phases: phases}
if err := rb.Validate(runbookspec.ValidateOptions{}); err != nil {
	return err
}
page, err := rb.Markdown(runbookspec.RenderOptions{Resolve: resolve})

log, err := runbookspec.LoadLog("events.json", rb.ID)
err = log.Append(rb, runbookspec.Event{Step: "1.1", Type: runbookspec.EventDone, At: time.Now()})
for _, s := range runbookspec.Statuses(rb, log) {
	fmt.Println(s.Step, s.State, s.Blockers)
}
```

The package example in `runbookspec/example_test.go` is a complete program.

## Packages

| Package | Holds |
|---|---|
| `runbookspec` | The model, validation, the event log and derived state, rendering and link checks |
| `schema` | The JSON Schemas of a runbook and its log, embedded; `go generate ./schema` rewrites them |
| `cli` | The Cobra commands a host tool mounts: validate, list, show, record, verify, prepare, run, restart, state, doc (`--html` for HTML), schema; a host can rename the events flag |
| `cmd/runbookspec` | The same commands over a runbook kept as JSON |

## Documentation

The [getting-started guide](docs/guides/getting-started.md) walks through
declaring, rendering, recording, checking and running a runbook; the
[specs](docs/specs/PRD.md) describe the design and the
[roadmap](docs/specs/ROADMAP.md) what comes next. Each release has
[notes](docs/releases/v0.2.0.md), and the [changelog](CHANGELOG.md) lists
them all.

## License

MIT
