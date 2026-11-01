package runbookspec

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakeDNS struct{}

func (fakeDNS) LookupHost(_ context.Context, h string) ([]string, error) {
	if h == "example.com" {
		return []string{"192.0.2.10"}, nil
	}
	return nil, &net.DNSError{Err: "no such host", Name: h, IsNotFound: true}
}
func (fakeDNS) LookupCNAME(context.Context, string) (string, error) { return "app.example.net.", nil }
func (fakeDNS) LookupMX(context.Context, string) ([]*net.MX, error) {
	return []*net.MX{{Host: ".", Pref: 0}}, nil
}
func (fakeDNS) LookupNS(context.Context, string) ([]*net.NS, error) {
	return []*net.NS{{Host: "ada.ns.cloudflare.com."}, {Host: "bob.ns.cloudflare.com."}}, nil
}
func (fakeDNS) LookupTXT(context.Context, string) ([]string, error) {
	return []string{"v=spf1 -all"}, nil
}

func dnsCheck(typ, name string, contains ...string) Check {
	return Check{ID: "d", Description: "dns", Kind: KindDNS, DNS: &DNSCheck{Name: name, Type: typ, Contains: contains}}
}

func TestDNSChecks(t *testing.T) {
	c := &Checker{Resolver: fakeDNS{}}
	ctx := context.Background()
	for _, tc := range []struct {
		ch   Check
		want Outcome
	}{
		{dnsCheck("NS", "example.com", "ns.cloudflare.com"), Pass},
		{dnsCheck("NS", "example.com", "domaincontrol.com"), Fail},
		{dnsCheck("A", "example.com", "192.0.2.10"), Pass},
		{dnsCheck("A", "missing.example", "192.0.2.10"), Fail},
		{dnsCheck("TXT", "example.com", "v=spf1 -all"), Pass},
		{dnsCheck("MX", "example.com", "."), Pass},
		{dnsCheck("CNAME", "www.example.com", "app.example.net"), Pass},
		{dnsCheck("SRV", "example.com", "x"), Skip},
	} {
		if r := c.Check(ctx, tc.ch); r.Outcome != tc.want {
			t.Errorf("%s %s %v: %s (%s), want %s", tc.ch.DNS.Type, tc.ch.DNS.Name, tc.ch.DNS.Contains, r.Outcome, r.Detail, tc.want)
		}
	}
}

func TestHTTPCheck(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/readyz":
			fmt.Fprint(w, `{"status":"ready"}`)
		case "/old":
			http.Redirect(w, r, "https://app.example.com/", http.StatusMovedPermanently)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := &Checker{}
	ctx := context.Background()
	for _, tc := range []struct {
		h    HTTPCheck
		want Outcome
	}{
		{HTTPCheck{URL: srv.URL + "/readyz", Status: 200, Contains: "ready"}, Pass},
		{HTTPCheck{URL: srv.URL + "/readyz", Status: 200, Contains: "absent"}, Fail},
		{HTTPCheck{URL: srv.URL + "/missing", Status: 200}, Fail},
		{HTTPCheck{URL: srv.URL + "/old", Status: 301, Location: "https://app.example.com/"}, Pass},
		{HTTPCheck{URL: srv.URL + "/old", Status: 301, Location: "https://elsewhere/"}, Fail},
	} {
		h := tc.h
		if r := c.Check(ctx, Check{ID: "h", Description: "http", Kind: KindHTTP, HTTP: &h}); r.Outcome != tc.want {
			t.Errorf("%s: %s (%s), want %s", h.URL, r.Outcome, r.Detail, tc.want)
		}
	}
}

func TestUnreachableCheck(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	open := ln.Addr().String()
	defer ln.Close() //nolint:errcheck // the test's own listener
	c := &Checker{}
	ctx := context.Background()
	if r := c.Check(ctx, Check{ID: "u", Description: "u", Kind: KindUnreachable, Unreachable: &UnreachableCheck{Address: open, Timeout: "2s"}}); r.Outcome != Fail {
		t.Errorf("an open port: %s (%s)", r.Outcome, r.Detail)
	}
	c.Dial = func(context.Context, string, string) (net.Conn, error) { return nil, errors.New("i/o timeout") }
	if r := c.Check(ctx, Check{ID: "u", Description: "u", Kind: KindUnreachable, Unreachable: &UnreachableCheck{Address: "192.0.2.10:443"}}); r.Outcome != Pass {
		t.Errorf("a closed address: %s (%s)", r.Outcome, r.Detail)
	}
	if r := c.Check(ctx, Check{ID: "u", Description: "u", Kind: KindUnreachable, Unreachable: &UnreachableCheck{Address: "x:1", Timeout: "soon"}}); r.Outcome != Skip {
		t.Errorf("a bad timeout: %s", r.Outcome)
	}
}

