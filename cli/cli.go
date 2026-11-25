// Package cli is the runbook command line as Cobra commands: a host tool
// mounts them over its own runbook, its event log, its named checks and the
// providers of its variables.
package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"text/tabwriter"
	"time"

	oscfs "github.com/grokify/oscompat/fs"
	"github.com/spf13/cobra"

	rb "github.com/plexusone/systemspec-runbook/runbookspec"
	"github.com/plexusone/systemspec-runbook/schema"
)

// Options configure the commands.
type Options struct {
	// Use and Short name the parent command, such as "runbook".
	Use, Short string
	// Load returns the runbook, validated (required).
	Load func() (*rb.Runbook, error)
	// EventsPath is the event log; a missing file is an empty log.
	EventsPath string
	// EventsFlag names the flag that sets EventsPath; "events" when empty.
	// A host tool with an events flag of its own names this one otherwise.
	EventsFlag string
	// Render returns the options doc renders with: link resolver,
	// verify command and generator. Nil renders without them.
	Render func() (rb.RenderOptions, error)
	// Sources supply variables after the --var flags and before the
	// environment, such as a stack's outputs.
	Sources []rb.Source
	// Named are the host's registered checks.
	Named map[string]rb.NamedFunc
	// Root is the directory commands' Dir is relative to; "." when empty.
	Root string
}

// New returns the parent command with validate, list, show, record,
// verify, run, restart, state, doc and schema.
func New(o Options) *cobra.Command {
	if o.Use == "" {
		o.Use = "runbook"
	}
	if o.Short == "" {
		o.Short = "List, show, record, verify and run the runbook's steps, and write its page"
	}
	if o.Root == "" {
		o.Root = "."
	}
	c := &commands{o: o}
	root := &cobra.Command{Use: o.Use, Short: o.Short}
	if o.EventsFlag == "" {
		o.EventsFlag = "events"
	}
	root.PersistentFlags().StringVar(&c.o.EventsPath, o.EventsFlag, o.EventsPath, "the runbook's event log (JSON); missing means no events yet")
	root.PersistentFlags().StringVar(&c.o.Root, "root", o.Root, "the directory commands run in, and their dir is relative to")
	root.AddCommand(c.validate(), c.list(), c.show(), c.record(), c.verify(), c.run(), c.restart(), c.state(), c.doc(), c.schema())
	return root
}

type commands struct{ o Options }

func (c *commands) load() (*rb.Runbook, *rb.Log, error) {
	if c.o.Load == nil {
		return nil, nil, errors.New("cli: no runbook to load")
	}
	r, err := c.o.Load()
	if err != nil {
		return nil, nil, err
	}
	l, err := rb.LoadLog(c.o.EventsPath, r.ID)
	if err != nil {
		return nil, nil, err
	}
	if err := l.Validate(r); err != nil {
		return nil, nil, err
	}
	return r, l, nil
}

func (c *commands) values(r *rb.Runbook, flags []string) (*rb.Values, error) {
	given := map[string]string{}
	for _, f := range flags {
		k, v, ok := strings.Cut(f, "=")
		if !ok || k == "" {
			return nil, fmt.Errorf("--var %q: want name=value", f)
		}
		given[k] = v
	}
	v := rb.NewValues(r, append([]rb.Source{rb.MapSource(given)}, c.o.Sources...)...)
	v.Root = c.o.Root
	return v, nil
}

// save writes the log atomically, creating its directory on the first
// event.
func (c *commands) save(l *rb.Log) error {
	if l.Schema == "" {
		l.Schema = schema.EventsID
	}
	data, err := l.Marshal()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(c.o.EventsPath), 0o755); err != nil { //nolint:gosec // G301: the log is committed documentation
		return fmt.Errorf("cli: %w", err)
	}
	return oscfs.WriteFileAtomic(c.o.EventsPath, data, oscfs.DefaultFilePerm)
}

