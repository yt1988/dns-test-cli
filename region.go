package main

// Taiwan's name, the way 1ms-shell docs/tool-contract.md §5 asks: fixed in every language. Other places keep their
// ISO code in the "from" line; the JSON output keeps the code for every place, TW included.
var twName = map[string]string{"zh-CN": "中国台湾", "zh-TW": "中國台灣", "en": "Taiwan, China", "ja": "中国台湾", "ko": "중국 대만",
	"es": "Taiwán, China", "fr": "Taïwan, Chine", "de": "Taiwan, China", "pt": "Taiwan, China", "ru": "Тайвань, Китай"}

func regionName(cc string) string {
	if cc != "TW" {
		return cc
	}
	if s, ok := twName[lang]; ok {
		return s
	}
	return twName["en"]
}
