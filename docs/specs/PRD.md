# systemspec-runbook: Product Requirements

## Summary

A runbook as data: phases of steps, each with an owner, its dependencies,
the actions that do it, the sentence that says when it is done, and typed
checks that prove it. A Go program declares the runbook; systemspec-runbook
validates it, renders it as a Markdown page, records what was done in an
event log, derives where each step stands, and runs its checks.

## Problem

- **Runbooks are prose.** "Done when the zone is active" is read, not run.
  Whether a step still holds after an incident is checked by hand, by
  whoever remembers how.
- **Pages drift from the tools.** A runbook names commands, hosts, ports and
  sections of other generated pages; nothing checks they still match.
- **Progress is not recorded.** Which steps are done, by whom and when,
  lives in a chat or a person's memory.
- **Steps repeat.** A second environment, a second site or a rebuild runs
  most of the same steps, typed again from the page.

## Users

| User | Needs |
|---|---|
| An operator | What to do next, the exact commands, a check of the result, and a record of what was done |
| A coding agent working beside the operator | The same steps, owners and checks, to prepare work, verify it and record it without handling secrets |
| A reader after an incident | A check of everything the runbook set up, and the history of when it was done |

## Goals

1. **A typed model** of runbooks, phases, steps, actions, links and checks,
   with validation that reports every problem together.
2. **A generated page** in Markdown, whose links to steps and to other
   generated pages are computed, never typed.
3. **An event log** of what happened to each step, with each step's state
   derived from its events and its dependencies.
4. **Checks that can be run** for every step, including steps a person does
   in a console: DNS answers, HTTP statuses, addresses that must not
   answer, command results, and checks a program registers by name.
5. **JSON Schemas** of a runbook and of its log, generated from the types.
6. **A command line** over JSON files, and Cobra commands a host tool mounts
   over its own runbook.

## Non-goals

- **A workflow engine.** Nothing runs steps on a schedule or on its own; a
  person or an agent runs each one.
- **Secrets.** No type holds a value; checks read names, settings and
  public answers; the log refuses notes that look like secrets.
- **Automating console steps.** A step done once in a console keeps an
  instruction and gains a check.

## Success Measures

- A real launch is run from a runbook declared with it, and its checks say
  where the launch stands at any time.
- A second environment is set up from the same runbook with no step typed
  from prose that has a command.
- The rendered page has no broken links, by test.
