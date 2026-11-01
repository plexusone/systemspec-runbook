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

## Schemas

`schema.Runbook()` and `schema.Events()` return the embedded JSON Schemas.
After changing the types, regenerate them with `go generate ./schema`.
