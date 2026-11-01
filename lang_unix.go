//go:build !windows

package main

// uiLangs is for Windows; elsewhere the environment says the language.
func uiLangs() []string { return nil }
