package runbookspec

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

// Outcome is what a check found.
type Outcome string

// The outcomes.
const (
	// Pass: what the check looks for is so.
	Pass Outcome = "pass"
	// Fail: it is not; the detail says what the check saw.
	Fail Outcome = "fail"
	// Skip: the check could not run; the detail says why. A skip is never
	// a pass.
	Skip Outcome = "skip"
)

// Result is one check's outcome.
type Result struct {
	// Step and Check name it.
	Step, Check string
	// Description is the check's.
	Description string
	// Outcome is what it found.
	Outcome Outcome
	// Detail says what it saw, or why it skipped.
	Detail string
}

// ErrSkip, returned or wrapped by a named check, makes its outcome a skip:
// for example when the credentials it needs are not available.
var ErrSkip = errors.New("skipped")

// NamedFunc is a check registered in Go. It returns nil when what it looks
// for is so, an error wrapping ErrSkip when it cannot tell, and any other
// error, saying what it saw, when it is not so.
type NamedFunc func(ctx context.Context, args map[string]string) (detail string, err error)

// DNSResolver is the part of *net.Resolver the dns check uses.
type DNSResolver interface {
	LookupHost(ctx context.Context, host string) ([]string, error)
	LookupCNAME(ctx context.Context, host string) (string, error)
	LookupMX(ctx context.Context, name string) ([]*net.MX, error)
	LookupNS(ctx context.Context, name string) ([]*net.NS, error)
	LookupTXT(ctx context.Context, name string) ([]string, error)
}

// Checker runs checks. Its zero value runs the generic checks with the
// system's resolver, a 10-second HTTP client that does not follow
// redirects, and no named checks.
type Checker struct {
	// Named are the registered checks, by name.
	Named map[string]NamedFunc
	// Values fill the checks' variables; nil leaves any variable unfilled,
	// so a check that uses one skips.
	Values *Values
	// Resolver answers dns checks; nil uses net.DefaultResolver.
	Resolver DNSResolver
	// HTTP makes http checks; nil uses a client that does not follow
	// redirects, with a 10-second timeout.
	HTTP *http.Client
	// Dial makes unreachable checks' connections; nil uses net.Dialer.
	Dial func(ctx context.Context, network, address string) (net.Conn, error)
	// Command runs command checks and returns standard output; nil runs
	// the program with os/exec, without a shell.
	Command func(ctx context.Context, args []string) (stdout string, err error)
}

// Has reports whether a named check is registered, for validation.
func (c *Checker) Has(name string) bool { _, ok := c.Named[name]; return ok }

// Step runs the step's checks, in order.
func (c *Checker) Step(ctx context.Context, s *Step) []Result {
	out := make([]Result, 0, len(s.Checks))
	for _, ch := range s.Checks {
		r := c.Check(ctx, ch)
		r.Step = s.ID
		out = append(out, r)
	}
	return out
}

// Check runs one check.
func (c *Checker) Check(ctx context.Context, ch Check) Result {
	r := Result{Check: ch.ID, Description: ch.Description}
	vars, err := c.expand(ctx, ch.strings())
	if err != nil {
		r.Outcome, r.Detail = Skip, err.Error()
		return r
	}
	get := func(s string) string { return vars[s] }

	var detail string
	switch ch.Kind {
	case KindDNS:
		detail, err = c.dns(ctx, ch.DNS, get)
	case KindHTTP:
		detail, err = c.http(ctx, ch.HTTP, get)
	case KindUnreachable:
		detail, err = c.unreachable(ctx, ch.Unreachable, get)
	case KindCommand:
		detail, err = c.command(ctx, ch.Command, get)
	case KindNamed:
		fn, ok := c.Named[ch.Named.Name]
		if !ok {
			r.Outcome, r.Detail = Skip, "no check is registered as "+ch.Named.Name
			return r
		}
		args := make(map[string]string, len(ch.Named.Args))
		for k, v := range ch.Named.Args {
			args[k] = get(v)
		}
		detail, err = fn(ctx, args)
	default:
		r.Outcome, r.Detail = Skip, fmt.Sprintf("unknown kind %q", ch.Kind)
		return r
	}
	switch {
	case err == nil:
		r.Outcome, r.Detail = Pass, detail
	case errors.Is(err, ErrSkip):
		r.Outcome, r.Detail = Skip, err.Error()
	default:
		r.Outcome, r.Detail = Fail, err.Error()
	}
	return r
}

// expand fills the variables of the check's strings, keyed by the
// original string.
func (c *Checker) expand(ctx context.Context, in []string) (map[string]string, error) {
	out := make(map[string]string, len(in))
	for _, s := range in {
		if len(References(s)) == 0 {
			out[s] = s
			continue
		}
		if c.Values == nil {
			return nil, &UnfilledError{Names: References(s)}
		}
		v, err := c.Values.Expand(ctx, s)
		if err != nil {
			return nil, err
		}
		out[s] = v
	}
	return out, nil
}

