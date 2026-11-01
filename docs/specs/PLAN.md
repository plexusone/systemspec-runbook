# systemspec-runbook: Plan

## Approach

1. **The model.** Types, validation, the event log, derived state,
   rendering, link checks and schemas: enough for a program to declare a
   runbook, generate its page and record progress.
2. **Checks.** The runner and the generic checks, then recording verify
   results, so every step can be checked.
3. **Command line.** Commands over JSON files, and Cobra commands a host
   tool mounts over its own runbook.
4. **Release.** A documentation site and the first release, once the model
   has run a real launch from start to end.

## Working Rules

- **Declared by the program that uses it.** A runbook is built in Go from
  the user's own facts, so hosts, ports and counts are never typed twice.
- **A check for every step,** including the ones a person does.
- **Public-ready from the first commit:** examples and tests use neutral
  names.
