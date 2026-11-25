package runbookspec

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withInputs gives the runbook to run a token step 1.2 needs, a key-agent
// check step 1.1 needs, and variables of every kind.
func withInputs(rb *Runbook, agentHasKey bool) (*Runbook, *Checker) {
	rb.Variables = []Variable{
		{Name: "profile", Description: "the profile", Env: "RUNBOOKSPEC_TEST_PROFILE", Example: "ops"},
		{Name: "ip", Description: "the address", From: &Exec{Args: []string{"pulumi", "stack", "output", "ip"}}},
		{Name: "rev", Description: "the revision", Later: true},
	}
	rb.Inputs = []Input{
		{Name: "API_TOKEN", Description: "a token for the API", Env: "RUNBOOKSPEC_TEST_TOKEN", Secret: true,
			HowTo: "Create it at [Tokens](https://example.com/tokens) with Zone: Read."},
		{Name: "agent-key", Description: "the key, loaded in the SSH agent",
			Check: &CommandCheck{Args: []string{"ssh-add", "-l"}, Contains: "SHA256:abc"}},
	}
	rb.Phases[0].Steps[0].Needs = []string{"agent-key"}
	rb.Phases[0].Steps[1].Needs = []string{"API_TOKEN"}
	rb.Phases[0].Steps[1].Actions[0].Exec[0].Args = []string{"pulumi", "up", "--ip", "{{ip}}", "--rev", "{{rev}}"}
	checker := &Checker{Command: func(_ context.Context, args []string) (string, error) {
		if agentHasKey {
			return "256 SHA256:abc fleet (ED25519)\n", nil
		}
		return "The agent has no identities.\n", errors.New("exit status 1")
	}}
	return rb, checker
}

func TestRequirements(t *testing.T) {
	rb, checker := withInputs(runbookToRun(), false)
	if err := rb.Validate(ValidateOptions{}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RUNBOOKSPEC_TEST_PROFILE", "ops")
	t.Setenv("RUNBOOKSPEC_TEST_TOKEN", "")
	r := &Runner{Values: NewValues(rb), Checker: checker}
	got := map[string]Requirement{}
	for _, req := range r.Requirements(context.Background(), rb, rb.Steps()) {
		got[req.Name] = req
	}
	for name, want := range map[string]string{"profile": RequirementSet, "ip": RequirementDerived, "rev": RequirementLater,
		"API_TOKEN": RequirementMissing, "agent-key": RequirementMissing} {
		if got[name].State != want {
			t.Errorf("%s: %s (%s); want %s", name, got[name].State, got[name].Detail, want)
		}
	}
	if s := strings.Join(got["profile"].Steps, ","); s != "1.1" || got["profile"].Detail != "from $RUNBOOKSPEC_TEST_PROFILE" {
		t.Errorf("profile: %+v", got["profile"])
	}
	if !got["API_TOKEN"].Secret || got["API_TOKEN"].Detail != "$RUNBOOKSPEC_TEST_TOKEN is not set" || strings.Join(got["API_TOKEN"].Steps, ",") != "1.2" {
		t.Errorf("token: %+v", got["API_TOKEN"])
	}
	if !strings.HasPrefix(got["agent-key"].Detail, "ssh-add -l: ") {
		t.Errorf("agent key: %+v", got["agent-key"])
	}

	t.Setenv("RUNBOOKSPEC_TEST_TOKEN", "s3cret-value")
	_, r.Checker = withInputs(runbookToRun(), true)
	for _, req := range r.Requirements(context.Background(), rb, rb.Steps()) {
		if req.State == RequirementMissing || strings.Contains(req.Detail, "s3cret") {
			t.Errorf("%s: %s %s", req.Name, req.State, req.Detail)
		}
	}
}

func TestRunPhaseRefusesWithAnInputMissing(t *testing.T) {
	h := newHarness()
	var checker *Checker
	h.rb, checker = withInputs(h.rb, false)
	h.rb.Variables[1].From = nil // given below
	t.Setenv("RUNBOOKSPEC_TEST_TOKEN", "")
	r := h.runner(map[string]string{"profile": "ops", "ip": "192.0.2.1"})
	r.Checker = checker
	r.Command = "rb run"
	err := r.RunPhase(context.Background(), h.rb, h.log, "1")
	var mi *MissingInputsError
	if !errors.As(err, &mi) || len(mi.Inputs) != 2 || len(h.ran) != 0 || len(h.log.Events) != 0 {
		t.Fatalf("err = %v, ran %d, events %s", err, len(h.ran), h.types())
	}
	for _, want := range []string{"Phase 1 (Provision) could not start:", "API_TOKEN: a token for the API ($RUNBOOKSPEC_TEST_TOKEN is not set)",
		"Create it at [Tokens](https://example.com/tokens)", "agent-key: the key, loaded in the SSH agent (ssh-add -l: "} {
		if !strings.Contains(h.out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, h.out.String())
		}
	}
	// A single step checks only its own inputs.
	h.out.Reset()
	if err := r.Run(context.Background(), h.rb, h.log, "1.1"); !errors.As(err, &mi) || len(mi.Inputs) != 1 || mi.Inputs[0].Name != "agent-key" {
		t.Errorf("run 1.1: %v\n%s", err, h.out.String())
	}
}

