package main

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
)

// The menu runs when dnstest starts in a terminal with no options (as the installer starts it). On the first run it
// asks for a language and saves it. Then it asks what to test (or offers to change the language), the IP family, the
// number of rounds and whether to save the result. Enter keeps each default and 0 goes back a step (on the first
// question, 0 exits). It returns the options it built, which main runs and also prints, so the same test can be run
// again without the menu. Works like vps2cloud's.

type menu struct {
	in  *bufio.Reader
	out io.Writer
}

// What ask returns for 0 (back, or exit on the first question) and for the end of the input (Ctrl-D).
const back, end = 0, -1

func heading(s string) string { return paint(cCyan, "›") + " " + paint(cBold, s) }
func key(n int) string        { return paint(cCyan, strconv.Itoa(n)) }

func (m menu) line() (string, bool) {
	s, err := m.in.ReadString('\n')
	s = strings.TrimSpace(s)
	return s, !(s == "" && err != nil)
}

// question prints a title and numbered options, then a "0)" line (back, or exit on the first question).
func (m menu) question(title string, opts []string, first bool) {
	fmt.Fprintln(m.out, "\n"+heading(title))
	for i, o := range opts {
		fmt.Fprintf(m.out, "  %s) %s\n", key(i+1), o)
	}
	zero := T("back")
	if first {
		zero = T("exit")
	}
	fmt.Fprintf(m.out, "  %s) %s\n", key(0), paint(cDim, zero))
}

// ask reads a number from 0 to max; Enter gives def. It returns end when the input closes.
func (m menu) ask(def, max int) int {
	for {
		fmt.Fprintf(m.out, "%s %s: ", paint(cBold, T("choice")), paint(cDim, "["+strconv.Itoa(def)+"]"))
		s, ok := m.line()
		if !ok {
			return end
		}
		if s == "" {
			return def
		}
		if n, e := strconv.Atoi(s); e == nil && n >= 0 && n <= max {
			return n
		}
		fmt.Fprintln(m.out, "  "+paint(cYellow, T("num", max)))
	}
}

// pick reads a list like "1,3,5" or "2-4" of the numbered choices; "0" is back. It returns nil for back and
// end-of-input alike, with ok false only for the latter.
func (m menu) pick(prompt string, max int) (sel []int, ok bool) {
	for {
		fmt.Fprintf(m.out, "%s %s: ", paint(cBold, prompt), paint(cDim, "[0 = "+T("back")+"]"))
		s, ok := m.line()
		if !ok {
			return nil, false
		}
		if s == "0" {
			return nil, true
		}
		var out []int
		valid := s != ""
		for _, part := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' || r == '，' }) {
			a, b, isRange := strings.Cut(part, "-")
			lo, e1 := strconv.Atoi(a)
			hi := lo
			var e2 error
			if isRange {
				hi, e2 = strconv.Atoi(b)
			}
			if e1 != nil || e2 != nil || lo < 1 || hi > max || lo > hi {
				valid = false
				break
			}
			for i := lo; i <= hi; i++ {
				out = append(out, i)
			}
		}
		if valid && len(out) > 0 {
			return out, true
		}
		fmt.Fprintln(m.out, "  "+paint(cYellow, T("pickErr", max)))
	}
}

// hasRoute tells whether this machine can reach the internet over IPv6 (a UDP "connection" sends nothing).
var hasRoute = func(v6 bool) bool {
	addr := "8.8.8.8:53"
	if v6 {
		addr = "[2001:4860:4860::8888]:53"
	}
	c, err := net.DialTimeout("udp", addr, time.Second)
	if err != nil {
		return false
	}
	defer c.Close()
	ip := c.LocalAddr().(*net.UDPAddr).IP
	return ip != nil && !ip.IsLoopback() && !ip.IsLinkLocalUnicast()
}

// langLabel names the language item in the current language and in English, so it can be found in a language one
// cannot read.
func langLabel() string {
	if lang == "en" {
		return "Language"
	}
	return T("langItem") + " / Language"
}

