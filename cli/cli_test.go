package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	rb "github.com/plexusone/systemspec-runbook/runbookspec"
)

// testRunbook has a person's step with a check, and a step that runs the
// go command, which every machine that runs these tests has.
func testRunbook() *rb.Runbook {
	return &rb.Runbook{
		ID: "launch", Title: "Launch",
		Variables: []rb.Variable{{Name: "what", Description: "what go prints"}},
		Phases: []rb.Phase{{ID: "1", Title: "Prepare", Gives: "a machine", Steps: []rb.Step{
			{ID: "1.1", Title: "Sign in", Owner: rb.OwnerOperator, Done: "signed in",
				Actions: []rb.Action{{Text: "Sign in."}},
				Checks:  []rb.Check{{ID: "c", Description: "signed in", Kind: rb.KindNamed, Named: &rb.NamedCheck{Name: "signed-in"}}}},
			{ID: "1.2", Title: "Go", Owner: rb.OwnerOperator, Done: "go runs", DependsOn: []string{"1.1"},
				Actions: []rb.Action{{Text: "Print the {{what}}.", Exec: []rb.Exec{{Args: []string{"go", "env", "{{what}}"}}}}}},
		}}},
	}
}

type env struct {
	dir, events string
	signedIn    bool
}

func (e *env) exec(t *testing.T, stdin string, args ...string) (string, error) {
	t.Helper()
	cmd := New(Options{
		Load:       func() (*rb.Runbook, error) { return testRunbook(), nil },
		EventsPath: e.events,
		Root:       e.dir,
		Named: map[string]rb.NamedFunc{"signed-in": func(context.Context, map[string]string) (string, error) {
			if e.signedIn {
				return "yes", nil
			}
			return "", errors.New("not signed in")
		}},
		Render: func() (rb.RenderOptions, error) { return rb.RenderOptions{Generator: "test"}, nil },
	})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetArgs(args)
	err := cmd.ExecuteContext(context.Background())
	return out.String(), err
}

func newEnv(t *testing.T) *env {
	dir := t.TempDir()
	return &env{dir: dir, events: filepath.Join(dir, "log", "events.json")}
}

func TestListShowRecord(t *testing.T) {
	e := newEnv(t)
	out, err := e.exec(t, "", "list")
	if err != nil || !strings.Contains(out, "1.2   blocked") || !strings.Contains(out, "2 steps: 1 ready, 1 blocked") {
		t.Fatalf("list: %v\n%s", err, out)
	}
	if out, err = e.exec(t, "", "show", "1.2"); err != nil || !strings.Contains(out, "go env <what>") || !strings.Contains(out, "Run it: run 1.2") {
		t.Errorf("show: %v\n%s", err, out)
	}
	if out, err = e.exec(t, "", "record", "1.1", "done", "--by", "operator"); err != nil || !strings.Contains(out, "now done") {
		t.Fatalf("record: %v\n%s", err, out)
	}
	data, err := os.ReadFile(e.events)
	if err != nil || !strings.Contains(string(data), "systemspec-runbook/schema/events.schema.json") {
		t.Errorf("the log: %v\n%s", err, data)
	}
	if out, err = e.exec(t, "", "validate"); err != nil || !strings.Contains(out, "2 steps and 1 events OK") {
		t.Errorf("validate: %v\n%s", err, out)
	}
	if _, err = e.exec(t, "", "record", "1.1", "done", "--note", "github_pat_11AAAAAAAA"); err == nil {
		t.Error("a note that looks like a secret was recorded")
	}
}

