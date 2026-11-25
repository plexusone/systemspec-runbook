package runbookspec

import "regexp"

// Fixes returns the fixes whose patterns match text: the step's first, then
// the runbook's, each once. A pattern that does not compile matches
// nothing; Validate reports it.
func (rb *Runbook) Fixes(s *Step, text string) []Fix {
	var out []Fix
	seen := map[string]bool{}
	add := func(fixes []Fix) {
		for _, f := range fixes {
			re, err := regexp.Compile(f.Match)
			if err != nil || seen[f.Fix] || !re.MatchString(text) {
				continue
			}
			seen[f.Fix] = true
			out = append(out, f)
		}
	}
	if s != nil {
		add(s.Troubleshoot)
	}
	add(rb.Troubleshoot)
	return out
}