func (c *commands) validate() *cobra.Command {
	return &cobra.Command{
		Use:   "validate",
		Short: "Check the runbook and its event log",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			r, l, err := c.load()
			if err != nil {
				return err
			}
			if err := r.Validate(rb.ValidateOptions{KnownNamedCheck: func(name string) bool { _, ok := c.o.Named[name]; return ok }}); err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "%s: %d steps and %d events OK\n", r.ID, len(r.Steps()), len(l.Events))
			return err
		},
	}
}

func (c *commands) list() *cobra.Command {
	var only string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List every step with its owner, its state and what it waits for",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			r, l, err := c.load()
			if err != nil {
				return err
			}
			return WriteList(cmd.OutOrStdout(), r, rb.Statuses(r, l), rb.State(only))
		},
	}
	cmd.Flags().StringVar(&only, "state", "", "only steps in this state: blocked, ready, in-progress, done or verified")
	return cmd
}

func (c *commands) show() *cobra.Command {
	return &cobra.Command{
		Use:   "show <step>",
		Short: "Show a step: its state, actions, commands, credentials and checks",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, l, err := c.load()
			if err != nil {
				return err
			}
			return WriteStep(cmd.OutOrStdout(), r, rb.Statuses(r, l), args[0])
		},
	}
}

func (c *commands) record() *cobra.Command {
	var note, by string
	cmd := &cobra.Command{
		Use:   "record <step> <started|done|verified|failed|skipped|reopened>",
		Short: "Add an event to the runbook's log",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, l, err := c.load()
			if err != nil {
				return err
			}
			e := rb.Event{Step: args[0], Type: rb.EventType(args[1]), At: time.Now().UTC().Truncate(time.Second), By: rb.Owner(by), Note: note}
			if err := l.Append(r, e); err != nil {
				return err
			}
			if err := c.save(l); err != nil {
				return err
			}
			for _, s := range rb.Statuses(r, l) {
				if s.Step == e.Step {
					_, err = fmt.Fprintf(cmd.OutOrStdout(), "%s: %s, now %s\n", e.Step, e.Type, s.State)
					return err
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&note, "note", "", "context, never a secret")
	cmd.Flags().StringVar(&by, "by", "", "who: operator or agent")
	return cmd
}

func (c *commands) verify() *cobra.Command {
	var all, record bool
	var vars []string
	var by string
	cmd := &cobra.Command{
		Use:   "verify [<step>...]",
		Short: "Run steps' checks, read-only; --record logs verified or failed; exits non-zero when a check fails",
		RunE: func(cmd *cobra.Command, args []string) error {
			r, l, err := c.load()
			if err != nil {
				return err
			}
			if all == (len(args) > 0) {
				return errors.New("verify: name the steps, or --all")
			}
			ids := args
			if all {
				for _, s := range r.Steps() {
					if len(s.Checks) > 0 {
						ids = append(ids, s.ID)
					}
				}
			}
			values, err := c.values(r, vars)
			if err != nil {
				return err
			}
			checker := &rb.Checker{Named: c.o.Named, Values: values}
			w := cmd.OutOrStdout()
			failed := 0
			statuses := rb.Statuses(r, l)
			for _, id := range ids {
				s := r.Step(id)
				if s == nil {
					return fmt.Errorf("verify: no step %s", id)
				}
				results := checker.Step(cmd.Context(), s)
				pass, fail, skip := rb.Summary(results)
				failed += fail
				if _, err := fmt.Fprintf(w, "%s %s: %d pass, %d fail, %d skip\n", s.ID, s.Title, pass, fail, skip); err != nil {
					return err
				}
				for _, res := range results {
					if _, err := fmt.Fprintf(w, "  %-4s %s: %s\n", res.Outcome, res.Description, res.Detail); err != nil {
						return err
					}
				}
				if !record {
					continue
				}
				i := slices.IndexFunc(statuses, func(st rb.Status) bool { return st.Step == id })
				if e, ok := rb.VerifyEvent(statuses[i], results, time.Now().UTC().Truncate(time.Second), rb.Owner(by)); ok {
					if err := l.Append(r, e); err != nil {
						return err
					}
					if err := c.save(l); err != nil {
						return err
					}
				}
			}
			if failed > 0 {
				return fmt.Errorf("verify: %d check(s) failed", failed)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "every step that has checks")
	cmd.Flags().BoolVar(&record, "record", false, "record verified for steps whose checks all pass, and failed for done steps that no longer pass")
	cmd.Flags().StringArrayVar(&vars, "var", nil, "a variable's value, name=value; repeat for each")
	cmd.Flags().StringVar(&by, "by", "agent", "who verified")
	return cmd
}

func (c *commands) run() *cobra.Command {
	var vars []string
	var as string
	var yes, force bool
	cmd := &cobra.Command{
		Use:   "run <step|phase>",
		Short: "Run a step or a whole phase: show each action, confirm and run its commands, wait on a person's steps, record it, then check it",
		Long: "run refuses a step whose dependencies are not done (unless --force), fills every variable before\n" +
			"anything runs, asks before each command (unless --yes), stops at the first failure, and records\n" +
			"started, failed and done in the event log, then runs the step's checks and records verified.\n" +
			"Commands run without a shell. Another owner's actions are confirmed as done, never run.\n\n" +
			"Given a phase, such as 8, it runs the phase's steps in order, skipping those already done or\n" +
			"verified, and reports every missing variable before the first step starts. When a step fails, it\n" +
			"says where: the phase, step, action and command, the end of the command's output, the runbook's\n" +
			"fixes whose patterns match, and the commands that resume the phase or retry the step.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// A failed run reports where it stopped and how to go on;
			// the command's usage would only bury that.
			cmd.SilenceUsage = true
			r, l, err := c.load()
			if err != nil {
				return err
			}
			values, err := c.values(r, vars)
			if err != nil {
				return err
			}
			runner := &rb.Runner{
				Values:  values,
				Checker: &rb.Checker{Named: c.o.Named, Values: values},
				Root:    c.o.Root,
				As:      rb.Owner(as),
				Yes:     yes,
				Force:   force,
				Stdin:   cmd.InOrStdin(),
				Out:     cmd.OutOrStdout(),
				Save:    c.save,
				Command: runCommand(cmd),
			}
			if r.Phase(args[0]) != nil {
				return runner.RunPhase(cmd.Context(), r, l, args[0])
			}
			return runner.Run(cmd.Context(), r, l, args[0])
		},
	}
	cmd.Flags().StringArrayVar(&vars, "var", nil, "a variable's value, name=value; repeat for each")
	cmd.Flags().StringVar(&as, "as", string(rb.OwnerOperator), "who runs: operator, agent or both")
	cmd.Flags().BoolVar(&yes, "yes", false, "run commands without asking before each; a person's steps are still confirmed")
	cmd.Flags().BoolVar(&force, "force", false, "run a step whose dependencies are not done; the log says so")
	return cmd
}

// runCommand is the run command as the person typed it, such as
// "bin/infra runbook run": the program as invoked, then the command's path
// below the root.
func runCommand(cmd *cobra.Command) string {
	parts := strings.Fields(cmd.CommandPath())
	if len(parts) == 0 {
		return "run"
	}
	if len(os.Args) > 0 && os.Args[0] != "" {
		parts[0] = os.Args[0]
	}
	return strings.Join(parts, " ")
}

func (c *commands) doc() *cobra.Command {
	var out string
	var asHTML bool
	cmd := &cobra.Command{
		Use:   "doc",
		Short: "Write the runbook's page, generated from its declaration, as Markdown or with --html as HTML",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			r, err := c.o.Load()
			if err != nil {
				return err
			}
			var opts rb.RenderOptions
			if c.o.Render != nil {
				if opts, err = c.o.Render(); err != nil {
					return err
				}
			}
			var page []byte
			if asHTML {
				page, err = r.HTML(rb.HTMLOptions{RenderOptions: opts})
			} else {
				page, err = r.Markdown(opts)
			}
			if err != nil {
				return err
			}
			return writeOut(cmd.OutOrStdout(), out, page)
		},
	}
	cmd.Flags().StringVarP(&out, "output", "o", "", "file to write; standard output when empty")
	cmd.Flags().BoolVar(&asHTML, "html", false, "write an HTML page instead of Markdown")
	return cmd
}

func (c *commands) schema() *cobra.Command {
	var out string
	var logSchema bool
	cmd := &cobra.Command{
		Use:   "schema",
		Short: "Write the JSON Schema of a runbook, or with --log of its event log",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			data := schema.Runbook()
			if logSchema {
				data = schema.Events()
			}
			return writeOut(cmd.OutOrStdout(), out, data)
		},
	}
	cmd.Flags().StringVarP(&out, "output", "o", "", "file to write; standard output when empty")
	cmd.Flags().BoolVar(&logSchema, "log", false, "the event log's schema")
	return cmd
}

func writeOut(w io.Writer, path string, data []byte) error {
	if path == "" {
		_, err := w.Write(data)
		return err
	}
	return oscfs.WriteFile(path, data, oscfs.DefaultFilePerm)
}

// WriteList writes every step with its state, owner and what it waits for,
// then a count by state.
func WriteList(w io.Writer, r *rb.Runbook, statuses []rb.Status, only rb.State) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(tw, "STEP\tSTATE\tOWNER\tTITLE\tWAITS FOR"); err != nil {
		return err
	}
	counts := map[rb.State]int{}
	for _, st := range statuses {
		counts[st.State]++
		if only != "" && st.State != only {
			continue
		}
		s := r.Step(st.Step)
		title := s.Title
		if s.Gate {
			title += " (gate)"
		}
		if _, err := fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", s.ID, st.State, s.Owner, title, strings.Join(st.Blockers, ", ")); err != nil {
			return err
		}
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	var parts []string
	for _, state := range []rb.State{rb.StateVerified, rb.StateDone, rb.StateInProgress, rb.StateReady, rb.StateBlocked} {
		if counts[state] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", counts[state], state))
		}
	}
	_, err := fmt.Fprintf(w, "%d steps: %s\n", len(statuses), strings.Join(parts, ", "))
	return err
}

