// Package schema holds the JSON Schemas of a runbook and of its event log,
// generated from the Go types and their documentation comments.
//
// Regenerate them after changing the types:
//
//	go generate ./schema
package schema

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"path"
	"path/filepath"
	"reflect"

	oscfs "github.com/grokify/oscompat/fs"
	"github.com/invopop/jsonschema"

	"github.com/plexusone/systemspec-runbook/runbookspec"
)

//go:generate go run ./gen

// ModulePath is the module the types belong to.
const ModulePath = "github.com/plexusone/systemspec-runbook"

// Schema IDs.
const (
	RunbookID = "https://github.com/plexusone/systemspec-runbook/schema/runbook.schema.json"
	EventsID  = "https://github.com/plexusone/systemspec-runbook/schema/events.schema.json"
)

// File names, relative to this package's directory.
const (
	RunbookFile = "runbook.schema.json"
	EventsFile  = "events.schema.json"
)

//go:embed runbook.schema.json
var runbook []byte

//go:embed events.schema.json
var events []byte

// Runbook returns a runbook's JSON Schema.
func Runbook() []byte { return runbook }

// Events returns the event log's JSON Schema.
func Events() []byte { return events }

// Generate builds both schemas from the Go types. root is the module's
// directory, absolute or relative to the working directory: the types'
// documentation comments are read from its source.
func Generate(root string) (runbook, events []byte, err error) {
	r, err := reflector(root)
	if err != nil {
		return nil, nil, err
	}
	rb := r.Reflect(&runbookspec.Runbook{})
	rb.ID = RunbookID
	rb.Title = "Runbook"
	rb.Description = "An ordered procedure: phases of steps, each with an owner, its dependencies, the actions that do it, the sentence that says when it is done, and the checks that prove it."

	ev := r.Reflect(&runbookspec.Log{})
	ev.ID = EventsID
	ev.Title = "Runbook events"
	ev.Description = "What happened to each step of a runbook, by whom and when; never a value."

	if runbook, err = marshal(rb); err != nil {
		return nil, nil, err
	}
	if events, err = marshal(ev); err != nil {
		return nil, nil, err
	}
	return runbook, events, nil
}

func reflector(root string) (*jsonschema.Reflector, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	cwd, err := filepath.Abs(".")
	if err != nil {
		return nil, err
	}
	rel, err := filepath.Rel(absRoot, cwd)
	if err != nil {
		return nil, err
	}
	// jsonschema keys comments by path.Join(base, directory walked), so a
	// base that undoes the walk's relative root yields import paths.
	base := path.Join(ModulePath, oscfs.NormalizePath(rel))
	r := &jsonschema.Reflector{Mapper: enums}
	if err := r.AddGoComments(base, root); err != nil {
		return nil, fmt.Errorf("schema: reading comments: %w", err)
	}
	r.CommentMap = normalizeKeys(r.CommentMap)
	return r, nil
}

// normalizeKeys returns comments keyed by clean import paths: on Windows
// jsonschema joins the base with backslashes and an unresolved "..".
func normalizeKeys(comments map[string]string) map[string]string {
	out := make(map[string]string, len(comments))
	for k, v := range comments {
		out[oscfs.NormalizePath(k)] = v
	}
	return out
}

// enums maps the closed sets to string enums.
func enums(t reflect.Type) *jsonschema.Schema {
	var values []any
	switch t {
	case reflect.TypeFor[runbookspec.Owner]():
		values = anys(runbookspec.Owners)
	case reflect.TypeFor[runbookspec.CheckKind]():
		values = anys(runbookspec.CheckKinds)
	case reflect.TypeFor[runbookspec.EventType]():
		values = anys(runbookspec.EventTypes)
	default:
		return nil
	}
	return &jsonschema.Schema{Type: "string", Enum: values}
}

func anys[T any](in []T) []any {
	out := make([]any, len(in))
	for i, v := range in {
		out[i] = v
	}
	return out
}

func marshal(s *jsonschema.Schema) ([]byte, error) {
	out, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("schema: %w", err)
	}
	return append(out, '\n'), nil
}
