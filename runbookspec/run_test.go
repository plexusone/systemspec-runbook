package runbookspec

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// runbookToRun has a ready step with a person's action, a command run with
// a variable and a random value, and a check; and a step that waits on it.
func runbookToRun() *Runbook {
	return &Runbook{
		ID: "launch", Title: "Launch",
		Variables: []Variable{{Name: "profile", Description: "the profile"}},
		Phases: []Phase{{ID: "1", Title: "Provision", Gives: "a stack", Steps: []Step{
			{ID: "1.1", Title: "Stack", Owner: OwnerBoth, Done: "the stack exists",
				Actions: []Action{
					{Owner: OwnerAgent, Text: "Write the file."},
					{Owner: OwnerOperator, Text: "Create it as {{profile}}.", Exec: []Exec{
						{Args: []string{"pulumi", "stack", "init", "prod"}, Dir: "stack", Env: map[string]string{"AWS_PROFILE": "{{profile}}"}},
						{Args: []string{"pulumi", "config", "set", "--secret", "pw"}, Dir: "stack", Stdin: &Stdin{RandomHex: 32}},
					}},
				},
				Checks: []Check{{ID: "c", Description: "it answers", Kind: KindNamed, Named: &NamedCheck{Name: "ok"}}},
			},
			{ID: "1.2", Title: "Up", Owner: OwnerOperator, Done: "up", DependsOn: []string{"1.1"},
				Actions: []Action{{Text: "Bring it up.", Exec: []Exec{{Args: []string{"pulumi", "up"}}}}}},
		}}},
	}
}

type harness struct {
	rb      *Runbook
	log     *Log
	out     bytes.Buffer
	ran     []Prepared
	answers []bool
	asked   []string
	failOn  string
	saves   int
}

func newHarness() *harness { rb := runbookToRun(); return &harness{rb: rb, log: &Log{Runbook: rb.ID}} }

func (h *harness) runner(values map[string]string) *Runner {
	return &Runner{
		Values:  NewValues(h.rb, MapSource(values)),
		Checker: &Checker{Named: map[string]NamedFunc{"ok": func(context.Context, map[string]string) (string, error) { return "fine", nil }}},
		Root:    "/repo",
		Out:     &h.out,
		Prompt: func(q string) (bool, error) {
			h.asked = append(h.asked, strings.TrimSpace(q))
			if len(h.answers) == 0 {
				return false, nil
			}
			a := h.answers[0]
			h.answers = h.answers[1:]
			return a, nil
		},
		Exec: func(_ context.Context, p Prepared, _ io.Reader, _, _ io.Writer) error {
			h.ran = append(h.ran, p)
			if h.failOn != "" && strings.Contains(p.Line, h.failOn) {
				return errors.New("exit status 1")
			}
			return nil
		},
		Save: func(*Log) error { h.saves++; return nil },
		Now:  func() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC) },
	}
}

func (h *harness) types() string {
	var t []string
	for _, e := range h.log.Events {
		t = append(t, e.Step+":"+string(e.Type))
	}
	return strings.Join(t, " ")
}

