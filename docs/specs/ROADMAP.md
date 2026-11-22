# systemspec-runbook: Roadmap

Items carry stable IDs; phase status is derived from its items.

## Phase 1: Model

- [x] **RMI-SYSTEMSPECRUNBOOK-001** The runbook model and its validation
- [x] **RMI-SYSTEMSPECRUNBOOK-002** The event log and each step's derived state
- [x] **RMI-SYSTEMSPECRUNBOOK-003** Markdown rendering with computed links, and link checks
- [x] **RMI-SYSTEMSPECRUNBOOK-004** JSON Schemas of a runbook and its log, generated from the types

## Phase 2: Checks

- [x] **RMI-SYSTEMSPECRUNBOOK-005** The check runner: named checks registered in Go, `${name}` variables, and `pass`, `fail` and `skip` results
- [x] **RMI-SYSTEMSPECRUNBOOK-006** Generic checks: `dns`, `http`, `unreachable` and `command`
- [x] **RMI-SYSTEMSPECRUNBOOK-007** Recording verify results: `verified` for steps that pass, `failed` for done steps that no longer do

## Phase 3: Guided Runs and Command Line

- [x] **RMI-SYSTEMSPECRUNBOOK-012** Variables: declared `{{name}}` references in text, commands and checks, filled from flags, the environment or providers the host registers, such as a stack's outputs; a step cannot run with one unfilled
- [x] **RMI-SYSTEMSPECRUNBOOK-013** Executable actions: a program and its arguments with a directory and environment, never through a shell; standard input from another command or from a random value generated in Go and never shown
- [x] **RMI-SYSTEMSPECRUNBOOK-014** Guided run of a step: refuse a blocked step, show each action with its variables filled, confirm and run commands, wait on console steps, stop at the first failure, record `started`, `failed` and `done`, then run the checks and record `verified`
- [x] **RMI-SYSTEMSPECRUNBOOK-008** Cobra commands a host tool mounts: list, show, record, verify, run, doc, schema
- [x] **RMI-SYSTEMSPECRUNBOOK-009** `runbookspec`, the command line over a runbook and log in JSON
- [x] **RMI-SYSTEMSPECRUNBOOK-016** HTML pages: the Markdown page as a styled HTML document, or its body for a host's page; `doc --html`
- [x] **RMI-SYSTEMSPECRUNBOOK-017** A host's events flag: the commands' event log flag renamed when the host has an events flag of its own
- [x] **RMI-SYSTEMSPECRUNBOOK-015** Variables from commands: a variable names the command whose output is its value, such as a stack's output, so a runbook kept as JSON runs with `runbookspec` and no host code; `--root` sets where commands run

## Phase 4: Release

- [ ] **RMI-SYSTEMSPECRUNBOOK-010** Documentation site: guides for declaring a runbook, events and states, checks, and rendering
- [x] **RMI-SYSTEMSPECRUNBOOK-011** The first release, v0.1.0, so modules that depend on it require a tag; what running a real launch from start to end shows goes into v0.1.1 or v0.2.0