func TestValidateInputs(t *testing.T) {
	rb := runbookToRun()
	rb.Inputs = []Input{{Name: "A", Description: "a"}, {Name: "A", Description: "again", Env: "A"}, {Name: "B", Env: "B", HowTo: "uses {{nope}}"}}
	rb.Phases[0].Steps[0].Needs = []string{"C"}
	err := rb.Validate(ValidateOptions{})
	for _, want := range []string{"input A: needs an env or a check", "input A: declared twice", "input B: description is required",
		"step 1.1: needs C, which is not a declared input", "input B: references {{nope}}"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("Validate lacks %q: %v", want, err)
		}
	}
}

func TestLoadEnvFileAndTemplate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "launch.env")
	data := "# comment\n\nexport RUNBOOKSPEC_TEST_A=one\nRUNBOOKSPEC_TEST_B=\"two words\"\nRUNBOOKSPEC_TEST_C=from-file\nRUNBOOKSPEC_TEST_D=\n"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RUNBOOKSPEC_TEST_A", "")
	t.Setenv("RUNBOOKSPEC_TEST_B", "")
	t.Setenv("RUNBOOKSPEC_TEST_C", "from-shell")
	set, err := LoadEnvFile(path)
	if err != nil || strings.Join(set, ",") != "RUNBOOKSPEC_TEST_A,RUNBOOKSPEC_TEST_B" {
		t.Fatalf("set %v, %v", set, err)
	}
	if os.Getenv("RUNBOOKSPEC_TEST_B") != "two words" || os.Getenv("RUNBOOKSPEC_TEST_C") != "from-shell" {
		t.Error("a value was not set, or the environment's was replaced")
	}
	if err := os.WriteFile(path, []byte("not a line\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadEnvFile(path); err == nil || !strings.Contains(err.Error(), "line 1") {
		t.Errorf("a bad line: %v", err)
	}

	tpl := string(EnvTemplate("phase 1", []Requirement{
		{Name: "profile", Env: "AWS_PROFILE", Description: "the profile", Example: "ops", Steps: []string{"1.1"}},
		{Name: "TOKEN", Env: "TOKEN", Description: "a token", Secret: true, Steps: []string{"1.2"}},
		{Name: "agent-key", Description: "the key", Steps: []string{"1.1"}},
	}))
	for _, want := range []string{"# profile: the profile (such as ops); steps 1.1\nexport AWS_PROFILE=\n", "# secret: export it in the shell instead", "\n# export TOKEN=\n"} {
		if !strings.Contains(tpl, want) {
			t.Errorf("template lacks %q:\n%s", want, tpl)
		}
	}
	if strings.Contains(tpl, "agent-key") {
		t.Error("the template has a line for an input with no environment variable")
	}
}

func TestRenderBeforeYouStart(t *testing.T) {
	rb, _ := withInputs(runbookToRun(), true)
	md, err := rb.Markdown(RenderOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"## Before You Start\n", "| `profile` | `$RUNBOOKSPEC_TEST_PROFILE` or `--var profile=…` | the profile, such as `ops` | [1.1](#11-stack) |",
		"### API_TOKEN\n", "**Set:** `export RUNBOOKSPEC_TEST_TOKEN=…`, in the shell that runs; it is a secret",
		"**Checked with:** `ssh-add -l`, which must print `SHA256:abc`.", "**Used by:** [1.2](#12-up).", "**Needs:** [API_TOKEN](#api_token)."} {
		if !strings.Contains(string(md), want) {
			t.Errorf("the Markdown lacks %q", want)
		}
	}
	if broken := BrokenLinks("page.md", md); len(broken) > 0 {
		t.Errorf("broken links: %v", broken)
	}
	page, err := rb.HTML(HTMLOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`<h2 id="before-you-start">Before You Start</h2>`, `<section class="input" id="api_token"><h3>API_TOKEN</h3>`,
		`<a href="#api_token">API_TOKEN</a>`, `<span class="label">Checked with:</span> <code>ssh-add -l</code>`} {
		if !strings.Contains(string(page), want) {
			t.Errorf("the HTML lacks %q", want)
		}
	}
}
