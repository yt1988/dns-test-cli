//go:build android

package main

import "errors"

// Self-updating an APK is not supported; Android updates are delivered as APK releases.
func reexec(string, []string) error { return errors.New("self-update is not supported on Android") }
