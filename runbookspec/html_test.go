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
	if strings.Contains(s, "<script>alert(1)") || !strings.Contains(s, "&lt;script&gt;alert(1)") {
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

func TestHTMLStepsAnchorsAndCommands(t *testing.T) {
	r := &Runbook{
		ID: "setup", Title: "Example", Summary: "See [[step:1.2]] first.",
		Phases: []Phase{{ID: "1", Title: "First", Gives: "a start", Steps: []Step{
			{ID: "1.1", Title: "Make the key", Owner: OwnerOperator, Done: "It exists.",
				Actions: []Action{{Owner: OwnerAgent, Text: "Prepare it.", Exec: []Exec{{Args: []string{"tool", "a & b <c>"}}}}}},
			{ID: "1.2", Title: "Use it", Owner: OwnerBoth, DependsOn: []string{"1.1"}, Done: "It works.",
				Checks: []Check{{ID: "c", Description: "it answers", Kind: KindHTTP, HTTP: &HTTPCheck{URL: "https://example.com", Status: 200}}}},
		}}},
	}
	page, err := r.HTML(HTMLOptions{RenderOptions: RenderOptions{VerifyCommand: "tool verify {step}"}})
	if err != nil {
		t.Fatal(err)
	}
	s := string(page)
	md, err := r.Markdown(RenderOptions{})
	if err != nil {
		t.Fatal(err)
	}
	anchor := StepAnchor(r.Step("1.2"))
	for _, want := range []string{
		`<section class="step" id="` + anchor + `">`, `href="#` + anchor + `"`, // the summary's link and the step agree
		`<span class="badge operator">operator</span>`, `<span class="badge agent">agent</span>`,
		`tool &#39;a &amp; b &lt;c&gt;&#39;`, "tool verify 1.2", "width: 100%",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("the page lacks %q", want)
		}
	}
	if !strings.Contains(string(md), "(#"+anchor+")") {
		t.Errorf("the Markdown page links to another anchor than %s", anchor)
	}
	if _, err := (&Runbook{ID: "x", Title: "x", Summary: "[[step:9.9]]", Phases: r.Phases}).HTML(HTMLOptions{}); err == nil {
		t.Error("a reference to a missing step was rendered")
	}
}

func TestHTMLCopiesCommands(t *testing.T) {
	page, err := htmlRunbook().HTML(HTMLOptions{})
	if err != nil {
		t.Fatal(err)
	}
	s := string(page)
	if !strings.Contains(s, "<script>") || !strings.Contains(s, "navigator.clipboard") || !strings.Contains(s, ".copy {") {
		t.Error("the page has no copy buttons")
	}
	if strings.Index(s, "<script>") < strings.Index(s, "</main>") {
		t.Error("the script runs before the code blocks exist")
	}
	frag, err := htmlRunbook().HTML(HTMLOptions{Fragment: true})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(frag), "<script>") {
		t.Error("a fragment carries the script; its host includes Script")
	}
}

// TestHTMLNewTabLinks checks that each link to another page, in a text
// field or an action's Link, is followed by an icon that opens it in a new
// tab, and that a link within the page gets none.
func TestHTMLNewTabLinks(t *testing.T) {
	rb := htmlRunbook()
	rb.Phases[0].Steps[0].Actions[1].Text = "Open [Branding](https://example.com/auth?a=1&b=2), then [the next step](#11-make-the-key)."
	rb.Phases[0].Steps[0].Actions[0].Link = &Link{Text: "the plan", Href: "plan.md#fleet"}
	page, err := rb.HTML(HTMLOptions{})
	if err != nil {
		t.Fatal(err)
	}
	s := string(page)
	for _, want := range []string{
		`<a href="https://example.com/auth?a=1&amp;b=2">Branding</a><a class="newtab" href="https://example.com/auth?a=1&amp;b=2" target="_blank" rel="noopener noreferrer"`,
		`<a href="plan.md#fleet">the plan</a><a class="newtab" href="plan.md#fleet" target="_blank"`,
		`<a href="#11-make-the-key">the next step</a>.`,
		".newtab svg",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("the page lacks %q", want)
		}
	}
	if n := strings.Count(s, `class="newtab"`); n != 2 {
		t.Errorf("%d new-tab links; want 2, none for the link within the page", n)
	}
}
