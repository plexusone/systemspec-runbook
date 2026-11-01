# systemspec-runbook: Roadmap

Items carry stable IDs; phase status is derived from its items.

## Phase 1: Model

- [x] **RMI-SYSTEMSPECRUNBOOK-001** The runbook model and its validation
- [x] **RMI-SYSTEMSPECRUNBOOK-002** The event log and each step's derived state
- [x] **RMI-SYSTEMSPECRUNBOOK-003** Markdown rendering with computed links, and link checks
- [x] **RMI-SYSTEMSPECRUNBOOK-004** JSON Schemas of a runbook and its log, generated from the types

## Phase 2: Checks

- [ ] **RMI-SYSTEMSPECRUNBOOK-005** The check runner: named checks registered in Go, `${name}` variables, and `pass`, `fail` and `skip` results
- [ ] **RMI-SYSTEMSPECRUNBOOK-006** Generic checks: `dns`, `http`, `unreachable` and `command`
- [ ] **RMI-SYSTEMSPECRUNBOOK-007** Recording verify results: `verified` for steps that pass, `failed` for done steps that no longer do

## Phase 3: Command Line

- [ ] **RMI-SYSTEMSPECRUNBOOK-008** Cobra commands a host tool mounts: list, show, record, verify, doc, schema
- [ ] **RMI-SYSTEMSPECRUNBOOK-009** `runbookspec`, the command line over a runbook and log in JSON

## Phase 4: Release

- [ ] **RMI-SYSTEMSPECRUNBOOK-010** Documentation site: guides for declaring a runbook, events and states, checks, and rendering
- [ ] **RMI-SYSTEMSPECRUNBOOK-011** The first release, after the model has run a real launch from start to end
