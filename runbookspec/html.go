package runbookspec

//go:generate go run github.com/a-h/templ/cmd/templ@v0.3.1118 generate -f page.templ

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/a-h/templ"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
)

// HTMLOptions configure the HTML page.
type HTMLOptions struct {
	RenderOptions
	// Fragment returns the page's body alone, for a host to place in a
	// page of its own, instead of a whole document with its style.
	Fragment bool
}

// markdown converts the runbook's Markdown text fields with tables and
// heading IDs, and without raw HTML, so text in a runbook cannot inject
// markup.
var markdown = goldmark.New(
	goldmark.WithExtensions(extension.GFM),
	goldmark.WithParserOptions(parser.WithAutoHeadingID()),
)

// HTML renders the runbook as an HTML page, built from the runbook with
// templ components: a whole document styled for light and dark themes,
// full width, or with Fragment its body alone. Text fields, which are
// Markdown, are converted without raw HTML; commands are escaped. Step
// anchors are the Markdown page's, so links work on both.
func (rb *Runbook) HTML(opts HTMLOptions) ([]byte, error) {
	v, err := rb.view(opts.RenderOptions)
	if err != nil {
		return nil, err
	}
	var c templ.Component = pageDocument(v)
	if opts.Fragment {
		c = pageBody(v)
	}
	var b bytes.Buffer
	if err := c.Render(context.Background(), &b); err != nil {
		return nil, fmt.Errorf("runbook: rendering HTML: %w", err)
	}
	return b.Bytes(), nil
}

// pageView is what the templ components render.
type pageView struct {
	Title, Generator string
	Summary, Usage   templ.Component
	Phases           []phaseView
	Later            []laterView
}

type phaseView struct {
	Heading, Anchor string
	Gives, Summary  templ.Component
	Steps           []stepView
}

type stepView struct {
	Heading, Anchor, Owner string
	After                  []linkView
	Summary, Done          templ.Component
	Actions                []actionView
	Notes                  []templ.Component
	Credentials            []string
	Checks                 []templ.Component
	VerifyCommand          string
	Gate                   bool
}

type actionView struct {
	Owner string
	Text  templ.Component
	Link  *linkView
	Code  string
}

type linkView struct{ Text, Href string }

type laterView struct {
	What  string
	Needs templ.Component
}

// view builds the page's view, expanding references as the Markdown page
// does and converting each text field.
func (rb *Runbook) view(opts RenderOptions) (pageView, error) {
	r := &renderer{rb: rb, opts: opts}
	md := func(s string) templ.Component { return r.html(r.text(strings.TrimSpace(s))) }
	v := pageView{Title: rb.Title, Generator: opts.Generator, Summary: md(rb.Summary)}
	if rb.Usage != "" {
		v.Usage = md(rb.Usage)
	}
	for _, ph := range rb.Phases {
		pv := phaseView{Heading: phaseHeading(ph), Anchor: Slug(phaseHeading(ph)), Gives: md(ph.Gives)}
		if ph.Summary != "" {
			pv.Summary = md(ph.Summary)
		}
		for i := range ph.Steps {
			s := &ph.Steps[i]
			sv := stepView{Heading: stepHeading(s), Anchor: StepAnchor(s), Owner: ownerText(s.Owner), Done: md(s.Done),
				Credentials: s.Credentials, Gate: s.Gate}
			for _, d := range s.DependsOn {
				if dep := rb.Step(d); dep != nil {
					sv.After = append(sv.After, linkView{Text: d, Href: "#" + StepAnchor(dep)})
				} else {
					r.errs = append(r.errs, fmt.Errorf("runbook: step %s depends on %s, which does not exist", s.ID, d))
				}
			}
			if s.Summary != "" {
				sv.Summary = md(s.Summary)
			}
			for _, a := range s.Actions {
				av := actionView{Text: md(a.Text), Code: a.Code()}
				if a.Owner != "" {
					av.Owner = ownerText(a.Owner)
				}
				if a.Link != nil {
					href := a.Link.Href
					if a.Link.Key != "" {
						var err error
						if href, err = r.resolve(a.Link.Key); err != nil {
							r.errs = append(r.errs, err)
						}
					}
					av.Link = &linkView{Text: a.Link.Text, Href: href}
				}
				sv.Actions = append(sv.Actions, av)
			}
			for _, n := range s.Notes {
				sv.Notes = append(sv.Notes, md(n))
			}
			for _, c := range s.Checks {
				sv.Checks = append(sv.Checks, md(c.Description))
			}
			if opts.VerifyCommand != "" && len(s.Checks) > 0 {
				sv.VerifyCommand = strings.ReplaceAll(opts.VerifyCommand, "{step}", s.ID)
			}
			pv.Steps = append(pv.Steps, sv)
		}
		v.Phases = append(v.Phases, pv)
	}
	for _, l := range rb.Later {
		v.Later = append(v.Later, laterView{What: l.What, Needs: md(l.Needs)})
	}
	return v, errors.Join(r.errs...)
}