// WriteStep writes one step: its state, actions with their commands,
// credentials, when it is done and its checks.
func WriteStep(w io.Writer, r *rb.Runbook, statuses []rb.Status, id string) error {
	s := r.Step(id)
	if s == nil {
		return fmt.Errorf("no step %s in the %s runbook", id, r.ID)
	}
	st := statuses[slices.IndexFunc(statuses, func(st rb.Status) bool { return st.Step == id })]
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s\n\nOwner: %s\nState: %s", s.ID, s.Title, s.Owner, st.State)
	if len(st.Blockers) > 0 {
		fmt.Fprintf(&b, " (waits for %s)", strings.Join(st.Blockers, ", "))
	}
	if st.Last != nil {
		fmt.Fprintf(&b, "\nLast:  %s %s", st.Last.Type, st.Last.At.Format(time.RFC3339))
		if st.Last.Note != "" {
			fmt.Fprintf(&b, ": %s", st.Last.Note)
		}
	}
	if len(s.DependsOn) > 0 {
		fmt.Fprintf(&b, "\nAfter: %s", strings.Join(s.DependsOn, ", "))
	}
	b.WriteString("\n")
	if s.Summary != "" {
		fmt.Fprintf(&b, "\n%s\n", rb.Placeholders(s.Summary))
	}
	runnable := false
	for n, a := range s.Actions {
		owner := ""
		if a.Owner != "" {
			owner = string(a.Owner) + ": "
		}
		fmt.Fprintf(&b, "\n%d. %s%s\n", n+1, owner, rb.Placeholders(a.Text))
		if code := a.Code(); code != "" {
			fmt.Fprintf(&b, "\n    %s\n", strings.ReplaceAll(code, "\n", "\n    "))
		}
		runnable = runnable || len(a.Exec) > 0
	}
	for _, note := range s.Notes {
		fmt.Fprintf(&b, "\n%s\n", rb.Placeholders(note))
	}
	if len(s.Credentials) > 0 {
		fmt.Fprintf(&b, "\nCreates: %s\n", strings.Join(s.Credentials, ", "))
	}
	fmt.Fprintf(&b, "\nDone when %s\n", rb.Placeholders(s.Done))
	if len(s.Checks) > 0 {
		b.WriteString("\nChecks:\n")
		for _, ch := range s.Checks {
			fmt.Fprintf(&b, "- %s\n", ch.Description)
		}
	}
	if runnable {
		fmt.Fprintf(&b, "\nRun it: run %s\n", s.ID)
	} else {
		fmt.Fprintf(&b, "\nRecord it: record %s done\n", s.ID)
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func (c *commands) restart() *cobra.Command {
	var note, by string
	cmd := &cobra.Command{
		Use:   "restart",
		Short: "Begin a new run of a recurring runbook: every step returns to ready or blocked",
		Long: "restart begins a new run of a runbook marked recurring, such as a deploy. The earlier runs'\n" +
			"events stay in the log as history; the steps' states come from the new run.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			r, l, err := c.load()
			if err != nil {
				return err
			}
			at := l.NextRunStart(time.Now().UTC().Truncate(time.Second))
			if err := l.Restart(r, rb.RunStart{At: at, By: rb.Owner(by), Note: note}); err != nil {
				return err
			}
			if err := c.save(l); err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "%s: run %d begun\n", r.ID, len(l.Runs)+1)
			return err
		},
	}
	cmd.Flags().StringVar(&note, "note", "", "context, such as the release being deployed; never a secret")
	cmd.Flags().StringVar(&by, "by", "", "who: operator or agent")
	return cmd
}