// chooseLang lists the languages, sets the one picked and saves it. It returns back for 0 and end when the input
// closes.
func (m menu) chooseLang(first bool) int {
	title := langLabel()
	if first {
		title = "Language / 语言"
	}
	fmt.Fprintln(m.out, "\n"+heading(title))
	def := 1
	for i, l := range languages {
		item := fmt.Sprintf("%s) %s", key(i+1), langNames[l])
		fmt.Fprint(m.out, "  "+item+strings.Repeat(" ", max(1, 18-width(item))))
		if (i+1)%3 == 0 || i == len(languages)-1 {
			fmt.Fprintln(m.out)
		}
		if l == lang {
			def = i + 1
		}
	}
	zero := T("back")
	if first {
		zero = T("exit")
	}
	fmt.Fprintf(m.out, "  %s) %s\n", key(0), paint(cDim, zero))
	n := m.ask(def, len(languages))
	if n <= 0 {
		return n
	}
	lang = languages[n-1]
	c := loadConfig()
	c.Lang = lang
	if err := saveConfig(c); err == nil {
		fmt.Fprintln(m.out, paint(cGreen, "✓ "+T("langSaved", langNames[lang])))
	}
	return n
}

var groups = []string{"global", "cn", "region"}

// wizard returns the options for the run, or nil when the user exits or the input ends. askLang is set on the first
// run, before any language has been chosen.
func wizard(in io.Reader, out io.Writer, l *list, askLang bool) []string {
	m := menu{bufio.NewReader(in), out}
	if askLang {
		if m.chooseLang(true) <= 0 {
			return nil
		}
	}
	v6 := hasRoute(true)
	fams := "IPv4"
	if v6 {
		fams = "IPv4 + IPv6"
	}
	count := map[string]int{}
	for _, r := range l.Resolvers {
		count[r.Group]++
	}
	var scope, fam, rounds, save []string
	step := 0
	for {
		switch step {
		case 0:
			fmt.Fprintf(out, "\n%s\n%s\n", paint(cBold, T("title", version, len(l.Resolvers))), paint(cDim, T("machine", fams)))
			m.question(T("what"), []string{T("all", len(l.Resolvers)), T("byGroup"), T("byProto"), langLabel()}, true)
			switch n := m.ask(1, 4); n {
			case end, back:
				return nil
			case 1:
				scope = nil
			case 2:
				fmt.Fprintln(out, "\n"+heading(T("byGroup")))
				for i, g := range groups {
					fmt.Fprintf(out, "  %s) %s\n", key(i+1), T("g_"+g, count[g]))
				}
				sel, ok := m.pick(T("pickGroup"), len(groups))
				if !ok {
					return nil
				}
				if sel == nil {
					continue
				}
				var ids []string
				for _, i := range sel {
					ids = append(ids, groups[i-1])
				}
				scope = []string{"-g", strings.Join(ids, ",")}
			case 3:
				fmt.Fprintln(out, "\n"+heading(T("byProto")))
				for i, p := range protos {
					fmt.Fprintf(out, "  %s) %s\n", key(i+1), protoName[p]+paint(cDim, "  "+T("p_"+p)))
				}
				sel, ok := m.pick(T("pickProto"), len(protos))
				if !ok {
					return nil
				}
				if sel == nil {
					continue
				}
				var ids []string
				for _, i := range sel {
					ids = append(ids, protos[i-1])
				}
				scope = []string{"-p", strings.Join(ids, ",")}
			case 4:
				if m.chooseLang(false) == end {
					return nil
				}
				continue
			}
			step = 1
		case 1:
			if !v6 {
				fam, step = nil, 2
				continue
			}
			m.question(T("ip"), []string{T("v4"), T("v6")}, false)
			switch m.ask(1, 2) {
			case end:
				return nil
			case back:
				step = 0
				continue
			case 1:
				fam = nil
			case 2:
				fam = []string{"-6"}
			}
			step = 2
		case 2:
			m.question(T("rounds"), []string{T("quick"), T("std"), T("precise")}, false)
			switch m.ask(2, 3) {
			case end:
				return nil
			case back:
				step = 1
				if !v6 {
					step = 0
				}
				continue
			case 1:
				rounds = []string{"-n", "5"}
			case 2:
				rounds = nil
			case 3:
				rounds = []string{"-n", "20"}
			}
			step = 3
		case 3:
			m.question(T("saveQ"), []string{T("saveNo"), "Markdown (.md)", "JSON (.json)"}, false)
			switch m.ask(1, 3) {
			case end:
				return nil
			case back:
				step = 2
				continue
			case 1:
				save = nil
			case 2:
				save = []string{"-o", "md"}
			case 3:
				save = []string{"-o", "json"}
			}
			return append(append(append(append([]string{}, scope...), fam...), rounds...), save...)
		}
	}
}

// commandLine shows options as a command to type, quoting only what needs it.
func commandLine(args []string) string {
	parts := []string{"dnstest"}
	for _, a := range args {
		if strings.ContainsAny(a, " *?'\"") {
			a = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
		}
		parts = append(parts, a)
	}
	return strings.Join(parts, " ")
}
