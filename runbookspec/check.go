package runbookspec

// CheckKind names what a check does.
type CheckKind string

// The kinds of check.
const (
	// KindDNS looks up a name and expects answers.
	KindDNS CheckKind = "dns"
	// KindHTTP requests a URL and expects a status.
	KindHTTP CheckKind = "http"
	// KindUnreachable expects a connection to fail.
	KindUnreachable CheckKind = "unreachable"
	// KindCommand runs a command, never through a shell, and expects it to
	// exit 0.
	KindCommand CheckKind = "command"
	// KindNamed runs a check registered in Go under a name, for what
	// needs an SDK or an API.
	KindNamed CheckKind = "named"
)

// CheckKinds are the valid kinds.
var CheckKinds = []CheckKind{KindDNS, KindHTTP, KindUnreachable, KindCommand, KindNamed}

// Check proves part of a step is done. Kind names it, and exactly the
// field of that kind holds its parameters. A check only reads.
//
// Strings in the parameters may hold ${name} variables, which the runner
// replaces with values known only at run time, such as a stack output.
type Check struct {
	// ID is unique within the step.
	ID string `json:"id"`
	// Description says what is checked.
	Description string `json:"description"`
	// Kind names the check and the field holding its parameters.
	Kind CheckKind `json:"kind"`
	// DNS holds a dns check's parameters.
	DNS *DNSCheck `json:"dns,omitempty"`
	// HTTP holds an http check's parameters.
	HTTP *HTTPCheck `json:"http,omitempty"`
	// Unreachable holds an unreachable check's parameters.
	Unreachable *UnreachableCheck `json:"unreachable,omitempty"`
	// Command holds a command check's parameters.
	Command *CommandCheck `json:"command,omitempty"`
	// Named holds a named check's parameters.
	Named *NamedCheck `json:"named,omitempty"`
}

// DNSCheck passes when a lookup returns an answer containing each wanted
// string.
type DNSCheck struct {
	// Name is looked up.
	Name string `json:"name"`
	// Type is A, AAAA, CNAME, MX, NS or TXT.
	Type string `json:"type"`
	// Contains are substrings, each found in at least one answer, such as
	// "ns.cloudflare.com" for a zone on Cloudflare.
	Contains []string `json:"contains"`
}

// HTTPCheck passes when a request returns the status, without following
// redirects.
type HTTPCheck struct {
	// URL is requested.
	URL string `json:"url"`
	// Method is the request's method; GET when empty.
	Method string `json:"method,omitempty"`
	// Status is the status expected.
	Status int `json:"status"`
	// Contains, when set, must be in the body.
	Contains string `json:"contains,omitempty"`
	// Location, when set, must prefix the Location header of a redirect.
	Location string `json:"location,omitempty"`
}

// UnreachableCheck passes when a connection to the address fails within
// the timeout.
type UnreachableCheck struct {
	// Address is host:port.
	Address string `json:"address"`
	// Timeout is a Go duration, such as "10s"; 10s when empty.
	Timeout string `json:"timeout,omitempty"`
}

// CommandCheck passes when the command exits 0.
type CommandCheck struct {
	// Args are the command and its arguments, run without a shell.
	Args []string `json:"args"`
	// Contains, when set, must be in the command's standard output.
	Contains string `json:"contains,omitempty"`
}

// NamedCheck runs a check registered in Go.
type NamedCheck struct {
	// Name is the registered check, such as "aws.s3-bucket".
	Name string `json:"name"`
	// Args are its parameters.
	Args map[string]string `json:"args,omitempty"`
}
