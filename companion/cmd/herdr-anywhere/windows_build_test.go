package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// TestBuildsForWindows cross-compiles the companion, and type-checks every
// package's tests, for 64-bit Windows: Herdr ships a Windows x86_64 core and
// app (releases.herdr.dev/core/latest.json and app/latest.json, read
// 2026-11-07), so a Windows person can run Herdr and needs this program too.
// The flags a release build uses: no cgo, no VCS stamp.
func TestBuildsForWindows(t *testing.T) {
	if testing.Short() {
		t.Skip("cross-compiles the whole module")
	}
	if runtime.GOOS == "windows" {
		t.Skip("this is the Windows build")
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	env := append(os.Environ(), "GOOS=windows", "GOARCH=amd64", "CGO_ENABLED=0")
	for _, args := range [][]string{
		{"build", "-buildvcs=false", "-o", filepath.Join(t.TempDir(), "herdr-anywhere.exe"), "./cmd/herdr-anywhere"},
		{"vet", "./..."}, // compiles every _test.go too
	} {
		cmd := exec.Command("go", args...)
		cmd.Dir, cmd.Env = root, env
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("GOOS=windows go %v: %v\n%s", args[0], err, out)
		}
	}
}
