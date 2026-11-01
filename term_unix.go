//go:build !windows

package main

import (
	"os"

	"golang.org/x/sys/unix"
)

// termCols is the width of the terminal on f, 0 when unknown.
func termCols(f *os.File) int {
	ws, err := unix.IoctlGetWinsize(int(f.Fd()), unix.TIOCGWINSZ)
	if err != nil {
		return 0
	}
	return int(ws.Col)
}

// enableVT is for Windows; other terminals read color codes anyway.
func enableVT() bool { return true }
