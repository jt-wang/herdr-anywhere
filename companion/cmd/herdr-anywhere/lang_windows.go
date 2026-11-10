package main

import "golang.org/x/sys/windows"

// windowsUILanguages: the display languages in Settings > Time & language, first the one Windows shows.
func windowsUILanguages() ([]string, error) {
	return windows.GetUserPreferredUILanguages(windows.MUI_LANGUAGE_NAME)
}
