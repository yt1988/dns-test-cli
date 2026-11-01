package main

import (
	"fmt"
	"io"
	"math"
	"sort"
	"strings"
	"sync"
	"time"
)

var useColor bool

const (
	cReset   = "\x1b[0m"
	cBold    = "\x1b[1m"
	cDim     = "\x1b[2m"
	cRed     = "\x1b[31m"
	cGreen   = "\x1b[32m"
	cYellow  = "\x1b[33m"
	cCyan    = "\x1b[36m"
	cMagenta = "\x1b[35m"
)

func paint(code, s string) string {
	if !useColor || s == "" {
		return s
	}
	return code + s + cReset
}

var protoName = map[string]string{pUDP: "UDP", pTCP: "TCP", pDoT: "DoT", pDoH: "DoH", pDoQ: "DoQ"}

// width is how many columns s takes in a terminal: CJK and fullwidth characters take two.
func width(s string) int {
	w, esc := 0, false
	for _, r := range s {
		switch {
		case r == 0x1b:
			esc = true
		case esc:
			esc = r != 'm'
		case r >= 0x1100 && r <= 0x115f, r >= 0x2e80 && r <= 0xa4cf, r >= 0xac00 && r <= 0xd7a3,
			r >= 0xf900 && r <= 0xfaff, r >= 0xfe30 && r <= 0xfe4f, r >= 0xff00 && r <= 0xff60, r >= 0xffe0 && r <= 0xffe6:
			w += 2
		default:
			w++
		}
	}
	return w
}

func padR(s string, n int) string { return s + strings.Repeat(" ", max(0, n-width(s))) }
func padL(s string, n int) string { return strings.Repeat(" ", max(0, n-width(s))) + s }

// fmtMS: one decimal below 10 ms, whole milliseconds above.
func fmtMS(d time.Duration) string {
	if d < 0 {
		return "–"
	}
	v := ms(d)
	if v < 10 {
		return fmt.Sprintf("%.1f", v)
	}
	return fmt.Sprintf("%.0f", math.Round(v))
}

// a resolver's row: its targets by protocol, ranked by its best one
type row struct {
	r    *resolver
	by   map[string]*target
	best *target
}

// ranked groups targets per resolver, best first. Plain DNS that an interceptor answers can't rank.
func ranked(res *result) []*row {
	var rows []*row
	idx := map[*resolver]*row{}
	for _, t := range res.targets {
		w := idx[t.r]
		if w == nil {
			w = &row{r: t.r, by: map[string]*target{}}
			idx[t.r] = w
			rows = append(rows, w)
		}
		w.by[t.proto] = t
		if !rankable(res, t) {
			continue
		}
		if w.best == nil || t.score() < w.best.score() {
			w.best = t
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i].best, rows[j].best
		if a == nil || b == nil {
			return a != nil
		}
		return a.score() < b.score()
	})
	return rows
}

func rankable(res *result, t *target) bool {
	return t.up && t.score() >= 0 && !(res.intercept && t.plain()) && t.loss() < .5
}

func cell(res *result, t *target) string {
	if t == nil {
		return paint(cDim, "·")
	}
	if !t.up {
		return paint(cRed, "×")
	}
	s := fmtMS(t.median())
	if res.intercept && t.plain() {
		return paint(cDim, s+"*")
	}
	switch l := t.loss(); {
	case l >= .2:
		return paint(cRed, s+"!")
	case l > 0:
		return paint(cYellow, s+"!")
	}
	return paint(tier(t.median()), s)
}

// tier colors a time as the website does: green up to 20 ms, then cyan, yellow, magenta, and red above 200 ms.
func tier(d time.Duration) string {
	switch v := ms(d); {
	case v <= 20:
		return cGreen
	case v <= 50:
		return cCyan
	case v <= 100:
		return cYellow
	case v <= 200:
		return cMagenta
	}
	return cRed
}

func mark(v int8) string {
	switch v {
	case 1:
		return paint(cGreen, "✓")
	case 0:
		return paint(cDim, "–")
	}
	return paint(cDim, "?")
}

