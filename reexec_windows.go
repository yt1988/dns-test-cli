//go:build windows

package main

import "errors"

// reexec is not possible on Windows: the caller asks to run the tool again.
func reexec(string, []string) error { return errors.New("restart needed") }
