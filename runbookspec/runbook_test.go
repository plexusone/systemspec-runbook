package runbookspec

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// sample is a small valid runbook: 0.1 and 0.2 are independent, and 1.1
// needs both.
func sample() *Runbook {
	dns := Check{ID: "ns", Description: "the zone is on Cloudflare", Kind: KindDNS,
		DNS: &DNSCheck{Name: "example.com", Type: "NS", Contains: []string{"ns.cloudflare.com"}}}
	return &Runbook{
		ID: "launch", Title: "Launch", Summary: "Launch it.",
		Phases: []Phase{
			{ID: "0", Title: "Prepare", Gives: "access", Steps: []Step{
				{ID: "0.1", Title: "Sign in", Owner: OwnerOperator, Done: "it works", Checks: []Check{dns}},
				{ID: "0.2", Title: "Build", Owner: OwnerAgent, Done: "it builds",
					Actions: []Action{{Text: "Build it.", Command: "make build"}}},
			}},
			{ID: "1", Title: "Provision", Gives: "a host", Steps: []Step{
				{ID: "1.1", Title: "Up", Owner: OwnerBoth, Done: "it is up", DependsOn: []string{"0.1", "0.2"},
					Credentials: []string{"hosting/prod/ssh-key"},
					Actions:     []Action{{Text: "Read the steps.", Link: &Link{Text: "the plan", Key: "plan:hosting"}}}},
			}},
		},
	}
}

func TestValidateAcceptsSample(t *testing.T) {
	if err := sample().Validate(ValidateOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := sample().Unchecked(); strings.Join(got, ",") != "0.2,1.1" {
		t.Errorf("unchecked = %v", got)
	}
}

func TestValidateReportsEveryProblem(t *testing.T) {
	rb := sample()
	s := &rb.Phases[1].Steps[0]
	s.ID = "2.1"                        // not in phase 1
	s.Owner = "someone"                 // unknown owner
	s.DependsOn = []string{"9.9"}       // unknown dependency
	s.Done = ""                         // required
	s.Actions[0].Link.Href = "plan.md"  // both key and href
	s.Credentials = []string{"unknown"} // not in the registry
	s.Checks = []Check{
		{ID: "a", Description: "x", Kind: KindHTTP, DNS: &DNSCheck{}},                      // wrong parameters
		{ID: "a", Description: "x", Kind: KindNamed, Named: &NamedCheck{Name: "nope"}},     // duplicate, unregistered
		{ID: "b", Description: "x", Kind: KindDNS, DNS: &DNSCheck{Name: "n", Type: "SRV"}}, // bad type, no contains
	}
	err := rb.Validate(ValidateOptions{
		KnownCredential: func(string) bool { return false },
		KnownNamedCheck: func(string) bool { return false },
	})
	if err == nil {
		t.Fatal("Validate passed")
	}
	for _, want := range []string{
		"not in phase 1", "owner \"someone\"", "depends on 9.9", "done is required",
		"exactly one of key and href", "credential unknown", "needs its http parameters",
		"must not set dns parameters", "check a: duplicate id", "nope is not registered",
		"dns type \"SRV\"", "what the answers contain",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("missing %q in:\n%v", want, err)
		}
	}
}

func TestValidateRejectsLaterDependency(t *testing.T) {
	rb := sample()
	rb.Phases[0].Steps[0].DependsOn = []string{"1.1"}
	if err := rb.Validate(ValidateOptions{}); err == nil || !strings.Contains(err.Error(), "not an earlier step") {
		t.Errorf("Validate = %v", err)
	}
}

func at(min int) time.Time { return time.Date(2026, 11, 1, 12, min, 0, 0, time.UTC) }

