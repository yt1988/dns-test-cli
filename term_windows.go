//go:build windows

package main

import (
	"os"

	"golang.org/x/sys/windows"
)

// termCols is the width of the console on f, 0 when unknown.
func termCols(f *os.File) int {
	var info windows.ConsoleScreenBufferInfo
	if err := windows.GetConsoleScreenBufferInfo(windows.Handle(f.Fd()), &info); err != nil {
		return 0
	}
	return int(info.Window.Right - info.Window.Left + 1)
}

// enableVT asks the console to read color and cursor codes (Windows 10 and later). It reports whether the console on
// stdout and stderr will, or true when they are not consoles (pipes, files).
func enableVT() bool {
	ok := true
	for _, f := range []*os.File{os.Stdout, os.Stderr} {
		var mode uint32
		h := windows.Handle(f.Fd())
		if windows.GetConsoleMode(h, &mode) != nil {
			continue
		}
		if windows.SetConsoleMode(h, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING) != nil {
			ok = false
		}
	}
	return ok
}
