package main

import (
	"bytes"
	"debug/elf"
	"debug/macho"
	"debug/pe"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"herdr-anywhere/companion/internal/update"
)

// The builds below run in parallel: each target compiles modernc.org/sqlite on its own, and one after another they took
// the package past go test's 10 minutes on a CI Mac. Each writes into its own t.TempDir(); Go's build cache is safe
// for several go commands at once.

// releaseBuild build a release's file: no cgo (set in the env), no VCS stamp, no local paths, no symbols.
var releaseBuild = []string{"-buildvcs=false", "-trimpath", "-ldflags=-s -w"}

// buildFor builds the companion for goos/goarch under the name releases and update.AssetFor give it, with flags.
func buildFor(t *testing.T, goos, goarch string, flags []string, vet bool) string {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	env := append(os.Environ(), "GOOS="+goos, "GOARCH="+goarch, "CGO_ENABLED=0")
	name, err := releaseName(goos, goarch)
	if err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(t.TempDir(), name)
	steps := [][]string{append(append([]string{"build"}, flags...), "-o", exe, "./cmd/herdr-anywhere")}
	if vet {
		steps = append(steps, []string{"vet", "./..."}) // compiles every _test.go too
	}
	for _, args := range steps {
		cmd := exec.Command("go", args...)
		cmd.Dir, cmd.Env = root, env
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("GOOS=%s GOARCH=%s go %v: %v\n%s", goos, goarch, args[0], err, out)
		}
	}
	return exe
}

// releaseName is goos/goarch's file name in a release, an error for a target releases have no file for.
func releaseName(goos, goarch string) (string, error) {
	if name := update.AssetFor(goos, goarch); name != "" {
		return name, nil
	}
	return "", fmt.Errorf("%s/%s: releases have no file for it", goos, goarch)
}

// pathCheckTarget is the target the path check builds: this computer's own when releases have a file for it (the
// packages are mostly built already, for the tests), else linux/amd64.
func pathCheckTarget(goos, goarch string) (string, string) {
	if update.AssetFor(goos, goarch) != "" {
		return goos, goarch
	}
	return "linux", "amd64"
}

// localPath is the first of this checkout's folder and Go's module cache that file holds, "" for neither: a build
// without -trimpath holds both. (A home folder alone is no test: "/root" is in every Go program.)
func localPath(t *testing.T, file string) string {
	t.Helper()
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	root, _ := filepath.Abs("../../..")
	modcache, _ := exec.Command("go", "env", "GOMODCACHE").Output()
	for _, p := range []string{root, strings.TrimSpace(string(modcache))} {
		// A build on Windows writes its paths with "/" (Go's cmd/internal/objabi AbsFile, as of Go
		// 1.26.3): looking for "C:\a\…" alone found nothing on CI's Windows runners.
		if len(p) > 1 && (bytes.Contains(b, []byte(p)) || bytes.Contains(b, []byte(strings.ReplaceAll(p, `\`, "/")))) {
			return p
		}
	}
	return ""
}

// The path check finds the paths a build without -trimpath leaves in the file, so a clean result means something.
func TestThePathCheckFindsAnUntrimmedBuild(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the companion")
	}
	t.Parallel()
	goos, goarch := pathCheckTarget(runtime.GOOS, runtime.GOARCH)
	exe := buildFor(t, goos, goarch, []string{"-buildvcs=false"}, false)
	root, _ := filepath.Abs("../../..")
	if p := localPath(t, exe); p != root {
		t.Fatalf("a build without -trimpath: the path check found %q, want this checkout, %s", p, root)
	}
}

// windows/arm64 has no file in a release: a build for it once got "" for a name and wrote over its own temp folder.
func TestATargetWithoutAReleaseFile(t *testing.T) {
	if name, err := releaseName("windows", "arm64"); err == nil || !strings.Contains(err.Error(), "windows/arm64") {
		t.Errorf("releaseName(windows, arm64) = %q, %v; want an error naming windows/arm64", name, err)
	}
	if goos, goarch := pathCheckTarget("windows", "arm64"); update.AssetFor(goos, goarch) == "" {
		t.Errorf("the path check builds %s/%s, which has no release file", goos, goarch)
	}
	if goos, goarch := pathCheckTarget("darwin", "arm64"); goos+"/"+goarch != "darwin/arm64" {
		t.Errorf("on darwin/arm64 the path check builds %s/%s, want this computer's own target", goos, goarch)
	}
}

// TestBuildsForWindows cross-compiles the companion, and type-checks every
// package's tests, for 64-bit Windows: Herdr ships a Windows x86_64 core and
// app (releases.herdr.dev/core/latest.json and app/latest.json), so a Windows person can run Herdr and needs this program too.
// It builds the release's file the way a release does, and checks it is an x64 Windows program.
func TestBuildsForWindows(t *testing.T) {
	if testing.Short() {
		t.Skip("cross-compiles the whole module")
	}
	if runtime.GOOS == "windows" {
		t.Skip("this is the Windows build")
	}
	t.Parallel()
	exe := buildFor(t, "windows", "amd64", releaseBuild, true)
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
	if p := localPath(t, exe); p != "" {
		t.Fatalf("the file holds %s", p)
	}
}

// herdr-core ships macOS aarch64 and x86_64 builds (releases.herdr.dev/core/latest.json 0.1.19), so releases carry a
// companion for Apple silicon and one for Intel Macs. Each file is a Mac program for its own processor, with no local paths.
func TestBuildsForMac(t *testing.T) {
	if testing.Short() {
		t.Skip("cross-compiles the companion")
	}
	t.Parallel()
	for arch, cpu := range map[string]macho.Cpu{"arm64": macho.CpuArm64, "amd64": macho.CpuAmd64} {
		exe := buildFor(t, "darwin", arch, releaseBuild, arch == "amd64")
		if want := "herdr-anywhere-macos-" + arch; filepath.Base(exe) != want {
			t.Fatalf("the release file is named %q, want %q", filepath.Base(exe), want)
		}
		f, err := macho.Open(exe)
		if err != nil {
			t.Fatal(err)
		}
		if f.Cpu != cpu {
			t.Errorf("%s: cpu %v", arch, f.Cpu)
		}
		f.Close()
		if p := localPath(t, exe); p != "" {
			t.Errorf("%s: the file holds %s", arch, p)
		}
	}
}

// Herdr ships Linux x86_64 and aarch64 builds (releases.herdr.dev/app/latest.json and core/latest.json).
// Each release file is a static program, so it runs whatever C library the computer has.
func TestBuildsForLinux(t *testing.T) {
	if testing.Short() {
		t.Skip("cross-compiles the companion")
	}
	t.Parallel()
	for arch, machine := range map[string]elf.Machine{"amd64": elf.EM_X86_64, "arm64": elf.EM_AARCH64} {
		exe := buildFor(t, "linux", arch, releaseBuild, false)
		if want := "herdr-anywhere-linux-" + arch; filepath.Base(exe) != want {
			t.Fatalf("the release file is named %q, want %q", filepath.Base(exe), want)
		}
		f, err := elf.Open(exe)
		if err != nil {
			t.Fatal(err)
		}
		if f.Machine != machine {
			t.Errorf("%s: machine %v", arch, f.Machine)
		}
		for _, p := range f.Progs {
			if p.Type == elf.PT_INTERP || p.Type == elf.PT_DYNAMIC {
				t.Errorf("%s: needs a dynamic loader (%v)", arch, p.Type)
			}
		}
		f.Close()
		if p := localPath(t, exe); p != "" {
			t.Errorf("%s: the file holds %s", arch, p)
		}
	}
}
