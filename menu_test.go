package main

import (
	"bytes"
	"strings"
	"testing"
)

// The menu turns keys into options: each case types its answers and expects the options it builds.
func TestWizard(t *testing.T) {
	l, err := parseList(builtinList)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		v6   bool
		keys string
		want string // "" for exit
	}{
		{false, "\n\n\n", ""}, // Enter everywhere: all, 10 rounds, no file
		{false, "2\n1,2\n3\n2\n", "-g global,cn -n 20 -o md"},          // regions, precise, Markdown
		{false, "3\n1,3-5\n1\n3\n", "-p udp,dot,doh,doq -n 5 -o json"}, // protocols, quick, JSON
		{true, "1\n2\n\n\n", "-6"},                                     // IPv6
		{true, "1\n0\n0\n", "exit"},                                    // back to the first question, then exit
		{false, "2\n0\n3\n2\n\n\n", "-p tcp"},                          // back out of regions, then protocols
		{false, "9\n1\n\n\n", ""},                                      // a wrong number is asked again
	} {
		hasRoute = func(bool) bool { return c.v6 }
		got := wizard(strings.NewReader(c.keys), &bytes.Buffer{}, l, false)
		if c.want == "exit" {
			if got != nil {
				t.Errorf("%q: want exit, got %v", c.keys, got)
			}
			continue
		}
		if got == nil || strings.Join(got, " ") != c.want {
			t.Errorf("%q: got %v, want %q", c.keys, got, c.want)
		}
	}
}

func TestCommandLine(t *testing.T) {
	if s := commandLine([]string{"-g", "global,cn", "--file", "my result.md"}); s != "dnstest -g global,cn --file 'my result.md'" {
		t.Error(s)
	}
}

func TestNewer(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{{"0.2.0", "0.1.9", true}, {"0.1.1", "0.1.1", false}, {"1.0.0", "dev", false}, {"0.10.0", "0.9.0", true}} {
		if newer(c.a, c.b) != c.want {
			t.Errorf("newer(%s, %s) != %v", c.a, c.b, c.want)
		}
	}
}