func (c *Checker) dns(ctx context.Context, d *DNSCheck, get func(string) string) (string, error) {
	res := c.Resolver
	if res == nil {
		res = net.DefaultResolver
	}
	name := get(d.Name)
	var answers []string
	var err error
	switch strings.ToUpper(d.Type) {
	case "A", "AAAA":
		answers, err = res.LookupHost(ctx, name)
	case "CNAME":
		var cname string
		cname, err = res.LookupCNAME(ctx, name)
		answers = []string{cname}
	case "MX":
		var mx []*net.MX
		mx, err = res.LookupMX(ctx, name)
		for _, m := range mx {
			answers = append(answers, m.Host)
		}
	case "NS":
		var ns []*net.NS
		ns, err = res.LookupNS(ctx, name)
		for _, n := range ns {
			answers = append(answers, n.Host)
		}
	case "TXT":
		answers, err = res.LookupTXT(ctx, name)
	default:
		return "", fmt.Errorf("%w: dns type %s is not supported", ErrSkip, d.Type)
	}
	if err != nil {
		return "", fmt.Errorf("%s %s: %w", d.Type, name, err)
	}
	for _, want := range d.Contains {
		w := strings.ToLower(get(want))
		found := false
		for _, a := range answers {
			if strings.Contains(strings.ToLower(a), w) {
				found = true
				break
			}
		}
		if !found {
			return "", fmt.Errorf("%s %s: no answer contains %q; answers: %s", d.Type, name, w, strings.Join(answers, ", "))
		}
	}
	return fmt.Sprintf("%s %s: %s", d.Type, name, strings.Join(answers, ", ")), nil
}

func (c *Checker) http(ctx context.Context, h *HTTPCheck, get func(string) string) (string, error) {
	client := c.HTTP
	if client == nil {
		client = &http.Client{
			Timeout:       10 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		}
	}
	method := h.Method
	if method == "" {
		method = http.MethodGet
	}
	url := get(h.URL)
	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrSkip, err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("%s %s: %w", method, url, err)
	}
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err := resp.Body.Close(); err != nil && readErr == nil {
		readErr = err
	}
	if readErr != nil {
		return "", fmt.Errorf("%s %s: reading the body: %w", method, url, readErr)
	}
	if resp.StatusCode != h.Status {
		return "", fmt.Errorf("%s %s: status %d, want %d", method, url, resp.StatusCode, h.Status)
	}
	if want := get(h.Location); want != "" && !strings.HasPrefix(resp.Header.Get("Location"), want) {
		return "", fmt.Errorf("%s %s: redirects to %q, want %q", method, url, resp.Header.Get("Location"), want)
	}
	if want := get(h.Contains); want != "" && !bytes.Contains(body, []byte(want)) {
		return "", fmt.Errorf("%s %s: the body does not contain %q", method, url, want)
	}
	return fmt.Sprintf("%s %s: %d", method, url, resp.StatusCode), nil
}

func (c *Checker) unreachable(ctx context.Context, u *UnreachableCheck, get func(string) string) (string, error) {
	timeout := 10 * time.Second
	if t := get(u.Timeout); t != "" {
		d, err := time.ParseDuration(t)
		if err != nil {
			return "", fmt.Errorf("%w: timeout %q: %v", ErrSkip, t, err)
		}
		timeout = d
	}
	dial := c.Dial
	if dial == nil {
		dial = (&net.Dialer{}).DialContext
	}
	addr := get(u.Address)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	conn, err := dial(ctx, "tcp", addr)
	if err != nil {
		return fmt.Sprintf("%s: no connection within %s", addr, timeout), nil
	}
	if cerr := conn.Close(); cerr != nil {
		return "", fmt.Errorf("%s: connected, and closing failed: %w", addr, cerr)
	}
	return "", fmt.Errorf("%s: connected; it must not be reachable", addr)
}

func (c *Checker) command(ctx context.Context, cm *CommandCheck, get func(string) string) (string, error) {
	args := make([]string, len(cm.Args))
	for i, a := range cm.Args {
		args[i] = get(a)
	}
	run := c.Command
	if run == nil {
		run = runOutput
	}
	out, err := run(ctx, args)
	if err != nil {
		var nf *exec.Error
		if errors.As(err, &nf) {
			return "", fmt.Errorf("%w: %v", ErrSkip, err)
		}
		return "", fmt.Errorf("%s: %w", args[0], err)
	}
	if want := get(cm.Contains); want != "" && !strings.Contains(out, want) {
		return "", fmt.Errorf("%s: output does not contain %q", args[0], want)
	}
	return args[0] + ": exit 0", nil
}

// runOutput runs a program without a shell and returns its standard output.
func runOutput(ctx context.Context, args []string) (string, error) {
	cmd := exec.CommandContext(ctx, args[0], args[1:]...) //nolint:gosec // G204: the runbook's own declared command
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return stdout.String(), fmt.Errorf("%w: %s", err, firstLine(msg))
		}
		return stdout.String(), err
	}
	return stdout.String(), nil
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
