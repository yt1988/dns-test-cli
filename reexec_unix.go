//go:build !windows

package main

import (
	"os"
	"syscall"
)

// reexec replaces this process with the updated binary, keeping its arguments and environment.
func reexec(exe string, args []string) error { return syscall.Exec(exe, args, os.Environ()) }
