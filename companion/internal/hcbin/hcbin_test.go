package hcbin

import (
	"errors"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"herdr-anywhere/companion/internal/testfake"
)

func fake(t *testing.T, dir, out string) string {
	return testfake.HC(t, dir, "hc", testfake.Spec{Version: out})
}

// lastResort is the folder Resolve tries after PATH: ~/.local/bin, or on
// Windows %LOCALAPPDATA%\Programs\herdr-core, which it points at home.
func lastResort(t *testing.T, home string) string {
	if runtime.GOOS == "windows" {
		local := filepath.Join(home, "AppData", "Local")
		t.Setenv("LOCALAPPDATA", local)
		return filepath.Join(local, "Programs", "herdr-core")
	}
	return filepath.Join(home, ".local", "bin")
}

func noPath(string) (string, error) { return "", exec.ErrNotFound }

func TestVersionMustStartWithHc(t *testing.T) {
	d := t.TempDir()
	good := fake(t, filepath.Join(d, "a"), "hc 0.1.13")
	bad := fake(t, filepath.Join(d, "b"), "hashcat 6.2")
	if v, err := Version(good); err != nil || v != "hc 0.1.13" {
		t.Fatalf("Version(good) = %q, %v", v, err)
	}
	if _, err := Version(bad); err == nil {
		t.Fatal("Version accepted a program that is not herdr-core's hc")
	}
	if _, err := Version(filepath.Join(d, "missing")); err == nil {
		t.Fatal("Version accepted a missing file")
	}
}

func TestResolveOrder(t *testing.T) {
	d := t.TempDir()
	home := filepath.Join(d, "home")
	homeHc := fake(t, lastResort(t, home), "hc 0.1.1")
	pathHc := fake(t, filepath.Join(d, "path"), "hc 0.1.2")
	flagHc := fake(t, filepath.Join(d, "flag"), "hc 0.1.3")
	wrong := fake(t, filepath.Join(d, "wrong"), "hc-other 1")
	onPath := func(p string) func(string) (string, error) {
		return func(string) (string, error) { return p, nil }
	}

	cases := []struct {
		name, explicit string
		look           func(string) (string, error)
		want, version  string
	}{
		{"flag wins", flagHc, onPath(pathHc), flagHc, "hc 0.1.3"},
		{"then PATH", "", onPath(pathHc), pathHc, "hc 0.1.2"},
		{"then the last resort", "", noPath, homeHc, "hc 0.1.1"},
		{"a foreign hc on PATH is skipped", "", onPath(wrong), homeHc, "hc 0.1.1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, v, err := Resolve(c.explicit, c.look, home)
			if err != nil || p != c.want || v != c.version {
				t.Fatalf("Resolve = %q, %q, %v; want %q, %q", p, v, err, c.want, c.version)
			}
		})
	}
}

func TestResolveExplicitBadPathFails(t *testing.T) {
	d := t.TempDir()
	home := filepath.Join(d, "home")
	fake(t, lastResort(t, home), "hc 0.1.1")
	wrong := fake(t, filepath.Join(d, "wrong"), "nope")
	if _, _, err := Resolve(wrong, noPath, home); err == nil {
		t.Fatal("an explicit --hc that is not hc must fail, not fall back")
	}
}

func TestResolveNothingFound(t *testing.T) {
	home := t.TempDir()
	lastResort(t, home)
	_, _, err := Resolve("", noPath, home)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

// Herdr's Windows installer (releases.herdr.dev/core/install.ps1, read
// 2026-11-07) puts herdr-core.exe in %LOCALAPPDATA%\Programs\herdr-core and on
// PATH, and makes hc.exe beside it only when no hc exists yet.
func TestCandidatesOnWindows(t *testing.T) {
	look := func(name string) (string, error) {
		if name == "herdr-core" {
			return `C:\elsewhere\herdr-core.exe`, nil
		}
		return "", exec.ErrNotFound
	}
	got := Candidates("windows", look, `D:\h`, `D:\appdata`)
	want := []string{
		`C:\elsewhere\herdr-core.exe`,
		filepath.Join(`D:\appdata`, "Programs", "herdr-core", "hc.exe"),
		filepath.Join(`D:\appdata`, "Programs", "herdr-core", "herdr-core.exe"),
	}
	if len(got) != len(want) {
		t.Fatalf("Candidates = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Candidates = %q, want %q", got, want)
		}
	}
}

func TestCandidatesElsewhereUnchanged(t *testing.T) {
	got := Candidates("darwin", func(string) (string, error) { return "/opt/hc", nil }, "/h", "")
	want := []string{"/opt/hc", filepath.Join("/h", ".local", "bin", "hc")}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("Candidates = %q, want %q", got, want)
	}
}