func TestRunConfirmsRunsRecordsAndVerifies(t *testing.T) {
	h := newHarness()
	h.answers = []bool{true, true, true} // the agent's action, then each command
	if err := h.runner(map[string]string{"profile": "ops"}).Run(context.Background(), h.rb, h.log, "1.1"); err != nil {
		t.Fatalf("Run: %v\n%s", err, h.out.String())
	}
	if got := h.types(); got != "1.1:started 1.1:done 1.1:verified" {
		t.Errorf("events = %s", got)
	}
	if h.saves != 3 {
		t.Errorf("saved %d times, want after each event", h.saves)
	}
	if len(h.ran) != 2 {
		t.Fatalf("ran %d commands", len(h.ran))
	}
	first, second := h.ran[0], h.ran[1]
	if strings.Join(first.Args, " ") != "pulumi stack init prod" || first.Dir != filepath.Join("/repo", "stack") || strings.Join(first.Env, " ") != "AWS_PROFILE=ops" {
		t.Errorf("first command = %+v", first)
	}
	if second.RandomHex != 32 || !strings.Contains(second.Line, "openssl rand -hex 32 | pulumi config set") {
		t.Errorf("second command = %+v", second)
	}
	if !strings.Contains(h.asked[0], "Has the agent done this?") || h.asked[1] != "Run this command?" {
		t.Errorf("asked = %v", h.asked)
	}
	out := h.out.String()
	for _, want := range []string{"Create it as ops.", "  $ cd stack\n  $ export AWS_PROFILE=ops\n  $ pulumi stack init prod", "pass it answers: fine"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestRunWithYesStillConfirmsAPersonsAction(t *testing.T) {
	h := newHarness()
	h.answers = []bool{true}
	r := h.runner(map[string]string{"profile": "ops"})
	r.Yes = true
	if err := r.Run(context.Background(), h.rb, h.log, "1.1"); err != nil {
		t.Fatal(err)
	}
	if len(h.asked) != 1 || len(h.ran) != 2 {
		t.Errorf("asked %v, ran %d", h.asked, len(h.ran))
	}
}

func TestRunStopsWhenDeclinedAndOnFailure(t *testing.T) {
	h := newHarness()
	h.answers = []bool{true, false} // the agent's action is done; the first command is declined
	if err := h.runner(map[string]string{"profile": "ops"}).Run(context.Background(), h.rb, h.log, "1.1"); !errors.Is(err, ErrStopped) {
		t.Fatalf("declined: %v", err)
	}
	if got := h.types(); got != "1.1:started" || len(h.ran) != 0 {
		t.Errorf("declined: events %s, ran %d", got, len(h.ran))
	}

	h = newHarness()
	h.answers, h.failOn = []bool{true, true, true}, "config set"
	err := h.runner(map[string]string{"profile": "ops"}).Run(context.Background(), h.rb, h.log, "1.1")
	if err == nil || !strings.Contains(err.Error(), "command 2 (pulumi) failed") {
		t.Fatalf("failure: %v", err)
	}
	if got := h.types(); got != "1.1:started 1.1:failed" {
		t.Errorf("failure: events %s", got)
	}
	if st := Statuses(h.rb, h.log)[0].State; st != StateInProgress {
		t.Errorf("after a failure the step is %s", st)
	}
}

func TestRunRefusesBeforeAnythingHappens(t *testing.T) {
	h := newHarness()
	if err := h.runner(nil).Run(context.Background(), h.rb, h.log, "1.2"); err == nil || !strings.Contains(err.Error(), "waits for 1.1") {
		t.Errorf("blocked: %v", err)
	}
	var u *UnfilledError
	if err := h.runner(nil).Run(context.Background(), h.rb, h.log, "1.1"); !errors.As(err, &u) || u.Names[0] != "profile" {
		t.Errorf("unfilled: %v", err)
	}
	if len(h.log.Events) != 0 || len(h.ran) != 0 {
		t.Errorf("refused runs recorded %v and ran %d", h.log.Events, len(h.ran))
	}
	r := h.runner(nil)
	r.Force, r.Yes = true, true
	if err := r.Run(context.Background(), h.rb, h.log, "1.2"); err != nil {
		t.Fatalf("forced: %v", err)
	}
	if e := h.log.Events[0]; e.Type != EventStarted || !strings.Contains(e.Note, "forced") {
		t.Errorf("forced start = %+v", e)
	}
}

func TestLinePromptReadsOneLine(t *testing.T) {
	in := strings.NewReader("yes\nrest for the command\n")
	var out bytes.Buffer
	ok, err := LinePrompt(in, &out)("Run?")
	if err != nil || !ok || out.String() != "Run? [y/N] " {
		t.Errorf("prompt = %v, %v, %q", ok, err, out.String())
	}
	rest, err := io.ReadAll(in)
	if err != nil || string(rest) != "rest for the command\n" {
		t.Errorf("the prompt took input meant for the command: %q", rest)
	}
	if ok, err := LinePrompt(strings.NewReader(""), io.Discard)("Run?"); ok || err != nil {
		t.Errorf("end of input = %v, %v", ok, err)
	}
}

func TestExecCommandPipesAndRandom(t *testing.T) {
	if _, err := os.Stat("/bin/cat"); err != nil {
		t.Skip("needs cat and echo")
	}
	var out bytes.Buffer
	err := ExecCommand(context.Background(), Prepared{Args: []string{"cat"}, From: []string{"echo", "piped"}}, nil, &out, io.Discard)
	if err != nil || out.String() != "piped\n" {
		t.Errorf("pipe: %q, %v", out.String(), err)
	}
	out.Reset()
	if err := ExecCommand(context.Background(), Prepared{Args: []string{"cat"}, RandomHex: 16}, nil, &out, io.Discard); err != nil || len(out.String()) != 32 {
		t.Errorf("random: %q, %v", out.String(), err)
	}
	if err := ExecCommand(context.Background(), Prepared{Args: []string{"cat"}, From: []string{"false"}}, nil, io.Discard, io.Discard); err == nil {
		t.Error("a failing command piped in was not reported")
	}
}

// A later variable is produced by an earlier command of the same run, as a
// stack's outputs are once it is up.
func TestRunFillsALaterVariableWhenItsCommandRuns(t *testing.T) {
	rb := &Runbook{
		ID: "launch", Title: "Launch",
		Variables: []Variable{{Name: "ip", Description: "the address the first command creates", Later: true}},
		Phases: []Phase{{ID: "1", Title: "Up", Gives: "a host", Steps: []Step{{
			ID: "1.1", Title: "Up", Owner: OwnerOperator, Done: "up",
			Actions: []Action{{Text: "Bring it up and reach it.", Exec: []Exec{
				{Args: []string{"pulumi", "up"}},
				{Args: []string{"ssh", "ubuntu@{{ip}}", "true"}},
			}}},
		}}}},
	}
	outputs := map[string]string{}
	source := MapSource(outputs) // reads the map as it is when asked
	var ran []string
	run := func(produce bool) (*Log, error) {
		l := &Log{Runbook: rb.ID}
		ran = nil
		r := &Runner{
			Values: NewValues(rb, source), Yes: true, Out: io.Discard,
			Exec: func(_ context.Context, p Prepared, _ io.Reader, _, _ io.Writer) error {
				ran = append(ran, strings.Join(p.Args, " "))
				if p.Args[0] == "pulumi" && produce {
					outputs["ip"] = "192.0.2.10"
				}
				return nil
			},
		}
		return l, r.Run(context.Background(), rb, l, "1.1")
	}

	l, err := run(true)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if strings.Join(ran, "; ") != "pulumi up; ssh ubuntu@192.0.2.10 true" || l.Events[len(l.Events)-1].Type != EventDone {
		t.Errorf("ran %v, events %v", ran, l.Events)
	}

	delete(outputs, "ip")
	l, err = run(false)
	if err == nil || !strings.Contains(err.Error(), "no value for ip") {
		t.Fatalf("still unfilled at its command: %v", err)
	}
	if len(ran) != 1 || l.Events[len(l.Events)-1].Type != EventFailed {
		t.Errorf("ran %v, events %v", ran, l.Events)
	}

	// A variable that is not later is still required before anything runs.
	rb.Variables[0].Later = false
	l, err = run(true)
	var u *UnfilledError
	if !errors.As(err, &u) || len(ran) != 0 || len(l.Events) != 0 {
		t.Errorf("not later: %v, ran %v, events %v", err, ran, l.Events)
	}
}
