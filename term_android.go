//go:build android

package main

import "os"

// Android runs the CLI engine through JNI, so there is no terminal to inspect.
func termCols(*os.File) int { return 0 }
func enableVT() bool        { return false }
