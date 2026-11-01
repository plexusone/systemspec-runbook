// Command gen writes the runbook's and the event log's JSON Schemas into
// the schema package. Run it with go generate from the schema directory.
package main

import (
	"fmt"
	"os"

	oscfs "github.com/grokify/oscompat/fs"

	"github.com/plexusone/systemspec-runbook/schema"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "gen:", err)
		os.Exit(1)
	}
}

func run() error {
	runbook, events, err := schema.Generate("..")
	if err != nil {
		return err
	}
	// Schemas are committed and read.
	if err := oscfs.WriteFile(schema.RunbookFile, runbook, oscfs.DefaultFilePerm); err != nil {
		return err
	}
	return oscfs.WriteFile(schema.EventsFile, events, oscfs.DefaultFilePerm)
}