func printResult(w io.Writer, res *result, o runOpts, src string, verbose bool, took time.Duration) {
	rows := ranked(res)
	used := map[string]bool{}
	for _, t := range res.targets {
		used[t.proto] = true
	}
	var cols []string
	for _, p := range protos {
		if used[p] {
			cols = append(cols, p)
		}
	}
	nr := 0
	for _, r := range rows {
		if r.r.Group != "system" {
			nr++
		}
	}
	fmt.Fprintln(w, paint(cBold, "dnstest "+version)+paint(cDim, " · "+T("head", res.fam, nr, o.rounds)))
	fmt.Fprintf(w, "%s %s\n", paint(cDim, T("from")), paint(cBold, res.from.String()))
	if res.intercept {
		fmt.Fprintln(w, paint(cYellow, "! "+T("w_intercept")))
	} else if res.inject {
		fmt.Fprintln(w, paint(cYellow, "! "+T("w_inject")))
	}
	fmt.Fprintln(w)

	nameW := width(T("c_dns"))
	for _, r := range rows {
		nameW = max(nameW, width(r.r.Name))
	}
	const cw = 7
	var b strings.Builder
	b.WriteString("  " + padL("#", 2) + "  " + padR(T("c_dns"), nameW))
	for _, p := range cols {
		b.WriteString(padL(protoName[p], cw))
	}
	b.WriteString(padL(T("c_uncached"), max(10, width(T("c_uncached"))+2)) + padL("DNSSEC", 8) + padL("ECS", 5))
	fmt.Fprintln(w, paint(cDim, b.String()))
	for i, r := range rows {
		b.Reset()
		n := "–"
		if r.best != nil {
			n = fmt.Sprint(i + 1)
		}
		name := r.r.Name
		if r.r.Group == "system" {
			name = paint(cCyan, name)
		} else if i == 0 && r.best != nil {
			name = paint(cBold, name)
		}
		b.WriteString("  " + padL(n, 2) + "  " + padR(name, nameW))
		for _, p := range cols {
			c := cell(res, r.by[p])
			b.WriteString(padL(c, cw))
		}
		unc, sec, ecs := "–", int8(-1), int8(-1)
		if r.best != nil {
			unc = fmtMS(r.best.uncachedMed())
		}
		for _, t := range r.by {
			// an interceptor's answers say nothing about the resolver
			if res.intercept && t.plain() {
				continue
			}
			sec, ecs = maxI8(sec, t.dnssec), maxI8(ecs, t.ecs)
			if t.nxRewrite {
				unc = paint(cRed, T("nx_rewrite"))
			}
		}
		uw := max(10, width(T("c_uncached"))+2)
		b.WriteString(padL(unc, uw))
		ms, me := mark(sec), mark(ecs)
		b.WriteString(padL(ms, 8) + padL(me, 5))
		fmt.Fprintln(w, b.String())
	}

	if verbose {
		fmt.Fprintln(w)
		fmt.Fprintln(w, paint(cDim, "  "+T("v_head")))
		for _, r := range rows {
			for _, p := range cols {
				t := r.by[p]
				if t == nil {
					continue
				}
				line := fmt.Sprintf("  %s %-4s %-40s", padR(r.r.Name, nameW), protoName[p], t.ip+" "+t.host)
				if !t.up {
					line += paint(cRed, " "+t.err)
				} else {
					line += fmt.Sprintf(" hs %6s  med %6s  p90 %6s  unc %6s  loss %3.0f%%  %s", fmtMS(t.hs), fmtMS(t.median()), fmtMS(t.p90()), fmtMS(t.uncachedMed()), t.loss()*100, t.egress)
				}
				fmt.Fprintln(w, line)
			}
		}
	}

	fmt.Fprintln(w)
	summary(w, res, rows)
	fmt.Fprintln(w, paint(cDim, T("legend", o.rounds, strings.Join(res.names, ", "))))
	if res.intercept {
		fmt.Fprintln(w, paint(cDim, T("legend_star")))
	}
	if src == "built-in" {
		fmt.Fprintln(w, paint(cDim, T("offline_list")))
	}
	fmt.Fprintln(w, paint(cDim, T("took", took.Round(time.Second), listName(src))))
}

// listName says where the resolver list came from: dns.1ms.dog, a file, or the copy built into the binary.
func listName(src string) string {
	if src == "built-in" || src == "offline" {
		return T("builtin")
	}
	return src
}

// gap is what goes between a label and its value: a space after "Saved:", nothing after a full-width colon ("已保存：").
func gap(label string) string {
	if strings.HasSuffix(label, "：") {
		return ""
	}
	return " "
}

