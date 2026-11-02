package runbookspec

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"slices"
	"time"
)

// EventType is what happened to a step.
type EventType string

// The event types.
const (
	// EventStarted: work on the step began.
	EventStarted EventType = "started"
	// EventDone: the step was done.
	EventDone EventType = "done"
	// EventVerified: the step's checks passed.
	EventVerified EventType = "verified"
	// EventFailed: the step, or its checks, failed.
	EventFailed EventType = "failed"
	// EventSkipped: the step was decided not to be needed.
	EventSkipped EventType = "skipped"
	// EventReopened: a done step must be done again.
	EventReopened EventType = "reopened"
)

// EventTypes are the valid types.
var EventTypes = []EventType{EventStarted, EventDone, EventVerified, EventFailed, EventSkipped, EventReopened}

// Event is one thing that happened to a step. It never holds a value.
type Event struct {
	// Step is the step's ID.
	Step string `json:"step"`
	// Type is what happened.
	Type EventType `json:"type"`
	// At is when, in UTC.
	At time.Time `json:"at"`
	// By is who: an owner.
	By Owner `json:"by,omitempty"`
	// Note adds context, such as what a failed check saw.
	Note string `json:"note,omitempty"`
}

// Log is a runbook's event log, as committed.
type Log struct {
	// Schema is the log's JSON Schema.
	Schema string `json:"$schema,omitempty"`
	// Runbook is the ID of the runbook the events belong to.
	Runbook string `json:"runbook"`
	// Events are in the order they were recorded.
	Events []Event `json:"events"`
}

// LoadLog reads a log; a missing file is an empty log for the runbook.
func LoadLog(path, runbookID string) (*Log, error) {
	data, err := os.ReadFile(path) //nolint:gosec // G304: the path is the operator's log file
	if errors.Is(err, fs.ErrNotExist) {
		return &Log{Runbook: runbookID}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("runbook: reading %s: %w", path, err)
	}
	var l Log
	if err := json.Unmarshal(data, &l); err != nil {
		return nil, fmt.Errorf("runbook: parsing %s: %w", path, err)
	}
	if l.Runbook != runbookID {
		return nil, fmt.Errorf("runbook: %s is the log of runbook %q, not %q", path, l.Runbook, runbookID)
	}
	return &l, nil
}

// Marshal returns the log as committed: indented, ending in a newline.
func (l *Log) Marshal() ([]byte, error) {
	if l.Events == nil {
		l.Events = []Event{}
	}
	out, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("runbook: log: %w", err)
	}
	return append(out, '\n'), nil
}

// secretLike matches what a note must never hold: key material, tokens and
// long random strings.
var secretLike = regexp.MustCompile(`-----BEGIN|github_pat_|gh[pousr]_[A-Za-z0-9]{20,}|AKIA[0-9A-Z]{16}|[A-Za-z0-9+/_=-]{40,}`)

// Withheld replaces what Redact removes from a note.
const Withheld = "[withheld]"

// Redact replaces each part of s that looks like a secret with Withheld,
// for a note the runner writes itself, such as a failed command's error.
// A long path matches as well as a token does, and a note a person types is
// refused instead, so they describe it.
func Redact(s string) string { return secretLike.ReplaceAllString(s, Withheld) }

// Append checks the event against the runbook and adds it.
func (l *Log) Append(rb *Runbook, e Event) error {
	if rb.Step(e.Step) == nil {
		return fmt.Errorf("runbook: no step %s in %s", e.Step, rb.ID)
	}
	if !slices.Contains(EventTypes, e.Type) {
		return fmt.Errorf("runbook: event %q is not one of %v", e.Type, EventTypes)
	}
	if e.By != "" && !validOwner(e.By) {
		return fmt.Errorf("runbook: by %q is not one of %v", e.By, Owners)
	}
	if e.At.IsZero() {
		return errors.New("runbook: an event needs a time")
	}
	if secretLike.MatchString(e.Note) {
		return errors.New("runbook: the note looks like it holds a secret; describe it instead")
	}
	e.At = e.At.UTC()
	l.Events = append(l.Events, e)
	return nil
}

// Validate checks every event against the runbook.
func (l *Log) Validate(rb *Runbook) error {
	var errs []error
	check := &Log{Runbook: l.Runbook}
	for i, e := range l.Events {
		if err := check.Append(rb, e); err != nil {
			errs = append(errs, fmt.Errorf("event %d: %w", i+1, err))
		}
	}
	return errors.Join(errs...)
}
