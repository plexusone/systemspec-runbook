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
  registered in Go by name, for what needs an SDK. Checks only read.
- **A generated page.** Markdown with each step's owner, actions, commands,
  credentials, when it is done and its checks. Text links to a step with
  `[[step:1.2]]` and to a resolved target with `[[key|text]]`, so no anchor
  or generated section number is typed by hand.
- **Link checks.** `BrokenLinks` reports relative links whose file or
  heading does not exist, with headings named as MkDocs names them.
- **An event log** of what happened to each step, by whom and when. It
  refuses notes that look like secrets.
- **JSON Schemas** of a runbook and its log, generated from the Go types.

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

## Documentation

The [specs](docs/specs/PRD.md) describe the design and the
[roadmap](docs/specs/ROADMAP.md) what comes next: the check runner, the
command line, and Cobra commands a host tool mounts.

## License

MIT