// printMarkdown: the result as a Markdown table, to paste into a forum post or an issue.
func printMarkdown(w io.Writer, res *result, o runOpts, src string, took time.Duration) {
	rows := ranked(res)
	used := map[string]bool{}
	for _, t := range res.targets {
		used[t.proto] = true
	}
	head, sep := "| # | "+T("c_dns")+" |", "|---:|---|"
	var cols []string
	for _, p := range protos {
		if used[p] {
			cols = append(cols, p)
			head += " " + protoName[p] + " |"
			sep += "---:|"
		}
	}
	head += " " + T("c_uncached") + " | DNSSEC |"
	sep += "---:|:---:|"
	fmt.Fprintf(w, "**dnstest %s** · %s · %s %s\n\n%s\n%s\n", version, T("head", res.fam, len(rows), o.rounds), T("from"), res.from.String(), head, sep)
	plainMS := func(t *target) string {
		switch {
		case t == nil:
			return "·"
		case !t.up:
			return "×"
		case res.intercept && t.plain():
			return fmtMS(t.median()) + "*"
		case t.loss() > 0:
			return fmtMS(t.median()) + "!"
		}
		return fmtMS(t.median())
	}
	for i, r := range rows {
		n := "–"
		if r.best != nil {
			n = fmt.Sprint(i + 1)
		}
		line := "| " + n + " | " + r.r.Name + " |"
		for _, p := range cols {
			line += " " + plainMS(r.by[p]) + " |"
		}
		unc, sec := "–", ""
		if r.best != nil {
			unc = fmtMS(r.best.uncachedMed())
		}
		for _, t := range r.by {
			if t.dnssec == 1 {
				sec = "✓"
			}
		}
		fmt.Fprintf(w, "%s %s | %s |\n", line, unc, sec)
	}
	fmt.Fprintf(w, "\n%s\n", T("legend", o.rounds, strings.Join(res.names, ", ")))
	if res.intercept {
		fmt.Fprintln(w, "\n"+T("w_intercept"))
	}
	fmt.Fprintf(w, "\n%s · https://dns.1ms.dog\n", T("took", took.Round(time.Second), listName(src)))
}

func maxI8(a, b int8) int8 {
	if b > a {
		return b
	}
	return a
}

// summary: the fastest overall, the fastest encrypted one, and how the machine's own DNS compares.
func summary(w io.Writer, res *result, rows []*row) {
	var best, enc *target
	var sys []*row
	for _, r := range rows {
		if r.r.Group == "system" {
			sys = append(sys, r)
		}
		for _, t := range r.by {
			if !rankable(res, t) {
				continue
			}
			if best == nil || t.score() < best.score() {
				best = t
			}
			if !t.plain() && (enc == nil || t.score() < enc.score()) {
				enc = t
			}
		}
	}
	if best == nil {
		fmt.Fprintln(w, paint(cRed, T("none_up")))
		return
	}
	fmt.Fprintln(w, paint(cGreen, "› ")+T("best", paint(cBold, best.r.Name), protoName[best.proto], fmtMS(best.median()), fmtMS(best.uncachedMed())))
	if enc != nil && enc != best {
		fmt.Fprintln(w, paint(cGreen, "› ")+T("best_enc", paint(cBold, enc.r.Name), protoName[enc.proto], fmtMS(enc.median())))
	}
	for _, r := range sys {
		if r.best == nil {
			continue
		}
		d := r.best.score() - best.score()
		if d > 1 && r.best.r != best.r {
			fmt.Fprintln(w, paint(cCyan, "› ")+T("vs_system", r.r.Name, fmtMS(r.best.median()), best.r.Name, fmt.Sprintf("%.0f", d)))
		} else {
			fmt.Fprintln(w, paint(cCyan, "› ")+T("system_ok", r.r.Name, fmtMS(r.best.median())))
		}
	}
	if best != nil {
		if hint := setupHint(best); hint != "" {
			fmt.Fprintln(w, paint(cDim, "  "+hint))
		}
	}
}

// setupHint: what to enter in the system's DNS settings for the winner.
func setupHint(t *target) string {
	switch t.proto {
	case pUDP, pTCP:
		ips := t.r.UDP
		if len(t.r.UDP) == 0 || strings.Contains(t.ip, ":") {
			ips = t.r.UDP6
		}
		if len(ips) > 0 && t.r.Group != "system" {
			return T("hint_plain", strings.Join(ips, ", "))
		}
	case pDoT:
		return T("hint_dot", t.host)
	case pDoH:
		return T("hint_doh", t.url)
	case pDoQ:
		return T("hint_doq", "quic://"+t.host)
	}
	return ""
}

