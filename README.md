# systemspec-runbook

[![Go CI][go-ci-svg]][go-ci-url]
[![Go Lint][go-lint-svg]][go-lint-url]
[![Docs][docs-godoc-svg]][docs-godoc-url]
[![License][license-svg]][license-url]

 [go-ci-svg]: https://github.com/plexusone/systemspec-runbook/actions/workflows/go-ci.yaml/badge.svg?branch=main
 [go-ci-url]: https://github.com/plexusone/systemspec-runbook/actions/workflows/go-ci.yaml
 [go-lint-svg]: https://github.com/plexusone/systemspec-runbook/actions/workflows/go-lint.yaml/badge.svg?branch=main
 [go-lint-url]: https://github.com/plexusone/systemspec-runbook/actions/workflows/go-lint.yaml
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
- **Variables.** `{{name}}` in text, commands and checks, declared and
  filled from flags, the host's providers, the environment, or a command
  the variable declares, such as `pulumi stack output staticIp`, so a
  runbook kept as JSON needs no Go to read a stack's outputs.
- **A generated page.** Markdown with each step's owner, actions, commands,
  credentials, when it is done and its checks. Text links to a step with
  `[[step:1.2]]` and to a resolved target with `[[key|text]]`, so no anchor
  or generated section number is typed by hand.
- **Link checks.** `BrokenLinks` reports relative links whose file or
  heading does not exist, with headings named as MkDocs names them.
- **An event log** of what happened to each step, by whom and when. It
  refuses notes that look like secrets.
- **JSON Schemas** of a runbook and its log, generated from the Go types.

## Command Line

```bash
go install github.com/plexusone/systemspec-runbook/cmd/runbookspec@latest
runbookspec list   --runbook runbook.json --events events.json
runbookspec run 1.2 --runbook runbook.json --var profile=ops
runbookspec verify --all --record --runbook runbook.json --root .
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
| `cli` | The Cobra commands a host tool mounts: validate, list, show, record, verify, run, doc, schema |
| `cmd/runbookspec` | The same commands over a runbook kept as JSON |

## Documentation

The [getting-started guide](docs/guides/getting-started.md) walks through
declaring, rendering, recording, checking and running a runbook; the
[specs](docs/specs/PRD.md) describe the design and the
[roadmap](docs/specs/ROADMAP.md) what comes next.

## License

MIT
