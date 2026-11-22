package runbookspec

import (
	"strings"
	"testing"
)

func htmlRunbook() *Runbook {
	return &Runbook{
		ID: "setup", Title: "Example <setup>", Summary: "Set up `example`. <script>alert(1)</script>",
		Phases: []Phase{{ID: "1", Title: "First", Gives: "a start", Steps: []Step{{
			ID: "1.1", Title: "Make the key", Owner: OwnerOperator, Done: "The key exists.",
			Actions: []Action{{Text: "Run it:", Exec: []Exec{{Args: []string{"example", "make"}}}}, {Text: "Paste the <client ID> into `--id <id>`; see <https://example.com/docs>."}},
		}}}},
	}
}

func TestHTML(t *testing.T) {
	page, err := htmlRunbook().HTML(HTMLOptions{})
	if err != nil {
		t.Fatal(err)
	}
	s := string(page)
	for _, want := range []string{"Paste the &lt;client ID&gt;", "<code>--id &lt;id&gt;</code>", `<a href="https://example.com/docs">`, "<!doctype html>", "<title>Example &lt;setup&gt;</title>", "<code>example</code>", "example make", "prefers-color-scheme: dark", "<table>"} {
		if !strings.Contains(s, want) {
			t.Errorf("the page lacks %q", want)
		}
	}
	if strings.Contains(s, "<script>") {
		t.Error("raw HTML in the runbook's text reached the page")
	}
	frag, err := htmlRunbook().HTML(HTMLOptions{Fragment: true})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(frag), "<!doctype") || !strings.Contains(string(frag), "Make the key") {
		t.Errorf("fragment:\n%s", frag)
	}
}
