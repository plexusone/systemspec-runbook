# systemspec-runbook

A runbook as data: phases of steps, each with an owner, its dependencies,
the actions that do it, the sentence that says when it is done, and typed
checks that prove it.

A Go program declares the runbook from its own facts, so hosts, ports and
counts are read, never typed twice. systemspec-runbook then:

- **validates** it, reporting every problem together;
- **renders** it as a Markdown page whose links are computed;
- **records** what was done in an event log, and **derives** where each
  step stands: `blocked`, `ready`, `in-progress`, `done` or `verified`;
- **describes** both as JSON Schemas generated from the Go types.

Start with [Getting Started](guides/getting-started.md). The
[design documents](specs/PRD.md) explain the model and what comes next.
