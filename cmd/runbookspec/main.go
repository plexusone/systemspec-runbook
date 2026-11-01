// Command runbookspec validates, lists, shows, records, verifies and runs
// the steps of a runbook kept as JSON, and writes its page.
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/plexusone/systemspec-runbook/cli"
	"github.com/plexusone/systemspec-runbook/runbookspec"
)

func main() {
	var path string
	root := cli.New(cli.Options{
		Use:        "runbookspec",
		Short:      "Validate, list, show, record, verify and run the steps of a runbook kept as JSON",
		EventsPath: "events.json",
		Load: func() (*runbookspec.Runbook, error) {
			data, err := os.ReadFile(path) //nolint:gosec // G304: the runbook the operator names
			if err != nil {
				return nil, err
			}
			var r runbookspec.Runbook
			if err := json.Unmarshal(data, &r); err != nil {
				return nil, fmt.Errorf("%s: %w", path, err)
			}
			if err := r.Validate(runbookspec.ValidateOptions{}); err != nil {
				return nil, fmt.Errorf("%s: %w", path, err)
			}
			return &r, nil
		},
	})
	root.PersistentFlags().StringVar(&path, "runbook", "runbook.json", "the runbook (JSON)")
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}
