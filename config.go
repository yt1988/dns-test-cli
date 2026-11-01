package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// The saved choices live in the user's config directory (~/.config/dnstest/config.json on Linux).
type config struct {
	Lang      string `json:"lang,omitempty"`
	Latest    string `json:"latest,omitempty"`     // newest release seen
	CheckedAt int64  `json:"checked_at,omitempty"` // when it was looked up (Unix seconds)
}

func configPath() string {
	d, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(d, "dnstest", "config.json")
}

func loadConfig() config {
	var c config
	if p := configPath(); p != "" {
		if b, err := os.ReadFile(p); err == nil {
			json.Unmarshal(b, &c)
		}
	}
	return c
}

func saveConfig(c config) error {
	p := configPath()
	if p == "" {
		return fmt.Errorf("no config directory")
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(c, "", "  ")
	return os.WriteFile(p, append(b, '\n'), 0o644)
}

// isTTY tells whether w is a terminal (the menu, the update offer and the progress need one).
func isTTY(w any) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}