// html converts Markdown text, never passing raw HTML through, to a
// component that writes it.
func (r *renderer) html(text string) templ.Component {
	var b bytes.Buffer
	if err := markdown.Convert(escapeAngles([]byte(text)), &b); err != nil {
		r.errs = append(r.errs, fmt.Errorf("runbook: rendering HTML: %w", err))
	}
	// goldmark writes no raw HTML from the text, so its output is safe to
	// place as it is.
	return templ.Raw(b.String())
}

// escapeAngles writes < outside code as &lt;, so a placeholder such as
// <value> in a step's text is shown rather than dropped as raw HTML. Code
// spans and fenced or indented code blocks are left as they are, as are
// autolinks such as <https://example.com>.
func escapeAngles(md []byte) []byte {
	lines := strings.Split(string(md), "\n")
	fenced := false
	for i, l := range lines {
		trimmed := strings.TrimSpace(l)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			fenced = !fenced
			continue
		}
		if fenced || strings.HasPrefix(l, "    ") || strings.HasPrefix(l, "\t") {
			continue
		}
		parts := strings.Split(l, "`")
		for j := 0; j < len(parts); j += 2 {
			parts[j] = escapeText(parts[j])
		}
		lines[i] = strings.Join(parts, "`")
	}
	return []byte(strings.Join(lines, "\n"))
}

func escapeText(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '<' && !strings.HasPrefix(s[i+1:], "http://") && !strings.HasPrefix(s[i+1:], "https://") {
			b.WriteString("&lt;")
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// Style is the HTML page's style sheet, for a host that places the
// fragment in a page of its own. The page uses the window's full width.
const Style = `:root { --bg: #ffffff; --fg: #1f2328; --muted: #59636e; --line: #d1d9e0; --code: #f6f8fa; --link: #0969da;
  --operator: #8250df; --agent: #1a7f37; --both: #9a6700; --card: #ffffff; }
@media (prefers-color-scheme: dark) {
  :root { --bg: #0d1117; --fg: #e6edf3; --muted: #9198a1; --line: #3d444d; --code: #151b23; --link: #4493f8;
    --operator: #ab7df8; --agent: #3fb950; --both: #d29922; --card: #0d1117; }
}
* { box-sizing: border-box; }
body { margin: 0; background: var(--bg); color: var(--fg); font: 16px/1.55 system-ui, -apple-system, "Segoe UI", sans-serif; }
main { width: 100%; padding: 1.5rem clamp(1rem, 3vw, 3rem) 4rem; }
h1, h2, h3, h4 { line-height: 1.25; margin: 2rem 0 0.75rem; }
h1 { font-size: 1.9rem; margin-top: 0.5rem; }
h2 { font-size: 1.45rem; padding-bottom: 0.3rem; border-bottom: 1px solid var(--line); }
h3 { font-size: 1.15rem; margin: 0 0 0.5rem; }
a { color: var(--link); }
code { font: 0.88em ui-monospace, SFMono-Regular, Menlo, monospace; background: var(--code); padding: 0.1em 0.35em; border-radius: 4px; }
pre { background: var(--code); border: 1px solid var(--line); border-radius: 6px; padding: 0.75rem 1rem; overflow-x: auto; margin: 0.5rem 0; }
pre code { background: none; padding: 0; }
table { border-collapse: collapse; width: 100%; margin: 1rem 0; }
th, td { border: 1px solid var(--line); padding: 0.4rem 0.7rem; text-align: left; vertical-align: top; }
th { background: var(--code); }
td code { word-break: break-word; }
blockquote { margin: 1rem 0; padding: 0 1rem; color: var(--muted); border-left: 4px solid var(--line); }
li + li { margin-top: 0.25rem; }
.step { border: 1px solid var(--line); border-radius: 8px; padding: 1rem 1.25rem; margin: 1rem 0; background: var(--card); }
.meta { color: var(--muted); font-size: 0.92rem; margin-bottom: 0.5rem; }
.badge { display: inline-block; font-size: 0.78rem; font-weight: 600; padding: 0.05rem 0.5rem; border-radius: 999px; border: 1px solid currentColor; margin-right: 0.4rem; }
.badge.operator { color: var(--operator); } .badge.agent { color: var(--agent); } .badge.both { color: var(--both); }
.actions { padding-left: 1.4rem; }
.actions > li > p:first-of-type { display: inline; }
.label { font-weight: 600; }`