func TestCommandNamedAndVariableChecks(t *testing.T) {
	ctx := context.Background()
	c := &Checker{
		Command: func(_ context.Context, args []string) (string, error) {
			if args[0] == "ok" {
				return "cloud-init status: done\n", nil
			}
			return "", errors.New("exit status 1")
		},
		Named: map[string]NamedFunc{
			"aws.bucket": func(_ context.Context, a map[string]string) (string, error) {
				switch a["bucket"] {
				case "state":
					return "bucket state: ok", nil
				case "":
					return "", fmt.Errorf("%w: no credentials", ErrSkip)
				}
				return "", errors.New("no bucket " + a["bucket"])
			},
		},
	}
	cmd := func(prog, contains string) Check {
		return Check{ID: "c", Description: "c", Kind: KindCommand, Command: &CommandCheck{Args: []string{prog}, Contains: contains}}
	}
	named := func(bucket string) Check {
		return Check{ID: "n", Description: "n", Kind: KindNamed, Named: &NamedCheck{Name: "aws.bucket", Args: map[string]string{"bucket": bucket}}}
	}
	for _, tc := range []struct {
		ch   Check
		want Outcome
	}{
		{cmd("ok", "done"), Pass},
		{cmd("ok", "error"), Fail},
		{cmd("fails", ""), Fail},
		{named("state"), Pass},
		{named("other"), Fail},
		{named(""), Skip},
		{Check{ID: "x", Description: "x", Kind: KindNamed, Named: &NamedCheck{Name: "unregistered"}}, Skip},
		{named("{{bucket}}"), Skip}, // no values: the variable is unfilled
	} {
		if r := c.Check(ctx, tc.ch); r.Outcome != tc.want {
			t.Errorf("%+v: %s (%s), want %s", tc.ch, r.Outcome, r.Detail, tc.want)
		}
	}
	rb := &Runbook{Variables: []Variable{{Name: "bucket", Description: "b"}}}
	c.Values = NewValues(rb, MapSource(map[string]string{"bucket": "state"}))
	if r := c.Check(ctx, named("{{bucket}}")); r.Outcome != Pass {
		t.Errorf("with the variable filled: %s (%s)", r.Outcome, r.Detail)
	}
	if !c.Has("aws.bucket") || c.Has("nope") {
		t.Error("Has")
	}
	// A program that does not exist cannot tell: skip.
	real := &Checker{}
	if r := real.Check(ctx, cmd("no-such-program-for-runbook-tests", "")); r.Outcome != Skip {
		t.Errorf("a missing program: %s (%s)", r.Outcome, r.Detail)
	}
}

func TestVerifyEvent(t *testing.T) {
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	pass := Result{Check: "a", Outcome: Pass}
	fail := Result{Check: "b", Outcome: Fail}
	skip := Result{Check: "c", Outcome: Skip}
	done := Status{Step: "1.1", State: StateDone}
	ready := Status{Step: "1.1", State: StateReady}
	for _, tc := range []struct {
		st      Status
		results []Result
		want    EventType
	}{
		{ready, []Result{pass, pass}, EventVerified},
		{done, []Result{pass, fail}, EventFailed},
		{ready, []Result{pass, fail}, ""},
		{done, []Result{pass, skip}, ""},
		{done, []Result{skip}, ""},
		{done, nil, ""},
	} {
		e, ok := VerifyEvent(tc.st, tc.results, at, OwnerAgent)
		if got := EventType(""); ok {
			got = e.Type
			if got != tc.want {
				t.Errorf("%s %v: %s, want %s", tc.st.State, tc.results, got, tc.want)
			}
		} else if tc.want != "" {
			t.Errorf("%s %v: nothing, want %s", tc.st.State, tc.results, tc.want)
		}
	}
	if e, _ := VerifyEvent(done, []Result{pass, fail}, at, OwnerAgent); !strings.Contains(e.Note, "b") {
		t.Errorf("the failed note names the check: %q", e.Note)
	}
}
