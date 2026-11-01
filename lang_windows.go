//go:build windows

package main

import "golang.org/x/sys/windows"

// uiLangs: the display languages the user picked in Windows, most preferred first (zh-CN, ja-JP …).
func uiLangs() []string {
	l, err := windows.GetUserPreferredUILanguages(windows.MUI_LANGUAGE_NAME)
	if err != nil {
		return nil
	}
	return l
}
