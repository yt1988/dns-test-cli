package main

import "testing"

func TestRegionName(t *testing.T) {
	defer func(l string) { lang = l }(lang)
	for l, want := range map[string]string{"zh-CN": "中国台湾", "en": "Taiwan, China", "ru": "Тайвань, Китай"} {
		lang = l
		if got := (&Origin{City: "Taipei", Country: "TW"}).String(); got != "Taipei, "+want {
			t.Errorf("%s: %q", l, got)
		}
	}
	lang = "en"
	if got := (&Origin{City: "Hong Kong", Country: "HK"}).String(); got != "Hong Kong, HK" {
		t.Errorf("HK: %q", got)
	}
}