func TestVerifyRecordsAndFails(t *testing.T) {
	e := newEnv(t)
	if _, err := e.exec(t, "", "record", "1.1", "done"); err != nil {
		t.Fatal(err)
	}
	out, err := e.exec(t, "", "verify", "1.1", "--record")
	if err == nil || !strings.Contains(out, "fail signed in: not signed in") {
		t.Fatalf("a failing check: %v\n%s", err, out)
	}
	e.signedIn = true
	if out, err = e.exec(t, "", "verify", "--all", "--record"); err != nil {
		t.Fatalf("verify --all: %v\n%s", err, out)
	}
	if out, _ = e.exec(t, "", "list"); !strings.Contains(out, "1.1   verified") {
		t.Errorf("after verify:\n%s", out)
	}
	if _, err := e.exec(t, "", "verify"); err == nil {
		t.Error("verify with no steps and no --all passed")
	}
}

func TestRun(t *testing.T) {
	e := newEnv(t)
	e.signedIn = true
	// Blocked until 1.1 is done.
	if _, err := e.exec(t, "", "run", "1.2", "--var", "what=GOOS"); err == nil {
		t.Fatal("ran a blocked step")
	}
	// A person's step: confirmed, recorded, then checked.
	out, err := e.exec(t, "y\n", "run", "1.1")
	if err != nil || !strings.Contains(out, "pass signed in: yes") {
		t.Fatalf("run 1.1: %v\n%s", err, out)
	}
	// Missing variable: refused before anything runs.
	if _, err := e.exec(t, "", "run", "1.2"); err == nil || !strings.Contains(err.Error(), "no value for what") {
		t.Fatalf("run without the variable: %v", err)
	}
	out, err = e.exec(t, "y\n", "run", "1.2", "--var", "what=GOOS")
	if err != nil || !strings.Contains(out, "$ go env GOOS") {
		t.Fatalf("run 1.2: %v\n%s", err, out)
	}
	if out, _ = e.exec(t, "", "list"); !strings.Contains(out, "1.1   verified") || !strings.Contains(out, "1.2   done") {
		t.Errorf("after the runs:\n%s", out)
	}
}

func TestDocAndSchema(t *testing.T) {
	e := newEnv(t)
	page := filepath.Join(e.dir, "page.md")
	if _, err := e.exec(t, "", "doc", "-o", page); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(page)
	if err != nil || !strings.Contains(string(data), "go env <what>") || !strings.Contains(string(data), "Generated by test") {
		t.Errorf("page: %v\n%s", err, data)
	}
	out, err := e.exec(t, "", "schema", "--log")
	if err != nil || !strings.Contains(out, `"$id": "https://github.com/plexusone/systemspec-runbook/schema/events.schema.json"`) {
		t.Errorf("schema --log: %v", err)
	}
}

// TestVariableFromCommand checks a step whose check needs a variable that
// only a command gives: go version names the GOOS that go env prints.
func TestVariableFromCommand(t *testing.T) {
	dir := t.TempDir()
	r := &rb.Runbook{
		ID: "launch", Title: "Launch",
		Variables: []rb.Variable{{Name: "goos", Description: "the operating system go builds for",
			From: &rb.Exec{Args: []string{"go", "env", "GOOS"}}}},
		Phases: []rb.Phase{{ID: "1", Title: "Prepare", Gives: "a machine", Steps: []rb.Step{
			{ID: "1.1", Title: "Go", Owner: rb.OwnerOperator, Done: "go runs",
				Actions: []rb.Action{{Text: "Install go."}},
				Checks: []rb.Check{{ID: "os", Description: "go builds for this system", Kind: rb.KindCommand,
					Command: &rb.CommandCheck{Args: []string{"go", "version"}, Contains: "{{goos}}"}}}},
		}}},
	}
	if err := r.Validate(rb.ValidateOptions{}); err != nil {
		t.Fatal(err)
	}
	cmd := New(Options{Load: func() (*rb.Runbook, error) { return r, nil }, EventsPath: filepath.Join(dir, "events.json")})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"verify", "1.1", "--root", dir})
	if err := cmd.ExecuteContext(context.Background()); err != nil || !strings.Contains(out.String(), "pass go builds for this system") {
		t.Fatalf("verify: %v\n%s", err, out.String())
	}
}