func (c *commands) state() *cobra.Command {
	var format string
	cmd := &cobra.Command{
		Use:   "state",
		Short: "Show the runbook's state: its current run, every run before it, and when it last finished",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			r, l, err := c.load()
			if err != nil {
				return err
			}
			st := rb.StateOf(r, l)
			switch format {
			case "json":
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(st)
			case "text":
				return WriteState(cmd.OutOrStdout(), st)
			default:
				return fmt.Errorf("--format %q is not text or json", format)
			}
		},
	}
	cmd.Flags().StringVar(&format, "format", "text", "text or json")
	return cmd
}

// WriteState writes a runbook's state as text: the current run, then each
// earlier one.
func WriteState(w io.Writer, st rb.RunbookState) error {
	when := func(t *time.Time) string {
		if t == nil {
			return "-"
		}
		return t.Format(time.RFC3339)
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "%s\t%d steps\t", st.Runbook, st.Steps) //nolint:errcheck // tabwriter's error is Flush's
	if st.Recurring {
		fmt.Fprintf(tw, "recurring, %d run(s)\tlast finished %s\n", len(st.Runs), when(st.LastFinished)) //nolint:errcheck // as above
	} else {
		fmt.Fprintf(tw, "run once\tfinished %s\n", when(st.LastFinished)) //nolint:errcheck // as above
	}
	fmt.Fprintln(tw, "RUN\tOUTCOME\tSTARTED\tLAST EVENT\tFINISHED\tSTEPS DONE\tNOTE") //nolint:errcheck // as above
	for i := len(st.Runs) - 1; i >= 0; i-- {
		r := st.Runs[i]
		done := r.States[rb.StateDone] + r.States[rb.StateVerified]
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%d/%d\t%s\n", r.N, r.Outcome, when(r.Started), when(r.LastEvent), when(r.Finished), done, st.Steps, r.Note) //nolint:errcheck // as above
	}
	return tw.Flush()
}
