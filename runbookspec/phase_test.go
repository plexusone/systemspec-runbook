package runbookspec

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
)

// withFixes gives the runbook to run a fix of its own and step 1.2 one of
// its own for the same failure, plus one that never matches.
func withFixes(rb *Runbook) *Runbook {
	rb.Troubleshoot = []Fix{
		{Match: `port 22: Operation timed out`, Fix: "Add your address to the admin CIDRs."},
		{Match: `Permission denied \(publickey\)`, Fix: "Load the key with `ssh-add`."},
	}
	rb.Phases[0].Steps[1].Troubleshoot = []Fix{{Match: `Operation timed out`, Fix: "Check the instance is running as {{profile}}."}}
	return rb
}

func TestRunPhaseRunsInOrderAndSkipsFinished(t *testing.T) {
	h := newHarness()
	h.answers = []bool{true} // the agent's action in 1.1
	r := h.runner(map[string]string{"profile": "ops"})
	r.Yes = true
	if err := r.RunPhase(context.Background(), h.rb, h.log, "1"); err != nil {
		t.Fatalf("RunPhase: %v\n%s", err, h.out.String())
	}
	var programs []string
	for _, p := range h.ran {
		programs = append(programs, strings.Join(p.Args[:2], " "))
	}
	if got := strings.Join(programs, ", "); got != "pulumi stack, pulumi config, pulumi up" {
		t.Errorf("ran in the wrong order: %s", got)
	}
	if !strings.Contains(h.out.String(), "2 step(s) to run, 1.1, 1.2; 0 already done") || !strings.Contains(h.out.String(), "Phase 1 (Provision) is done: a stack.") {
		t.Errorf("output:\n%s", h.out.String())
	}

	// Again: every step is finished, so nothing runs.
	h.ran = nil
	h.out.Reset()
	r = h.runner(map[string]string{"profile": "ops"})
	if err := r.RunPhase(context.Background(), h.rb, h.log, "1"); err != nil || len(h.ran) != 0 || !strings.Contains(h.out.String(), "is done: every step is done or verified") {
		t.Errorf("second run: %v, ran %d\n%s", err, len(h.ran), h.out.String())
	}
	if err := r.RunPhase(context.Background(), h.rb, h.log, "9"); err == nil {
		t.Error("ran a phase that does not exist")
	}
}

func TestRunPhaseStopsAtFailureWithReport(t *testing.T) {
	h := newHarness()
	h.rb = withFixes(h.rb)
	h.answers = []bool{true}
	r := h.runner(map[string]string{"profile": "ops"})
	r.Yes = true
	r.Command = "bin/infra runbook run"
	r.Exec = func(_ context.Context, p Prepared, _ io.Reader, _, stderr io.Writer) error {
		h.ran = append(h.ran, p)
		if p.Args[0] == "pulumi" && p.Args[1] == "up" {
			for i := 1; i <= 25; i++ {
				_, _ = fmt.Fprintf(stderr, "line %d\n", i) //nolint:errcheck // a test's fake output
			}
			_, _ = fmt.Fprintln(stderr, "ssh: connect to host 192.0.2.1 port 22: Operation timed out") //nolint:errcheck // as above
			return errors.New("exit status 255")
		}
		return nil
	}
	err := r.RunPhase(context.Background(), h.rb, h.log, "1")
	var f *Failure
	if !errors.As(err, &f) || f.Step.ID != "1.2" || f.Action != 1 || f.Command != 1 {
		t.Fatalf("err = %v", err)
	}
	if err.Error() != "runbook: action 1, command 1 (pulumi) failed: exit status 255" {
		t.Errorf("Error() = %q", err.Error())
	}
	out := h.out.String()
	report := out[strings.Index(out, "Phase 1 (Provision) stopped"):]
	t.Logf("report:\n%s", report)
	for _, want := range []string{
		"Phase 1 (Provision) stopped at step 1.2 \"Up\", action 1, command 1:\n    pulumi up\n",
		"Error: exit status 255\n",
		"    line 7\n",
		"    ssh: connect to host 192.0.2.1 port 22: Operation timed out\n",
		"Fix:\n  - Check the instance is running as ops.\n  - Add your address to the admin CIDRs.\n",
		"Resume the phase: bin/infra runbook run 1\n",
		"Retry the step:   bin/infra runbook run 1.2",
	} {
		if !strings.Contains(report, want) {
			t.Errorf("the report lacks %q", want)
		}
	}
	if strings.Contains(report, "    line 6\n") || strings.Contains(report, "ssh-add") {
		t.Error("the report shows more than the output's last 20 lines, or a fix that does not match")
	}
	if !strings.Contains(h.types(), "1.2:failed") {
		t.Errorf("events: %s", h.types())
	}
}

func TestRunPhaseReportsEveryMissingVariableFirst(t *testing.T) {
	h := newHarness()
	h.rb.Variables = []Variable{
		{Name: "profile", Description: "the profile", Env: "RUNBOOKSPEC_TEST_UNSET_PROFILE", Example: "ops"},
		{Name: "region", Description: "the region", Env: "RUNBOOKSPEC_TEST_UNSET_REGION"},
	}
	h.rb.Phases[0].Steps[1].Actions[0].Exec[0].Args = []string{"pulumi", "up", "--region", "{{region}}"}
	h.rb.Troubleshoot = []Fix{{Match: `no value for`, Fix: "Set each variable."}}
	r := h.runner(nil)
	r.Command = "rb run"
	err := r.RunPhase(context.Background(), h.rb, h.log, "1")
	var u *UnfilledError
	if !errors.As(err, &u) || strings.Join(u.Names, ",") != "profile,region" {
		t.Fatalf("err = %v", err)
	}
	if len(h.ran) != 0 || len(h.log.Events) != 0 {
		t.Errorf("something ran or was recorded: %d, %s", len(h.ran), h.types())
	}
	for _, want := range []string{
		"Phase 1 (Provision) could not start:",
		"profile: the profile ($RUNBOOKSPEC_TEST_UNSET_PROFILE or --var profile=…, such as ops)",
		"region: the region ($RUNBOOKSPEC_TEST_UNSET_REGION or --var region=…)",
		"Fix:\n  - Set each variable.",
		"Resume the phase: rb run 1",
	} {
		if !strings.Contains(h.out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, h.out.String())
		}
	}
}

