// Package hcbin finds herdr-core's `hc`: --hc, else `hc` on PATH, else
// herdr-core on PATH, else the folder Herdr's installer uses (~/.local/bin, on
// Windows %LOCALAPPDATA%\Programs\herdr-core), hc there before herdr-core. A candidate counts only if `hc --version` starts with
// "hc " (the check herdr-app's core_bin.rs makes), so another program named
// hc is skipped.
package hcbin

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"herdr-anywhere/companion/internal/proc"
)

var ErrNotFound = errors.New("herdr-core's hc not found (tried --hc, hc and herdr-core on PATH, and the same in ~/.local/bin, on Windows in %LOCALAPPDATA%\\Programs\\herdr-core)")

// Version runs `path --version` and returns its trimmed output.
func Version(path string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := proc.CommandContext(ctx, path, "--version").Output()
	if err != nil {
		return "", fmt.Errorf("%s --version: %w", path, err)
	}
	text := strings.TrimSpace(string(out))
	if !strings.HasPrefix(text, "hc ") {
		return "", fmt.Errorf("%s --version printed %q, not \"hc <version>\"", path, text)
	}
	return text, nil
}

// Resolve returns the hc to run and its version. An explicit path must work;
// it never falls back.
func Resolve(explicit string, lookPath func(string) (string, error), home string) (string, string, error) {
	if explicit != "" {
		v, err := Version(explicit)
		if err != nil {
			return "", "", err
		}
		return explicit, v, nil
	}
	for _, p := range Candidates(runtime.GOOS, lookPath, home, os.Getenv("LOCALAPPDATA")) {
		if v, err := Version(p); err == nil {
			return p, v, nil
		}
	}
	return "", "", ErrNotFound
}

// Candidates lists where hc may be on goos, in the order they are tried.
// Herdr's installers (releases.herdr.dev/core/install.sh and install.ps1) put
// herdr-core in ~/.local/bin, on Windows herdr-core.exe in
// %LOCALAPPDATA%\Programs\herdr-core and on PATH, and link hc to it only when
// there's no hc yet (herbstluftwm ships one); both print "hc <version>".
func Candidates(goos string, lookPath func(string) (string, error), home, localAppData string) []string {
	var candidates []string
	for _, name := range []string{"hc", "herdr-core"} {
		if p, err := lookPath(name); err == nil {
			candidates = append(candidates, p)
		}
	}
	dir, ext := "", ""
	if goos == "windows" {
		if localAppData != "" {
			dir, ext = filepath.Join(localAppData, "Programs", "herdr-core"), ".exe"
		}
	} else if home != "" {
		dir = filepath.Join(home, ".local", "bin")
	}
	if dir != "" {
		candidates = append(candidates, filepath.Join(dir, "hc"+ext), filepath.Join(dir, "herdr-core"+ext))
	}
	return candidates
}
