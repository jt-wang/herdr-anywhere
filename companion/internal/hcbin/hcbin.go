// Package hcbin finds herdr-core's `hc`: --hc, else `hc` on PATH, else
// ~/.local/bin/hc; on Windows, after `hc` on PATH, herdr-core on PATH and
// the folder Herdr's installer uses. A candidate counts only if `hc --version` starts with
// "hc " (the check herdr-app's core_bin.rs makes), so another program named
// hc is skipped.
package hcbin

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

var ErrNotFound = errors.New("herdr-core's hc not found (tried --hc, PATH, ~/.local/bin/hc; on Windows also herdr-core on PATH and %LOCALAPPDATA%\\Programs\\herdr-core)")

// Version runs `path --version` and returns its trimmed output.
func Version(path string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "--version").Output()
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
// Herdr's Windows installer (releases.herdr.dev/core/install.ps1) puts
// herdr-core.exe in %LOCALAPPDATA%\Programs\herdr-core and on PATH, and links
// hc.exe to it only when there's no hc yet; both print "hc <version>".
func Candidates(goos string, lookPath func(string) (string, error), home, localAppData string) []string {
	var candidates []string
	if p, err := lookPath("hc"); err == nil {
		candidates = append(candidates, p)
	}
	if goos == "windows" {
		if p, err := lookPath("herdr-core"); err == nil {
			candidates = append(candidates, p)
		}
		if localAppData != "" {
			dir := filepath.Join(localAppData, "Programs", "herdr-core")
			candidates = append(candidates, filepath.Join(dir, "hc.exe"), filepath.Join(dir, "herdr-core.exe"))
		}
		return candidates
	}
	if home != "" {
		candidates = append(candidates, filepath.Join(home, ".local", "bin", "hc"))
	}
	return candidates
}
