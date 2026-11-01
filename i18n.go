package main

import (
	"fmt"
	"os"
	"strings"
)

// The languages of the site, so a link from a page and the tool it hands out speak the same one.
var languages = []string{"en", "zh-CN", "zh-TW", "ja", "ko", "es", "fr", "de", "pt", "ru"}

var lang = "en"

// T looks up a text in the current language and fills it like fmt.Sprintf. A test makes sure every language has
// every key, so the English fallback here is never used.
func T(key string, a ...any) string {
	s, ok := texts[lang][key]
	if !ok {
		s = texts["en"][key]
	}
	if len(a) > 0 {
		return fmt.Sprintf(s, a...)
	}
	return s
}

func knownLang(code string) string {
	code = strings.ReplaceAll(strings.SplitN(strings.SplitN(code, ".", 2)[0], "@", 2)[0], "_", "-")
	low := strings.ToLower(code)
	switch {
	case low == "":
		return ""
	case strings.HasPrefix(low, "zh"):
		if strings.Contains(low, "tw") || strings.Contains(low, "hk") || strings.Contains(low, "mo") || strings.Contains(low, "hant") {
			return "zh-TW"
		}
		return "zh-CN"
	}
	base := strings.SplitN(low, "-", 2)[0]
	for _, l := range languages {
		if l == base {
			return l
		}
	}
	return ""
}

// pickLang: --lang, then DNSTEST_LANG, then the system's (LC_ALL, LC_MESSAGES, LANG; the UI language on Windows).
func pickLang(flag string) string {
	for _, c := range append([]string{flag, os.Getenv("DNSTEST_LANG"), os.Getenv("LC_ALL"), os.Getenv("LC_MESSAGES"), os.Getenv("LANG")}, uiLangs()...) {
		if l := knownLang(c); l != "" {
			return l
		}
	}
	return "en"
}
