package runbookspec

import (
	"bytes"
	"fmt"
	"html"

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

// markdown converts the Markdown page with tables and heading IDs, and
// without raw HTML, so text in a runbook cannot inject markup.
var markdown = goldmark.New(
	goldmark.WithExtensions(extension.GFM),
	goldmark.WithParserOptions(parser.WithAutoHeadingID()),
)

// HTML renders the runbook's Markdown page as HTML: a whole document with
// a style for light and dark themes, or with Fragment its body alone.
func (rb *Runbook) HTML(opts HTMLOptions) ([]byte, error) {
	md, err := rb.Markdown(opts.RenderOptions)
	if err != nil {
		return nil, err
	}
	var body bytes.Buffer
	if err := markdown.Convert(md, &body); err != nil {
		return nil, fmt.Errorf("runbook: rendering HTML: %w", err)
	}
	if opts.Fragment {
		return body.Bytes(), nil
	}
	var page bytes.Buffer
	fmt.Fprintf(&page, `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>%s</title>
<style>
%s
</style>
</head>
<body>
<main>
`, html.EscapeString(rb.Title), Style)
	page.Write(body.Bytes())
	page.WriteString("</main>\n</body>\n</html>\n")
	return page.Bytes(), nil
}

// Style is the HTML page's style sheet, for a host that places the
// fragment in a page of its own.
const Style = `:root { --bg: #ffffff; --fg: #1f2328; --muted: #59636e; --line: #d1d9e0; --code: #f6f8fa; --link: #0969da; }
@media (prefers-color-scheme: dark) {
  :root { --bg: #0d1117; --fg: #e6edf3; --muted: #9198a1; --line: #3d444d; --code: #151b23; --link: #4493f8; }
}
* { box-sizing: border-box; }
body { margin: 0; background: var(--bg); color: var(--fg); font: 16px/1.55 system-ui, -apple-system, "Segoe UI", sans-serif; }
main { max-width: 60rem; margin: 0 auto; padding: 1.5rem 1rem 4rem; }
h1, h2, h3, h4 { line-height: 1.25; margin: 2rem 0 0.75rem; }
h1 { font-size: 1.9rem; margin-top: 0.5rem; }
h2 { font-size: 1.45rem; padding-bottom: 0.3rem; border-bottom: 1px solid var(--line); }
h3 { font-size: 1.15rem; }
a { color: var(--link); }
code { font: 0.88em ui-monospace, SFMono-Regular, Menlo, monospace; background: var(--code); padding: 0.1em 0.35em; border-radius: 4px; }
pre { background: var(--code); border: 1px solid var(--line); border-radius: 6px; padding: 0.75rem 1rem; overflow-x: auto; }
pre code { background: none; padding: 0; }
table { border-collapse: collapse; display: block; overflow-x: auto; margin: 1rem 0; }
th, td { border: 1px solid var(--line); padding: 0.4rem 0.7rem; text-align: left; vertical-align: top; }
th { background: var(--code); }
blockquote { margin: 1rem 0; padding: 0 1rem; color: var(--muted); border-left: 4px solid var(--line); }
li + li { margin-top: 0.25rem; }`
