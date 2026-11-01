package runbookspec_test

import (
	"fmt"
	"time"

	"github.com/plexusone/systemspec-runbook/runbookspec"
)

// A runbook is declared in Go, validated, rendered, and its progress is
// recorded in a log from which each step's state is derived.
func Example() {
	rb := &runbookspec.Runbook{
		ID:      "launch",
		Title:   "Launch",
		Summary: "Put the site on its own domain.",
		Phases: []runbookspec.Phase{{
			ID: "1", Title: "DNS", Gives: "The domain on the new name servers",
			Steps: []runbookspec.Step{
				{
					ID: "1.1", Title: "Move the name servers", Owner: runbookspec.OwnerOperator,
					Actions: []runbookspec.Action{{Text: "At the registrar, set the two name servers the DNS provider gives."}},
					Done:    "both name servers answer for the domain.",
					Checks: []runbookspec.Check{{
						ID: "ns", Description: "the domain's name servers are the provider's", Kind: runbookspec.KindDNS,
						DNS: &runbookspec.DNSCheck{Name: "example.com", Type: "NS", Contains: []string{"ns.example.net"}},
					}},
				},
				{
					ID: "1.2", Title: "Point the site", Owner: runbookspec.OwnerBoth, DependsOn: []string{"1.1"},
					Actions: []runbookspec.Action{{Text: "Add the site's record.", Command: "dnsctl add A www.example.com 192.0.2.10"}},
					Done:    "[[step:1.1]] is done and `www` answers.",
				},
			},
		}},
	}
	if err := rb.Validate(runbookspec.ValidateOptions{}); err != nil {
		fmt.Println(err)
		return
	}

	log := &runbookspec.Log{Runbook: rb.ID}
	if err := log.Append(rb, runbookspec.Event{Step: "1.1", Type: runbookspec.EventDone, At: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), By: runbookspec.OwnerOperator}); err != nil {
		fmt.Println(err)
		return
	}
	for _, s := range runbookspec.Statuses(rb, log) {
		fmt.Println(s.Step, s.State)
	}

	page, err := rb.Markdown(runbookspec.RenderOptions{})
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(len(page) > 0)
	// Output:
	// 1.1 done
	// 1.2 ready
	// true
}
