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
	"path/filepath"
	"strings"
	"time"
)

var version = "dev" // set at build time

const usage = `dnstest: which DNS resolver is fastest from here, over UDP, TCP, DoT, DoH and DoQ.

Usage: dnstest [options]     (with no options in a terminal, a menu asks what to test)

What to test
  -r, --resolver LIST   resolvers by id, comma separated (cloudflare,google,alidns …; --list shows all)
  -g, --group LIST      global, cn (mainland China), region (the other regional resolvers)
  -p, --proto LIST      protocols: udp, tcp, dot, doh, doq (default: all)
  -4 / -6               IPv4 (default) or IPv6
      --no-system       leave out this machine's own DNS servers

How to measure
  -n, --rounds N        cached lookups per resolver and protocol (default 10; half as many uncached)
  -t, --timeout DUR     per query (default 2s)
  -C, --concurrency N   queries in flight at once (default 16)

Output
  -o, --output FORMAT   md or json: also saved to a file in the current folder (dnstest-<date>-<time>.<ext>)
      --file NAME       the file to save to instead
      --json            the full result as JSON on stdout (nothing else)
  -v, --verbose         a line per resolver and protocol: address, handshake, median, p90, loss
      --lang CODE       en, zh-CN, zh-TW, ja, ko, es, fr, de, pt, ru (the menu asks once and remembers it;
                        DNSTEST_LANG works too)
      --no-color        plain text (also with NO_COLOR set)
  -q, --quiet           no progress display

Resolver list and updates
      --offline         use the list built into this binary, and don't look for updates
      --resolvers FILE  use a local list (same format as dns.1ms.dog/resolvers.json)
      --list            show the resolvers and what each offers, then exit
      --update          update to the latest release now
  -V, --version

Nothing is uploaded. Besides the DNS queries themselves, a run fetches the current resolver list from dns.1ms.dog and
asks Cloudflare where it runs from (city and network for the "from" line; the address is never shown or saved).
At most once an hour it also asks dns.1ms.dog for the latest release and, in a terminal, offers to update (the new
binary is checked against the release checksums). --offline or DNSTEST_NO_UPDATE=1 turns both off.
`

type cliOpts struct {
	resolvers, groups, protos, lang, file string
	output, saveTo                        string
	v6, v4, noSystem, json                bool
	verbose, quiet, noColor               bool
	offline, list, version                bool
	rounds, conc                          int
	timeout                               time.Duration
}

func main() {
	// quic-go logs a line when the OS won't give its UDP socket a bigger receive buffer; DoQ queries are a few hundred
	// bytes, so it doesn't matter here and would only clutter the output
	os.Setenv("QUIC_GO_DISABLE_RECEIVE_BUFFER_WARNING", "true")
	args := os.Args[1:]

	// language: --lang (read in cli), then DNSTEST_LANG, then the saved choice, then the system's
	askLang := true
	if l := knownLang(os.Getenv("DNSTEST_LANG")); l != "" {
		lang, askLang = l, false
	} else if l := knownLang(loadConfig().Lang); l != "" {
		lang, askLang = l, false
	} else {
		lang = pickLang("")
	}
	if code, stop := checkUpdate(args); stop {
		os.Exit(code)
	}

	// no options in a terminal: the menu builds them, and shows them so the test can be run again without it
	if len(args) == 0 && isTTY(os.Stdin) && isTTY(os.Stdout) {
		useColor = os.Getenv("NO_COLOR") == "" && enableVT()
		l, _, err := loadList("", false)
		if err != nil {
			fmt.Fprintf(os.Stderr, "dnstest: %v\n", err)
			os.Exit(1)
		}
		chosen := wizard(os.Stdin, os.Stdout, l, askLang)
		if chosen == nil {
			os.Exit(0)
		}
		fmt.Fprintf(os.Stdout, "\n%s\n  %s\n\n", paint(cDim, T("again")), paint(cCyan, commandLine(chosen)))
		args = chosen
	}
	os.Exit(cli(args, os.Stdout, os.Stderr))
}

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
	str(&o.groups, "", "g", "group")
	str(&o.protos, "", "p", "proto")
	str(&o.lang, "", "lang")
	str(&o.file, "", "resolvers")
	str(&o.output, "", "o", "output")
	str(&o.saveTo, "", "file")
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
	update := false
	boolean(&update, "update") // handled before cli; accepted here so it isn't an unknown flag
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
	if o.lang != "" {
		l := knownLang(o.lang)
		if l == "" {
			fmt.Fprintf(stderr, "dnstest: unknown language %q (en, zh-CN, zh-TW, ja, ko, es, fr, de, pt, ru)\n", o.lang)
			return 2
		}
		lang = l
	}
	useColor = !o.noColor && os.Getenv("NO_COLOR") == "" && isTTY(stdout) && enableVT()
	if o.output != "" && o.output != "md" && o.output != "json" {
		fmt.Fprintf(stderr, "dnstest: --output is md or json, not %q\n", o.output)
		return 2
	}

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
	if o.groups != "" {
		for _, g := range strings.Split(o.groups, ",") {
			g = strings.ToLower(strings.TrimSpace(g))
			if !contains(groups, g) {
				fmt.Fprintf(stderr, "dnstest: unknown group %q (global, cn, region)\n", g)
				return 2
			}
			ro.groups = append(ro.groups, g)
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
	// progress on stderr: the running dog in a color terminal wide enough for it, a plain line in any other terminal
	var prog progressFn
	var finish func()
	if !o.quiet && !o.json && isTTY(stderr) {
		cols := termCols(os.Stderr)
		if r := startRunner(stderr, cols); r != nil && !o.noColor && os.Getenv("NO_COLOR") == "" && enableVT() {
			prog = func(done, total int, t *target) {
				r.update(done, total, fmt.Sprintf("%3d%%  %s", done*100/max(1, total), paint(cDim, t.r.Name+" "+protoName[t.proto])))
			}
			finish = r.finish
		} else {
			if r != nil {
				r.finish()
			}
			pl := newProgLine(stderr)
			prog, finish = pl.update, pl.clear
		}
	}
	t0 := time.Now()
	res := run(ctx, l, ro, sys, prog)
	took := time.Since(t0)
	if finish != nil {
		finish()
	}
	if ctx.Err() != nil {
		fmt.Fprintln(stderr, "dnstest: interrupted")
		return 130
	}
	if o.json {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		enc.Encode(toJSON(res, src, took))
		return 0
	}
	printResult(stdout, res, ro, src, o.verbose, took)

	// -o: the same result saved as a file next to where it was run
	if o.output != "" {
		name := o.saveTo
		if name == "" {
			name = "dnstest-" + time.Now().Format("20060102-150405") + "." + o.output
		}
		f, err := os.Create(name)
		if err != nil {
			fmt.Fprintf(stderr, "dnstest: %v\n", err)
			return 1
		}
		if o.output == "json" {
			enc := json.NewEncoder(f)
			enc.SetIndent("", "  ")
			enc.Encode(toJSON(res, src, took))
		} else {
			printMarkdown(f, res, ro, src, took)
		}
		f.Close()
		if abs, err := filepath.Abs(name); err == nil {
			name = abs
		}
		fmt.Fprintln(stdout, paint(cGreen, T("saved"))+gap(T("saved"))+name)
	}
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