func TestStatuses(t *testing.T) {
	rb := sample()
	l := &Log{Runbook: rb.ID}
	states := func() string {
		var out []string
		for _, s := range Statuses(rb, l) {
			out = append(out, s.Step+"="+string(s.State))
		}
		return strings.Join(out, " ")
	}
	if got := states(); got != "0.1=ready 0.2=ready 1.1=blocked" {
		t.Fatalf("no events: %s", got)
	}
	steps := []struct {
		e    Event
		want string
	}{
		{Event{Step: "0.1", Type: EventStarted, At: at(1)}, "0.1=in-progress 0.2=ready 1.1=blocked"},
		{Event{Step: "0.1", Type: EventDone, At: at(2)}, "0.1=done 0.2=ready 1.1=blocked"},
		{Event{Step: "0.2", Type: EventVerified, At: at(3)}, "0.1=done 0.2=verified 1.1=ready"},
		{Event{Step: "0.1", Type: EventFailed, At: at(4), Note: "no answer"}, "0.1=in-progress 0.2=verified 1.1=blocked"},
		{Event{Step: "0.1", Type: EventSkipped, At: at(5)}, "0.1=done 0.2=verified 1.1=ready"},
		{Event{Step: "0.2", Type: EventReopened, At: at(6)}, "0.1=done 0.2=ready 1.1=blocked"},
	}
	for _, s := range steps {
		if err := l.Append(rb, s.e); err != nil {
			t.Fatal(err)
		}
		if got := states(); got != s.want {
			t.Errorf("after %s %s: %s, want %s", s.e.Step, s.e.Type, got, s.want)
		}
	}
	if b := Statuses(rb, l)[2].Blockers; strings.Join(b, ",") != "0.2" {
		t.Errorf("blockers = %v", b)
	}
	if err := l.Validate(rb); err != nil {
		t.Fatal(err)
	}
}

func TestAppendRefuses(t *testing.T) {
	rb := sample()
	l := &Log{Runbook: rb.ID}
	for _, c := range []struct {
		e    Event
		want string
	}{
		{Event{Step: "7.7", Type: EventDone, At: at(1)}, "no step 7.7"},
		{Event{Step: "0.1", Type: "finished", At: at(1)}, "is not one of"},
		{Event{Step: "0.1", Type: EventDone}, "needs a time"},
		{Event{Step: "0.1", Type: EventDone, At: at(1), By: "bot"}, "by \"bot\""},
		{Event{Step: "0.1", Type: EventDone, At: at(1), Note: "set to github_pat_11ABC"}, "looks like it holds a secret"},
		{Event{Step: "0.1", Type: EventDone, At: at(1), Note: "-----BEGIN EC PRIVATE KEY-----"}, "looks like it holds a secret"}, //nolint:gosec // G101: a PEM header with no key, to test the refusal
	} {
		if err := l.Append(rb, c.e); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("Append(%+v) = %v, want %q", c.e, err, c.want)
		}
	}
	if len(l.Events) != 0 {
		t.Errorf("refused events were added: %v", l.Events)
	}
}

func TestLogRoundTrip(t *testing.T) {
	rb := sample()
	path := filepath.Join(t.TempDir(), "events.json")
	l, err := LoadLog(path, rb.ID)
	if err != nil {
		t.Fatalf("missing log: %v", err)
	}
	if err := l.Append(rb, Event{Step: "0.1", Type: EventDone, At: at(1).In(time.FixedZone("X", 3600)), By: OwnerOperator}); err != nil {
		t.Fatal(err)
	}
	data, err := l.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	back, err := LoadLog(path, rb.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(back.Events) != 1 || !back.Events[0].At.Equal(at(1)) || back.Events[0].At.Location() != time.UTC {
		t.Errorf("round trip = %+v", back.Events)
	}
	if _, err := LoadLog(path, "other"); err == nil {
		t.Error("a log for another runbook was accepted")
	}
	empty, err := (&Log{Runbook: "x"}).Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(empty), `"events": []`) {
		t.Errorf("empty log = %s", empty)
	}
}

// A note the runner writes from a command's error is redacted, not refused,
// so the failure is still recorded; a long path matches like a token.
func TestRedactKeepsAFailureRecordable(t *testing.T) {
	msg := "action 3, command 2 (/opt/build/example/ProductRepo/ops/host/release.sh) failed: exit status 1; token github_pat_11ABCDEFGHIJKLMNOPQRST"
	got := Redact(msg)
	if strings.Contains(got, "github_pat_") || strings.Contains(got, "ProductRepo/ops/host/release") || !strings.Contains(got, "failed: exit status 1") || !strings.Contains(got, Withheld) {
		t.Errorf("Redact = %q", got)
	}
	rb := &Runbook{ID: "r", Title: "R", Phases: []Phase{{ID: "1", Title: "P", Gives: "g", Steps: []Step{{ID: "1.1", Title: "S", Owner: OwnerOperator, Done: "d"}}}}}
	l := &Log{}
	if err := l.Append(rb, Event{Step: "1.1", Type: EventFailed, At: time.Now(), Note: got}); err != nil {
		t.Errorf("a redacted note was refused: %v", err)
	}
	if err := l.Append(rb, Event{Step: "1.1", Type: EventFailed, At: time.Now(), Note: msg}); err == nil {
		t.Error("an unredacted note was accepted")
	}
}