func printList(w io.Writer, l *list) {
	for _, r := range l.Resolvers {
		var has []string
		if len(r.UDP)+len(r.UDP6) > 0 {
			has = append(has, "udp", "tcp")
		}
		if r.DoT != nil {
			has = append(has, "dot")
		}
		if r.DoH != nil {
			has = append(has, "doh")
		}
		if r.DoQ != nil {
			has = append(has, "doq")
		}
		addr := strings.Join(r.UDP, " ")
		if addr == "" && r.DoH != nil {
			addr = r.DoH.URL
		}
		fmt.Fprintf(w, "%-12s %-22s %-22s %s\n", r.ID, r.Name, strings.Join(has, ","), addr)
	}
}

/* ---------- JSON ---------- */

type jsonTarget struct {
	Resolver  string    `json:"resolver"`
	Name      string    `json:"name"`
	Proto     string    `json:"proto"`
	Address   string    `json:"address"`
	Host      string    `json:"host,omitempty"`
	URL       string    `json:"url,omitempty"`
	Up        bool      `json:"up"`
	Error     string    `json:"error,omitempty"`
	Handshake *float64  `json:"handshake_ms,omitempty"`
	Median    *float64  `json:"median_ms,omitempty"`
	P90       *float64  `json:"p90_ms,omitempty"`
	Uncached  *float64  `json:"uncached_median_ms,omitempty"`
	Loss      float64   `json:"loss"`
	Score     *float64  `json:"score,omitempty"`
	DNSSEC    *bool     `json:"dnssec,omitempty"`
	ECS       *bool     `json:"ecs,omitempty"`
	NXRewrite bool      `json:"nxdomain_rewritten,omitempty"`
	Egress    string    `json:"egress,omitempty"`
	Samples   []float64 `json:"samples_ms,omitempty"`
}

func toJSON(res *result, src string, took time.Duration) any {
	f := func(d time.Duration) *float64 {
		if d < 0 {
			return nil
		}
		v := math.Round(ms(d)*100) / 100
		return &v
	}
	b := func(v int8) *bool {
		if v < 0 {
			return nil
		}
		x := v == 1
		return &x
	}
	var ts []jsonTarget
	for _, r := range ranked(res) {
		for _, p := range protos {
			t := r.by[p]
			if t == nil {
				continue
			}
			j := jsonTarget{Resolver: t.r.ID, Name: t.r.Name, Proto: p, Address: t.ip, Host: t.host, URL: t.url, Up: t.up, Error: t.err,
				Loss: math.Round(t.loss()*1000) / 1000, DNSSEC: b(t.dnssec), ECS: b(t.ecs), NXRewrite: t.nxRewrite, Egress: t.egress}
			if t.up {
				if t.hs > 0 {
					j.Handshake = f(t.hs)
				}
				j.Median, j.P90, j.Uncached = f(t.median()), f(t.p90()), f(t.uncachedMed())
				if s := t.score(); s >= 0 && rankable(res, t) {
					v := math.Round(s*100) / 100
					j.Score = &v
				}
				for _, d := range t.cached {
					j.Samples = append(j.Samples, math.Round(ms(d)*100)/100)
				}
			}
			ts = append(ts, j)
		}
	}
	return map[string]any{
		"version": version, "family": res.fam, "list": src, "names": res.names, "seconds": math.Round(took.Seconds()*10) / 10,
		"from": res.from, "intercepted": res.intercept, "injected": res.inject, "results": ts,
	}
}

/* ---------- progress ---------- */

type progLine struct {
	mu sync.Mutex
	w  io.Writer
}

func newProgLine(w io.Writer) *progLine { return &progLine{w: w} }

func (p *progLine) update(done, total int, t *target) {
	p.mu.Lock()
	defer p.mu.Unlock()
	pct := done * 100 / max(1, total)
	n := 24 * done / max(1, total)
	// the rest of the track is dim; without colors it is a thinner line, so the bar still shows how far it got
	rest := "━"
	if !useColor {
		rest = "─"
	}
	bar := paint(cCyan, strings.Repeat("━", n)) + paint(cDim, strings.Repeat(rest, 24-n))
	fmt.Fprintf(p.w, "\r\x1b[K  %s %3d%%  %s", bar, pct, paint(cDim, t.r.Name+" "+protoName[t.proto]))
}

func (p *progLine) clear() {
	p.mu.Lock()
	defer p.mu.Unlock()
	fmt.Fprint(p.w, "\r\x1b[K")
}
