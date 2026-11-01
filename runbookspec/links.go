package runbookspec

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	mdLink    = regexp.MustCompile(`\]\(([^)\s]+)\)`)
	mdHeading = regexp.MustCompile(`(?m)^#{1,6}\s+(.+?)\s*#*\s*$`)
	codeFence = regexp.MustCompile("(?ms)^\\s*```.*?^\\s*```")
)

// BrokenLinks checks the relative links of a Markdown page at path: each
// target file must exist, and each anchor must be a heading in it, as
// MkDocs names headings. Links with a scheme are not checked. It returns
// one line per broken link. A file that cannot be read counts as broken.
func BrokenLinks(path string, page []byte) []string {
	text := codeFence.ReplaceAllString(string(page), "")
	headings := map[string]map[string]bool{}
	anchors := func(file string, content []byte) map[string]bool {
		if h, ok := headings[file]; ok {
			return h
		}
		h := map[string]bool{}
		body := codeFence.ReplaceAllString(string(content), "")
		for _, m := range mdHeading.FindAllStringSubmatch(body, -1) {
			h[Slug(stripMarkdown(m[1]))] = true
		}
		headings[file] = h
		return h
	}

	var broken []string
	for _, m := range mdLink.FindAllStringSubmatch(text, -1) {
		target := m[1]
		if strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:") {
			continue
		}
		file, anchor, _ := strings.Cut(target, "#")
		var content []byte
		if file == "" {
			file, content = path, page
		} else {
			file = filepath.Join(filepath.Dir(path), file)
			data, err := os.ReadFile(file) //nolint:gosec // G304: a file the page links to
			if err != nil {
				broken = append(broken, fmt.Sprintf("%s: %s does not exist", target, file))
				continue
			}
			content = data
		}
		if anchor != "" && strings.HasSuffix(file, ".md") && !anchors(file, content)[anchor] {
			broken = append(broken, fmt.Sprintf("%s: no heading #%s in %s", target, anchor, file))
		}
	}
	return broken
}

var inlineMarkup = regexp.MustCompile("[`*]|\\[([^\\]]*)\\]\\([^)]*\\)")

// stripMarkdown removes the markup MkDocs drops from a heading's anchor:
// code and emphasis marks, and a link's target. Underscores stay, as they
// do in MkDocs' anchors.
func stripMarkdown(s string) string {
	return inlineMarkup.ReplaceAllStringFunc(s, func(m string) string {
		if strings.HasPrefix(m, "[") {
			return inlineMarkup.FindStringSubmatch(m)[1]
		}
		return ""
	})
}
