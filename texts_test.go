package main

import (
	"regexp"
	"testing"
)

var verb = regexp.MustCompile(`%[sdv]`)

// Every language has every English key, with the same fmt verbs in the same order: a missing key would fall back to
// English, a wrong verb would print %!s(MISSING).
func TestTexts(t *testing.T) {
	for _, l := range languages {
		d, ok := texts[l]
		if !ok {
			t.Errorf("no texts for %s", l)
			continue
		}
		for k, en := range texts["en"] {
			s, ok := d[k]
			if !ok {
				t.Errorf("%s: missing %q", l, k)
				continue
			}
			if a, b := verb.FindAllString(en, -1), verb.FindAllString(s, -1); len(a) != len(b) {
				t.Errorf("%s: %q has verbs %v, English %v", l, k, b, a)
			} else {
				for i := range a {
					if a[i] != b[i] {
						t.Errorf("%s: %q has verbs %v, English %v", l, k, b, a)
						break
					}
				}
			}
		}
	}
}

func TestKnownLang(t *testing.T) {
	for in, want := range map[string]string{"zh_CN.UTF-8": "zh-CN", "zh_TW": "zh-TW", "zh-Hant-HK": "zh-TW", "ja_JP.UTF-8": "ja",
		"pt_BR": "pt", "C.UTF-8": "", "en_US": "en", "": ""} {
		if got := knownLang(in); got != want {
			t.Errorf("knownLang(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBuiltinList(t *testing.T) {
	l, err := parseList(builtinList)
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Resolvers) < 20 {
		t.Errorf("only %d resolvers", len(l.Resolvers))
	}
	for _, r := range l.Resolvers {
		if r.DoH == nil || r.DoH.URL == "" {
			t.Errorf("%s: no DoH", r.ID)
		}
	}
}
