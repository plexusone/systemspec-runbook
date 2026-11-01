# systemspec-runbook: Technical Requirements

Design for [PRD.md](PRD.md). Process is in [PLAN.md](PLAN.md); work items
are in [ROADMAP.md](ROADMAP.md).

## 1. Packages

| Package | Holds |
|---|---|
| `runbookspec` | The model, validation, the event log and derived state, rendering and link checks; the check runner and generic checks |
| `schema` | The JSON Schemas of a runbook and its log, generated from the types and embedded |
| `cli` | Cobra commands a host tool mounts over its own runbook: validate, list, show, record, verify, run, doc, schema |
| `cmd/runbookspec` | The same commands over a runbook kept as JSON |

`runbookspec` depends only on the standard library; `schema` adds
`invopop/jsonschema` and `oscompat`; `cli` adds Cobra.

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
  registered in Go, with string arguments). Parameters may hold `{{name}}`
  variables, filled at run time.
- **Exec:** what a guided run executes: a program and its arguments, a
  directory relative to the runbook's root, and environment variables;
  standard input from another command or from a random value. Never a
  shell, so a run behaves the same on every platform. The page shows the
  shell lines a person would type to do the same.
- **Variable:** a name, a description, an optional environment variable,
  an example, and an optional command whose output is its value. Every `{{name}}` in the runbook must be declared, and a
  variable never holds a secret.

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

## 5. Variables

Values fill variables from sources in order: `--var name=value` flags,
then providers the host registers, then each variable's declared
environment variable, then the variable's own command (`from`), such as
`pulumi stack output staticIp`. That command runs without a shell, in the
runbook's root or its `dir`, only when the value is first needed; its
trimmed standard output is the value. When it fails or prints nothing the
variable stays unfilled, the error names why, and the command runs again
the next time, so a value an earlier command of the run creates is found.
Validation refuses a command that reads standard input, references an
undeclared variable, or needs its own value through other variables'
commands. A command with an unfilled
variable never runs: the run names every missing value first. The page
shows a variable as `<name>`.

## 6. Checks

A `Checker` runs checks with a registry of named checks and the values.
Each returns `pass`, `fail` with what it saw, or `skip` with why: a check
whose variable is unfilled, whose program is missing, or whose named check
wraps `ErrSkip`, skips and says so; a skip is never a pass. Checks only
read. `VerifyEvent` decides what a verify records: `verified` when every
check passed, `failed` when a check fails on a step that was done, and
nothing otherwise.

## 7. Guided Runs

`Runner.Run` runs one step:

1. It refuses a blocked step unless forced (the `started` event says so),
   and asks before running a step that is already done again.
2. It fills every command's variables before anything runs or is
   recorded.
3. It records `started`, then shows each action. A command of the runner's
   owner, or of `both`, is shown as its shell line and run after a yes
   (or at once with `--yes`); another owner's action, or one with no
   command, waits for a yes that it is done.
4. A declined command or action stops the run; the step stays in
   progress. A failing command records `failed` with the command and
   stops.
5. After the last action it records `done`, runs the checks, and records
   what `VerifyEvent` decides.

Commands' standard input is the terminal, so a program's own questions
work; the runner reads its own answers one byte at a time so it never
takes input meant for a command. A random value is generated with
`crypto/rand` and piped in, never shown or passed as an argument.

## 8. Security

- No type holds a secret's value, and no check reads one.
- The log refuses notes matching key material, tokens and long random
  strings.
- Commands, in checks and in runs, run without a shell; a generated secret
  is piped into the program that stores it, never put in its arguments.
