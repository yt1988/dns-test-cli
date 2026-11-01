// dnstest measures how fast public DNS resolvers answer from the machine it runs on, over plain DNS (UDP and TCP
// port 53), DNS over TLS, DNS over HTTPS and DNS over QUIC, checks what they do with DNSSEC, client subnets and names
// that don't exist, and says which one suits this network best. The list of resolvers is the one dns.1ms.dog tests.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"time"
)

var version = "dev" // set at build time

const usage = `dnstest: which DNS resolver is fastest from here, over UDP, TCP, DoT, DoH and DoQ.

Usage: dnstest [options]

What to test
  -r, --resolver LIST   resolvers by id, comma separated (cloudflare,google,alidns …; --list shows all)
  -p, --proto LIST      protocols: udp, tcp, dot, doh, doq (default: all)
  -4 / -6               IPv4 (default) or IPv6
      --no-system       leave out this machine's own DNS servers

How to measure
  -n, --rounds N        cached lookups per resolver and protocol (default 10; half as many uncached)
  -t, --timeout DUR     per query (default 2s)
  -C, --concurrency N   queries in flight at once (default 16)

Output
      --json            the full result as JSON on stdout
  -v, --verbose         a line per resolver and protocol: address, handshake, median, p90, loss
      --lang CODE       en, zh-CN, zh-TW, ja, ko, es, fr, de, pt, ru (default: the system's language;
                        DNSTEST_LANG works too)
      --no-color        plain text (also with NO_COLOR set)
  -q, --quiet           no progress line

Resolver list
      --offline         use the list built into this binary instead of fetching dns.1ms.dog/resolvers.json
      --resolvers FILE  use a local list (same format)
      --list            show the resolvers and what each offers, then exit
  -V, --version

Nothing is uploaded. Besides the DNS queries themselves, a run makes one request: the current resolver list from
dns.1ms.dog (--offline skips it).
`

type cliOpts struct {
	resolvers, protos, lang, file string
	v6, v4, noSystem, json        bool
	verbose, quiet, noColor       bool
	offline, list, version        bool
	rounds, conc                  int
	timeout                       time.Duration
}

func main() { os.Exit(cli(os.Args[1:], os.Stdout, os.Stderr)) }

func cli(args []string, stdout, stderr io.Writer) int {
	var o cliOpts
	fs := flag.NewFlagSet("dnstest", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	str := func(p *string, def string, names ...string) {
		for _, n := range names {
			fs.StringVar(p, n, def, "")
		}
	}
	boolean := func(p *bool, names ...string) {
		for _, n := range names {
			fs.BoolVar(p, n, false, "")
		}
	}
	integer := func(p *int, def int, names ...string) {
		for _, n := range names {
			fs.IntVar(p, n, def, "")
		}
	}
	str(&o.resolvers, "", "r", "resolver")
	str(&o.protos, "", "p", "proto")
	str(&o.lang, "", "lang")
	str(&o.file, "", "resolvers")
	boolean(&o.v4, "4")
	boolean(&o.v6, "6")
	boolean(&o.noSystem, "no-system")
	boolean(&o.json, "json")
	boolean(&o.verbose, "v", "verbose")
	boolean(&o.quiet, "q", "quiet")
	boolean(&o.noColor, "no-color")
	boolean(&o.offline, "offline")
	boolean(&o.list, "list")
	boolean(&o.version, "V", "version")
	integer(&o.rounds, 10, "n", "rounds")
	integer(&o.conc, 16, "C", "concurrency")
	fs.DurationVar(&o.timeout, "t", 2*time.Second, "")
	fs.DurationVar(&o.timeout, "timeout", 2*time.Second, "")
	help := false
	boolean(&help, "h", "help")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(stderr, "dnstest: %v\n\n%s", err, usage)
		return 2
	}
	if help {
		fmt.Fprint(stdout, usage)
		return 0
	}
	if o.version {
		fmt.Fprintln(stdout, "dnstest "+version)
		return 0
	}
	lang = pickLang(o.lang)
	useColor = !o.noColor && os.Getenv("NO_COLOR") == "" && termCols(os.Stdout) > 0 && enableVT()

	l, src, err := loadList(o.file, o.offline)
	if err != nil {
		fmt.Fprintf(stderr, "dnstest: %v\n", err)
		return 1
	}
	if o.list {
		printList(stdout, l)
		return 0
	}

	ro := runOpts{v6: o.v6 && !o.v4, rounds: o.rounds, timeout: o.timeout, conc: o.conc}
	if o.rounds < 2 || o.rounds > 100 || o.conc < 1 || o.timeout < 100*time.Millisecond {
		fmt.Fprintln(stderr, "dnstest: --rounds 2–100, --concurrency ≥ 1, --timeout ≥ 100ms")
		return 2
	}
	if o.resolvers != "" {
		for _, id := range strings.Split(o.resolvers, ",") {
			id = strings.ToLower(strings.TrimSpace(id))
			if !hasResolver(l, id) {
				fmt.Fprintf(stderr, "dnstest: no resolver %q (dnstest --list shows them)\n", id)
				return 2
			}
			ro.resolvers = append(ro.resolvers, id)
		}
	}
	ro.protos = protos
	if o.protos != "" {
		ro.protos = nil
		for _, p := range strings.Split(o.protos, ",") {
			p = strings.ToLower(strings.TrimSpace(p))
			if !contains(protos, p) {
				fmt.Fprintf(stderr, "dnstest: unknown protocol %q (udp, tcp, dot, doh, doq)\n", p)
				return 2
			}
			ro.protos = append(ro.protos, p)
		}
	}
	var sys []resolver
	if !o.noSystem && len(ro.resolvers) == 0 {
		sys = systemResolvers(l, ro.v6)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	var prog progressFn
	var pl *progLine
	if !o.quiet && !o.json && termCols(os.Stderr) > 0 {
		pl = newProgLine(stderr)
		prog = pl.update
	}
	t0 := time.Now()
	res := run(ctx, l, ro, sys, prog)
	if pl != nil {
		pl.clear()
	}
	if ctx.Err() != nil {
		fmt.Fprintln(stderr, "dnstest: interrupted")
		return 130
	}
	if o.json {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		enc.Encode(toJSON(res, src, time.Since(t0)))
		return 0
	}
	printResult(stdout, res, ro, src, o.verbose)
	return 0
}

func hasResolver(l *list, id string) bool {
	for _, r := range l.Resolvers {
		if r.ID == id {
			return true
		}
	}
	return false
}

func contains(a []string, s string) bool {
	for _, x := range a {
		if x == s {
			return true
		}
	}
	return false
}

// systemResolvers turns the machine's own DNS servers into resolvers of their own, unless one is a listed public
// resolver (then that row already shows it). Loopback stubs are left out: they forward to something else.
func systemResolvers(l *list, v6 bool) []resolver {
	known := map[string]bool{}
	for _, r := range l.Resolvers {
		for _, a := range append(append([]string{}, r.UDP...), r.UDP6...) {
			known[a] = true
		}
	}
	var out []resolver
	n := 0
	for _, a := range systemServers() {
		a = a.Unmap()
		if a.Is6() != v6 || a.IsLoopback() || a.IsUnspecified() || known[a.String()] {
			continue
		}
		n++
		if n > 2 {
			break
		}
		r := resolver{ID: "system", Name: T("system", a.String()), Group: "system"}
		if v6 {
			r.UDP6 = []string{a.String()}
		} else {
			r.UDP = []string{a.String()}
		}
		out = append(out, r)
	}
	return out
}
