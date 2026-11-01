# systemspec-runbook: Technical Requirements

Design for [PRD.md](PRD.md). Process is in [PLAN.md](PLAN.md); work items
are in [ROADMAP.md](ROADMAP.md).

## 1. Packages

| Package | Holds |
|---|---|
| `runbookspec` | The model, validation, the event log and derived state, rendering and link checks; the check runner and generic checks |
| `schema` | The JSON Schemas of a runbook and its log, generated from the types and embedded |
| `cli` (planned) | Cobra commands a host tool mounts over its own runbook |
| `cmd/runbookspec` (planned) | The command line over JSON files |

`runbookspec` depends only on the standard library; `schema` adds
`invopop/jsonschema` and `oscompat`.

## 2. The Model

- **Runbook:** an ID, a title, a summary, how to use it, phases, and the
  work that follows it.
- **Phase:** a number, a title, what finishing it gives, and its steps.
- **Step:** an ID `<phase>.<n>`, a title, an owner (`operator`, `agent` or
  `both`), the steps it depends on, actions, the sentence that says when it
  is done, the IDs of the credentials it creates, whether it is a gate, and
  its checks.
- **Action:** Markdown text, an optional shell command shown as a code
  block, an optional owner, and an optional link resolved at render time.
- **Check:** an ID, a description, and a kind with exactly the matching
  parameters: `dns` (name, type, substrings the answers contain), `http`
  (URL, status, body or redirect location), `unreachable` (address and
  timeout), `command` (arguments, run without a shell), or `named` (a check
  registered in Go, with string arguments). Parameters may hold `${name}`
  variables, filled at run time.

Validation checks unique IDs, owners, that dependencies exist and come
earlier, that each check has exactly its kind's parameters, and, through
options, that credential IDs and named checks are known.

## 3. Events and State

An event is a step, a type (`started`, `done`, `verified`, `failed`,
`skipped`, `reopened`), a time, who, and a note that must not look like a
secret. A step's state is derived by replaying its events: `started` and
`failed` leave it `in-progress`, `done` and `skipped` leave it `done`,
`verified` leaves it `verified`, and `reopened` clears it. A step with no
state is `blocked` while a dependency is not done or verified, and `ready`
otherwise.

## 4. Rendering

`Markdown` writes the page: the summary, how to use it, a table of phases,
each step with its owner, dependencies, actions, credentials, when it is
done and its checks, and the work that follows. In text, `[[step:<id>]]`
links to a step and `[[<key>|text]]` to a target the caller's resolver
returns. `Slug` names headings as MkDocs does, and `BrokenLinks` reports
relative links whose file or heading does not exist.

## 5. Checks (planned)

A runner takes a registry of named checks and a source of variables. Each
check returns `pass`, `fail` with what it saw, or `skip` with why: a check
that needs credentials it does not have skips and says so; `skip` is never
reported as `pass`. Checks only read. A verify run can append `verified` for
each step whose checks pass, and `failed` for a done step that no longer
passes.

## 6. Security

- No type holds a secret's value, and no check reads one.
- The log refuses notes matching key material, tokens and long random
  strings.
- Commands in checks run without a shell.
