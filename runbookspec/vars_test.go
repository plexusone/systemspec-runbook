package runbookspec

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestReferencesAndPlaceholders(t *testing.T) {
	s := "ssh {{ user }}@{{host}} and {{host}} again, not ${HOME} or {{1bad}}"
	if got := strings.Join(References(s), ","); got != "user,host" {
		t.Errorf("References = %s", got)
	}
	if got := Placeholders(s); got != "ssh <user>@<host> and <host> again, not ${HOME} or {{1bad}}" {
		t.Errorf("Placeholders = %s", got)
	}
}

func TestValuesOrderAndEnvironment(t *testing.T) {
	rb := &Runbook{Variables: []Variable{
		{Name: "profile", Description: "the AWS profile", Env: "AWS_PROFILE"},
		{Name: "ip", Description: "the instance's address"},
	}}
	provider := func(_ context.Context, name string) (string, bool, error) {
		if name == "ip" {
			return "192.0.2.10", true, nil
		}
		return "", false, nil
	}
	v := NewValues(rb, MapSource(map[string]string{"profile": "from-flag"}), provider)
	v.lookupEnv = func(k string) (string, bool) {
		return map[string]string{"AWS_PROFILE": "from-env"}[k], k == "AWS_PROFILE"
	}
	got, err := v.Expand(context.Background(), "{{profile}} {{ip}}")
	if err != nil || got != "from-flag 192.0.2.10" {
		t.Errorf("Expand = %q, %v", got, err)
	}

	w := NewValues(rb)
	w.lookupEnv = v.lookupEnv
	if got, err := w.Expand(context.Background(), "{{profile}}"); err != nil || got != "from-env" {
		t.Errorf("from the environment = %q, %v", got, err)
	}
	_, err = w.ExpandAll(context.Background(), []string{"{{ip}}", "{{nope}} {{ip}}"})
	var u *UnfilledError
	if !errors.As(err, &u) || strings.Join(u.Names, ",") != "ip,nope" {
		t.Errorf("unfilled = %v", err)
	}
	failing := NewValues(rb, func(context.Context, string) (string, bool, error) { return "", false, errors.New("no stack") })
	if _, err := failing.Expand(context.Background(), "{{ip}}"); err == nil || !strings.Contains(err.Error(), "no stack") {
		t.Errorf("a source's error = %v", err)
	}
}

func TestValidateVariablesAndExec(t *testing.T) {
	rb := sample()
	rb.Variables = []Variable{{Name: "region", Description: "the region"}, {Name: "region", Description: "again"}, {Name: "bad name"}}
	s := &rb.Phases[0].Steps[1]
	s.Actions = []Action{
		{Text: "Use {{region}} and {{undeclared}}.", Exec: []Exec{
			{Args: []string{}},
			{Args: []string{"pulumi"}, Stdin: &Stdin{From: []string{"x"}, RandomHex: 32}},
			{Args: []string{"pulumi"}, Stdin: &Stdin{RandomHex: 8}},
		}},
	}
	err := rb.Validate(ValidateOptions{})
	if err == nil {
		t.Fatal("Validate passed")
	}
	for _, want := range []string{"declared twice", "variable \"bad name\"", "bad name: description is required",
		"references {{undeclared}}", "command 1: needs a program", "exactly one of from and randomHex", "randomHex is 16 to 1024"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("missing %q in:\n%v", want, err)
		}
	}
}

func TestShellLines(t *testing.T) {
	got := ShellLines([]Exec{
		{Args: []string{"pulumi", "stack", "init", "prod", "--secrets-provider=awskms://alias/k?region={{region}}"}, Dir: "stack", Env: map[string]string{"AWS_PROFILE": "{{profile}}"}},
		{Args: []string{"pulumi", "config", "set", "--secret", "fleet:dbMasterPassword"}, Dir: "stack", Env: map[string]string{"AWS_PROFILE": "{{profile}}"}, Stdin: &Stdin{RandomHex: 32}},
		{Args: []string{"ssh", "host", "sudo bash -s"}, Stdin: &Stdin{From: []string{"bin/infra", "host-setup", "site", "prod"}}},
	})
	want := "cd stack\nexport AWS_PROFILE={{profile}}\n" +
		"pulumi stack init prod '--secrets-provider=awskms://alias/k?region={{region}}'\n" +
		"openssl rand -hex 32 | pulumi config set --secret fleet:dbMasterPassword\n" +
		"cd -\nbin/infra host-setup site prod | ssh host 'sudo bash -s'"
	if got != want {
		t.Errorf("ShellLines:\n%s\nwant:\n%s", got, want)
	}
	if a := (Action{Exec: []Exec{{Args: []string{"echo", "{{x}}"}}}}); a.Code() != "echo <x>" {
		t.Errorf("Code = %q", a.Code())
	}
}
