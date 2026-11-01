package schema

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// TestEmbeddedSchemasAreCurrent fails when the types changed and the
// schemas were not regenerated with go generate ./schema.
func TestEmbeddedSchemasAreCurrent(t *testing.T) {
	rb, ev, err := Generate("..")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(rb, Runbook()) {
		t.Error("runbook.schema.json is stale: run go generate ./schema")
	}
	if !bytes.Equal(ev, Events()) {
		t.Error("events.schema.json is stale: run go generate ./schema")
	}
}

func TestSchemaContent(t *testing.T) {
	var s struct {
		ID   string `json:"$id"`
		Defs map[string]struct {
			Description string `json:"description"`
			Properties  map[string]struct {
				Description string `json:"description"`
				Enum        []any  `json:"enum"`
			} `json:"properties"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(Runbook(), &s); err != nil {
		t.Fatal(err)
	}
	if s.ID != RunbookID {
		t.Errorf("$id = %s", s.ID)
	}
	step, ok := s.Defs["Step"]
	if !ok {
		t.Fatal("no Step definition")
	}
	if !strings.Contains(step.Properties["done"].Description, "when the step is done") {
		t.Errorf("field comments were not read: %q", step.Properties["done"].Description)
	}
	if got := step.Properties["owner"].Enum; len(got) != 3 {
		t.Errorf("owner enum = %v", got)
	}
	if got := s.Defs["Check"].Properties["kind"].Enum; len(got) != 5 {
		t.Errorf("check kind enum = %v", got)
	}
}