func TestRunPhaseStopsAtAStepWaitingOutsideIt(t *testing.T) {
	h := newHarness()
	h.rb.Phases = append(h.rb.Phases, Phase{ID: "2", Title: "Use", Gives: "use", Steps: []Step{
		{ID: "2.1", Title: "Use it", Owner: OwnerOperator, Done: "used", DependsOn: []string{"1.2"},
			Actions: []Action{{Text: "Use it.", Exec: []Exec{{Args: []string{"use"}}}}}},
	}})
	r := h.runner(map[string]string{"profile": "ops"})
	r.Yes = true
	if err := r.RunPhase(context.Background(), h.rb, h.log, "2"); err == nil || len(h.ran) != 0 || !strings.Contains(h.out.String(), "it waits for 1.2, which is not done") {
		t.Errorf("err = %v, ran %d\n%s", err, len(h.ran), h.out.String())
	}
}

func TestFixesStepBeforeRunbook(t *testing.T) {
	rb := withFixes(runbookToRun())
	got := rb.Fixes(rb.Step("1.2"), "port 22: Operation timed out")
	if len(got) != 2 || !strings.HasPrefix(got[0].Fix, "Check the instance") || !strings.HasPrefix(got[1].Fix, "Add your address") {
		t.Errorf("fixes: %+v", got)
	}
	if got := rb.Fixes(nil, "Permission denied (publickey)"); len(got) != 1 || !strings.Contains(got[0].Fix, "ssh-add") {
		t.Errorf("runbook fixes: %+v", got)
	}
	if got := rb.Fixes(rb.Step("1.1"), "all fine"); len(got) != 0 {
		t.Errorf("matched nothing yet got %+v", got)
	}
}

func TestValidateRejectsABadFix(t *testing.T) {
	rb := runbookToRun()
	rb.Troubleshoot = []Fix{{Match: "(unclosed", Fix: "x"}}
	rb.Phases[0].Steps[0].Troubleshoot = []Fix{{Match: "ok", Fix: " "}, {Fix: "y"}, {Match: "z", Fix: "uses {{nope}}"}}
	err := rb.Validate(ValidateOptions{})
	for _, want := range []string{"troubleshoot 1: match \"(unclosed\"", "step 1.1: troubleshoot 1: fix is required", "step 1.1: troubleshoot 2: match is required", "references {{nope}}"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("Validate lacks %q: %v", want, err)
		}
	}
}

func TestRenderTroubleshooting(t *testing.T) {
	rb := withFixes(runbookToRun())
	md, err := rb.Markdown(RenderOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"## Troubleshooting\n", "- `port 22: Operation timed out`: Add your address to the admin CIDRs.", "**Troubleshooting:**\n\n- `Operation timed out`: Check the instance"} {
		if !strings.Contains(string(md), want) {
			t.Errorf("the Markdown lacks %q", want)
		}
	}
	page, err := rb.HTML(HTMLOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`<h2 id="troubleshooting">Troubleshooting</h2>`, `<ul class="fixes"><li><code>port 22: Operation timed out</code>:`, "<code>ssh-add</code>", `<span class="label">Troubleshooting:</span>`} {
		if !strings.Contains(string(page), want) {
			t.Errorf("the HTML lacks %q", want)
		}
	}
}

// TestRunPhaseStopsWhenChecksFail checks that a step whose actions are
// done but whose checks fail is recorded as failed, returns ErrChecksFailed,
// and stops the phase with a report instead of calling it done.
func TestRunPhaseStopsWhenChecksFail(t *testing.T) {
	h := newHarness()
	h.answers = []bool{true}
	h.rb.Phases[0].Steps[0].Checks = []Check{{ID: "www", Description: "www is sent to HTTPS", Kind: KindCommand,
		Command: &CommandCheck{Args: []string{"curl", "http://www.example.com"}}}}
	r := h.runner(map[string]string{"profile": "ops"})
	r.Yes = true
	r.Command = "bin/infra runbook run"
	r.Checker = &Checker{Command: func(context.Context, []string) (string, error) {
		return "", errors.New("exit status 6")
	}}
	err := r.RunPhase(context.Background(), h.rb, h.log, "1")
	if !errors.Is(err, ErrChecksFailed) {
		t.Fatalf("err = %v\n%s", err, h.out.String())
	}
	out := h.out.String()
	for _, want := range []string{
		`Phase 1 (Provision) stopped at step 1.1 "`,
		"its actions are done, but its checks failed (www)",
		"check again: bin/infra runbook verify 1.1 --record",
		"Resume the phase: bin/infra runbook run 1",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the output lacks %q\n%s", want, out)
		}
	}
	if strings.Contains(out, "is done:") || len(h.ran) == 0 || h.ran[len(h.ran)-1].Args[1] == "up" {
		t.Errorf("the phase went on past the failed step\n%s", out)
	}
	if !strings.Contains(h.types(), "1.1:failed") {
		t.Errorf("events: %s", h.types())
	}
}
