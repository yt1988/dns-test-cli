package main

import (
	"context"
	"math/rand/v2"
	"net"
	"net/netip"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

// How a run measures, so that results can be trusted and compared:
//   - Every resolver × protocol is a target with its own open connection. Handshakes are timed apart.
//   - Warm-up: connect, then ask each of the run's popular names once. That puts them in the resolver's cache and
//     gets the connection going; none of it counts.
//   - Rounds: in each round every target is asked once, in a new random order, a few at a time. A burst of network
//     trouble hits every resolver alike instead of whichever was being tested at that moment.
//     Cached: one of the popular names. Uncached (every other round): a random name under nxzone, which no resolver
//     can have cached, so it has to ask the authoritative servers: what an unpopular site costs.
//   - A reply only counts when it answers that very query (ID and question). No reply within the timeout is a loss.
//   - Checks: DNSSEC validation, client subnet (ECS), and whether a name that doesn't exist comes back as NXDOMAIN
//     (some resolvers answer with an ad server instead).
//   - Before all that, plain DNS is sent to an address that runs no DNS server. Any reply means something on the way
//     (a proxy, the router, the ISP) answers DNS queries itself: then the plain DNS times are that interceptor's.

type target struct {
	r     *resolver
	proto string
	ip    string // the address queries go to
	host  string // TLS name
	url   string // DoH
	c     client

	hs        time.Duration // TCP / TLS / QUIC handshake
	up        bool          // answered during warm-up
	err       string        // why not
	cached    []time.Duration
	uncached  []time.Duration
	sent      int
	lost      int
	nxRewrite bool // a name that doesn't exist got an address
	dnssec    int8 // -1 unknown, 0 doesn't validate, 1 validates
	ecs       int8 // -1 unknown, 0 doesn't pass on the client subnet, 1 does
	egress    string
}

type runOpts struct {
	resolvers []string // ids; empty is all
	groups    []string // global, cn, region; empty is all
	protos    []string
	v6        bool
	rounds    int
	timeout   time.Duration
	conc      int
}

type result struct {
	targets   []*target
	intercept bool // plain DNS is answered by something on the path
	inject    bool // forged answers for some names on the path
	fam       string
	names     []string
	from      *Origin // where the test ran from (no address)
}

func addrPort(ip, port string) string { return net.JoinHostPort(ip, port) }

func pick(ips []string) string {
	if len(ips) == 0 {
		return ""
	}
	return ips[0]
}

// buildTargets: one per resolver and protocol it offers in this address family. DoT, DoH and DoQ hosts without listed
// addresses are looked up later.
func buildTargets(l *list, o runOpts, sys []resolver) []*target {
	want := map[string]bool{}
	for _, id := range o.resolvers {
		want[strings.ToLower(id)] = true
	}
	var out []*target
	add := func(r *resolver) {
		for _, p := range o.protos {
			t := &target{r: r, proto: p, dnssec: -1, ecs: -1}
			switch p {
			case pUDP, pTCP:
				ips := r.UDP
				if o.v6 {
					ips = r.UDP6
				}
				t.ip = pick(ips)
			case pDoT, pDoQ:
				e := r.DoT
				if p == pDoQ {
					e = r.DoQ
				}
				if e == nil {
					continue
				}
				t.host = e.Host
				if o.v6 {
					t.ip = pick(e.IPs6)
				} else {
					t.ip = pick(e.IPs)
				}
			case pDoH:
				if r.DoH == nil {
					continue
				}
				t.url = r.DoH.URL
				if u, err := urlHost(t.url); err == nil {
					t.host = u
				}
				if o.v6 {
					t.ip = pick(r.DoH.IPs6)
				} else {
					t.ip = pick(r.DoH.IPs)
				}
			}
			if t.ip == "" && t.host == "" {
				continue // plain DNS not offered in this family
			}
			out = append(out, t)
		}
	}
	for i := range sys {
		add(&sys[i])
	}
	for i := range l.Resolvers {
		r := &l.Resolvers[i]
		if len(want) > 0 && !want[r.ID] || len(o.groups) > 0 && !contains(o.groups, r.Group) {
			continue
		}
		add(r)
	}
	return out
}

func (t *target) port() string {
	switch t.proto {
	case pDoT, pDoQ:
		return "853"
	case pDoH:
		if h, p, err := net.SplitHostPort(strings.SplitN(strings.TrimPrefix(t.url, "https://"), "/", 2)[0]); err == nil && h != "" {
			return p
		}
		return "443"
	}
	return "53"
}

func (t *target) newClient() client {
	a := addrPort(t.ip, t.port())
	switch t.proto {
	case pUDP:
		return &udpClient{addr: a}
	case pTCP:
		return &streamClient{addr: a}
	case pDoT:
		return &streamClient{addr: a, host: t.host}
	case pDoH:
		return newDoH(t.url, t.host, a)
	default:
		return &doqClient{addr: a, host: t.host}
	}
}

// ask sends one query and reads the reply. DoQ queries carry ID 0 (RFC 9250).
func (t *target) ask(ctx context.Context, name string, qt dnsmessage.Type, timeout time.Duration) (reply, time.Duration, error) {
	id := randID()
	if t.proto == pDoQ {
		id = 0
	}
	q, err := newQuery(name, qt, id)
	if err != nil {
		return reply{}, 0, err
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	m, d, err := t.c.exchange(ctx, q)
	if err != nil {
		return reply{}, 0, err
	}
	r, err := parseReply(q, m)
	return r, d, err
}

// lookupHosts finds addresses for DoT/DoH/DoQ hosts the list gives none for: the system resolver first, then, if it
// fails or gives an address no real server has (a proxy's fake IPs, private ranges), DoH to two big resolvers by IP.
func lookupHosts(ctx context.Context, ts []*target, v6 bool) {
	hosts := map[string]string{}
	for _, t := range ts {
		if t.ip == "" && t.host != "" {
			hosts[t.host] = ""
		}
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for h := range hosts {
		wg.Add(1)
		go func(h string) {
			defer wg.Done()
			ip := lookupHost(ctx, h, v6)
			mu.Lock()
			hosts[h] = ip
			mu.Unlock()
		}(h)
	}
	wg.Wait()
	for _, t := range ts {
		if t.ip == "" {
			t.ip = hosts[t.host]
			if t.ip == "" {
				t.err = "lookup"
			}
		}
	}
}

func realAddr(a netip.Addr) bool {
	if !a.IsValid() || a.IsPrivate() || a.IsLoopback() || a.IsUnspecified() || a.IsLinkLocalUnicast() || a.IsMulticast() {
		return false
	}
	if a.Is4() && netip.MustParsePrefix("198.18.0.0/15").Contains(a) { // the fake-IP range proxies hand out
		return false
	}
	if a.Is4() && netip.MustParsePrefix("100.64.0.0/10").Contains(a) {
		return false
	}
	return true
}

func lookupHost(ctx context.Context, host string, v6 bool) string {
	fam, qt := "ip4", dnsmessage.TypeA
	if v6 {
		fam, qt = "ip6", dnsmessage.TypeAAAA
	}
	c, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if as, err := net.DefaultResolver.LookupNetIP(c, fam, host); err == nil {
		for _, a := range as {
			if realAddr(a.Unmap()) {
				return a.Unmap().String()
			}
		}
	}
	// the system resolver failed or is faked: ask over DoH, by IP
	type res struct{ ip string }
	ch := make(chan res, 2)
	for _, b := range [][2]string{{"https://1.1.1.1/dns-query", "1.1.1.1:443"}, {"https://223.5.5.5/dns-query", "223.5.5.5:443"}} {
		go func(url, addr string) {
			h, _ := urlHost(url)
			t := &target{proto: pDoH, c: newDoH(url, h, addr)}
			defer t.c.close()
			r, _, err := t.ask(ctx, host, qt, 3*time.Second)
			if err == nil {
				for _, s := range r.Addrs {
					if a, err := netip.ParseAddr(s); err == nil && realAddr(a) {
						ch <- res{s}
						return
					}
				}
			}
			ch <- res{}
		}(b[0], b[1])
	}
	for i := 0; i < 2; i++ {
		if r := <-ch; r.ip != "" {
			return r.ip
		}
	}
	return ""
}

func urlHost(u string) (string, error) {
	s := strings.TrimPrefix(u, "https://")
	s = strings.SplitN(s, "/", 2)[0]
	if h, _, err := net.SplitHostPort(s); err == nil {
		return h, nil
	}
	return s, nil
}

// canary sends plain DNS to an address in Cloudflare's web range, where no DNS server runs. A reply to a random name
// means every plain DNS query is answered on the way; a reply only for a name that is censored somewhere means forged
// answers are injected for such names.
func canary(ctx context.Context, l *list, v6 bool, timeout time.Duration) (intercept, inject bool) {
	ip := "104.16.0.1"
	if v6 {
		ip = "2606:4700::6810:1"
	}
	try := func(proto, name string) bool {
		t := &target{proto: proto, ip: ip}
		t.c = t.newClient()
		defer t.c.close()
		_, _, err := t.ask(ctx, name, dnsmessage.TypeA, timeout)
		return err == nil
	}
	var wg sync.WaitGroup
	var a, b, c atomic.Bool
	wg.Add(3)
	go func() { defer wg.Done(); a.Store(try(pUDP, randLabel()+"."+l.NXZone)) }()
	go func() { defer wg.Done(); b.Store(try(pTCP, randLabel()+"."+l.NXZone)) }()
	go func() { defer wg.Done(); c.Store(try(pUDP, "www.google.com")) }()
	wg.Wait()
	intercept = a.Load() || b.Load()
	return intercept, !intercept && c.Load()
}

// each runs f over ts, n at a time, in a random order.
func each(ts []*target, n int, f func(*target)) {
	order := rand.Perm(len(ts))
	sem := make(chan struct{}, n)
	var wg sync.WaitGroup
	for _, i := range order {
		t := ts[i]
		sem <- struct{}{}
		wg.Add(1)
		go func() {
			defer func() { <-sem; wg.Done() }()
			f(t)
		}()
	}
	wg.Wait()
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	if isTimeout(err) || err == context.DeadlineExceeded {
		return "timeout"
	}
	s := err.Error()
	switch {
	case strings.Contains(s, "refused"):
		return "refused"
	case strings.Contains(s, "reset"):
		return "reset"
	case strings.Contains(s, "certificate"), strings.Contains(s, "x509"), strings.Contains(s, "tls:"):
		return "tls"
	case strings.Contains(s, "unreachable"), strings.Contains(s, "no route"):
		return "unreachable"
	case strings.Contains(s, "HTTP "):
		return strings.TrimSpace(s[strings.Index(s, "HTTP "):])
	}
	if len(s) > 40 {
		s = s[:40]
	}
	return s
}

type progressFn func(done, total int, t *target)

func run(ctx context.Context, l *list, o runOpts, sys []resolver, prog progressFn) *result {
	res := &result{fam: "IPv4"}
	if o.v6 {
		res.fam = "IPv6"
	}
	ts := buildTargets(l, o, sys)
	res.targets = ts

	// three of the popular names, the same for every target
	names := append([]string(nil), l.Names...)
	rand.Shuffle(len(names), func(i, j int) { names[i], names[j] = names[j], names[i] })
	if len(names) > 3 {
		names = names[:3]
	}
	res.names = names

	// all before the first timed query: where the test runs from is two HTTPS requests that shouldn't share the line
	var wg sync.WaitGroup
	wg.Add(3)
	go func() { defer wg.Done(); lookupHosts(ctx, ts, o.v6) }()
	go func() {
		defer wg.Done()
		// only plain DNS can be intercepted: without it in the test there is nothing to warn about
		if contains(o.protos, pUDP) || contains(o.protos, pTCP) {
			res.intercept, res.inject = canary(ctx, l, o.v6, o.timeout)
		}
	}()
	go func() { defer wg.Done(); res.from = origin(o.v6) }()
	wg.Wait()

	uRounds := (o.rounds + 1) / 2
	total := len(ts) * (1 + o.rounds + 1)
	var done atomic.Int64
	step := func(t *target) {
		if prog != nil {
			prog(int(done.Add(1)), total, t)
		}
	}

	// warm-up
	each(ts, o.conc, func(t *target) {
		defer step(t)
		if t.err != "" {
			return
		}
		t.c = t.newClient()
		cctx, cancel := context.WithTimeout(ctx, o.timeout+time.Second)
		hs, err := t.c.connect(cctx)
		cancel()
		if err != nil {
			t.err = errText(err)
			return
		}
		t.hs = hs
		var last error
		for _, n := range names {
			if _, _, err := t.ask(ctx, n, dnsmessage.TypeA, o.timeout); err != nil {
				last = err
				continue
			}
			t.up = true
		}
		if !t.up {
			t.err = errText(last)
		}
	})

	up := make([]*target, 0, len(ts))
	for _, t := range ts {
		if t.up {
			up = append(up, t)
		} else {
			done.Add(int64(o.rounds + 1))
		}
	}

	var mu sync.Mutex
	for r := 0; r < o.rounds && ctx.Err() == nil; r++ {
		name := names[r%len(names)]
		withNX := r%2 == 0 && r/2 < uRounds
		each(up, o.conc, func(t *target) {
			defer step(t)
			_, d, err := t.ask(ctx, name, dnsmessage.TypeA, o.timeout)
			mu.Lock()
			t.sent++
			if err != nil {
				t.lost++
			} else {
				t.cached = append(t.cached, d)
			}
			mu.Unlock()
			if !withNX {
				return
			}
			rp, d, err := t.ask(ctx, randLabel()+"."+l.NXZone, dnsmessage.TypeA, o.timeout)
			mu.Lock()
			t.sent++
			if err != nil {
				t.lost++
			} else {
				t.uncached = append(t.uncached, d)
				if rp.RCode == dnsmessage.RCodeSuccess && len(rp.Addrs) > 0 {
					t.nxRewrite = true
				}
			}
			mu.Unlock()
		})
	}

	// checks
	each(up, o.conc, func(t *target) {
		defer step(t)
		if rb, _, err := t.ask(ctx, l.Checks.Bad, dnsmessage.TypeA, o.timeout); err == nil {
			if rg, _, err := t.ask(ctx, l.Checks.Good, dnsmessage.TypeA, o.timeout); err == nil {
				if rb.RCode == dnsmessage.RCodeServerFailure && rg.AD {
					t.dnssec = 1
				} else {
					t.dnssec = 0
				}
			}
		}
		if r, _, err := t.ask(ctx, l.Checks.ECS, dnsmessage.TypeTXT, o.timeout); err == nil && len(r.TXT) > 0 {
			t.ecs = 0
			for _, s := range r.TXT {
				if strings.HasPrefix(s, "edns0-client-subnet") {
					t.ecs = 1
				} else if _, err := netip.ParseAddr(s); err == nil {
					t.egress = s
				}
			}
		}
	})
	for _, t := range ts {
		if t.c != nil {
			t.c.close()
		}
	}
	return res
}

/* ---------- statistics ---------- */

func quantile(ds []time.Duration, q float64) time.Duration {
	if len(ds) == 0 {
		return -1
	}
	s := append([]time.Duration(nil), ds...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	pos := q * float64(len(s)-1)
	i := int(pos)
	if i+1 >= len(s) {
		return s[i]
	}
	f := pos - float64(i)
	return s[i] + time.Duration(f*float64(s[i+1]-s[i]))
}

func (t *target) median() time.Duration      { return quantile(t.cached, .5) }
func (t *target) p90() time.Duration         { return quantile(t.cached, .9) }
func (t *target) uncachedMed() time.Duration { return quantile(t.uncached, .5) }

func (t *target) loss() float64 {
	if t.sent == 0 {
		return 0
	}
	return float64(t.lost) / float64(t.sent)
}

// score ranks targets as dns.1ms.dog does: the median of the cached lookups, and each percent of lost queries makes it
// 3% worse (a lost query costs a retry, a second or more). Uncached times are shown, not ranked: they depend on where
// each site's own DNS servers are. Lower is better; -1 when there's nothing to rank.
func (t *target) score() float64 {
	m := t.median()
	if m < 0 {
		return -1
	}
	return ms(m) * (1 + 3*t.loss())
}

func ms(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }

// plain reports whether a target is unencrypted DNS (the kind an interceptor answers).
func (t *target) plain() bool { return t.proto == pUDP || t.proto == pTCP }
