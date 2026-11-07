package main

import (
	"bytes"
	"debug/pe"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"herdr-anywhere/companion/internal/update"
)

// TestBuildsForWindows cross-compiles the companion, and type-checks every
// package's tests, for 64-bit Windows: Herdr ships a Windows x86_64 core and
// app (releases.herdr.dev/core/latest.json and app/latest.json, read
// 2026-11-07), so a Windows person can run Herdr and needs this program too.
// It builds the release's file the way a release does (no cgo, no VCS stamp, no local paths, no symbols), under
// the name releases and update.AssetFor give it, and checks it is an x64 Windows program.
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
	exe := filepath.Join(t.TempDir(), update.AssetFor("windows", "amd64"))
	for _, args := range [][]string{
		{"build", "-buildvcs=false", "-trimpath", "-ldflags=-s -w", "-o", exe, "./cmd/herdr-anywhere"},
		{"vet", "./..."}, // compiles every _test.go too
	} {
		cmd := exec.Command("go", args...)
		cmd.Dir, cmd.Env = root, env
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("GOOS=windows go %v: %v\n%s", args[0], err, out)
		}
	}
	if filepath.Base(exe) != "herdr-anywhere-windows-amd64.exe" {
		t.Fatalf("the release file is named %q", filepath.Base(exe))
	}
	f, err := pe.Open(exe)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if f.Machine != pe.IMAGE_FILE_MACHINE_AMD64 {
		t.Fatalf("machine %#x", f.Machine)
	}
	if b, _ := os.ReadFile(exe); bytes.Contains(b, []byte(root)) {
		t.Fatal("the file holds this computer's path to the source")
	}
}
