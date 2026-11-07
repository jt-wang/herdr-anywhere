// Package testfake gives tests a stand-in hc and short temp dirs that work on
// macOS, Linux and Windows.
package testfake

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
)

// Spec is what a fake hc does.
type Spec struct {
	// Version is printed for --version; empty prints nothing.
	Version string `json:"version,omitempty"`
	// Connect is what `connect` does: "echo" copies stdin to stdout (writing its pid to
	// $HM_PIDFILE when set), "print:<text>" prints text; empty does nothing.
	Connect string `json:"connect,omitempty"`
	// Exit is the exit code for every call.
	Exit int `json:"exit,omitempty"`
}

var (
	once  sync.Once
	built string
	err   error
)

func build() (string, error) {
	once.Do(func() {
		dir, e := os.MkdirTemp("", "fakehc")
		if e != nil {
			err = e
			return
		}
		built = filepath.Join(dir, "fakehc"+Ext())
		out, e := exec.Command("go", "build", "-o", built, "herdr-anywhere/companion/internal/testfake/fakehc").CombinedOutput()
		if e != nil {
			err = &buildError{e, out}
		}
	})
	return built, err
}

type buildError struct {
	err error
	out []byte
}

func (b *buildError) Error() string {
	return "building fakehc: " + b.err.Error() + "\n" + string(b.out)
}

// Ext is what a program's file name ends with here: ".exe" on Windows.
func Ext() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

// HC puts a fake hc named name (plus Ext) in dir, doing what s says, and returns its path.
func HC(t testing.TB, dir, name string, s Spec) string {
	t.Helper()
	src, err := build()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, name+Ext())
	if err := os.WriteFile(p, b, 0o755); err != nil {
		t.Fatal(err)
	}
	j, _ := json.Marshal(s)
	if err := os.WriteFile(filepath.Join(dir, name+".json"), j, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// ShortDir is a new temp dir with a short path, removed after the test: a
// unix socket's path may hold 103 bytes on macOS, and t.TempDir() there is longer.
func ShortDir(t testing.TB, prefix string) string {
	t.Helper()
	base := "/tmp"
	if runtime.GOOS == "windows" {
		base = "" // the system temp dir, under the person's profile
	}
	dir, err := os.MkdirTemp(base, prefix)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

// PermBits says files here have Unix permission bits. Windows has none (os.Stat
// shows 0666 or 0444 there); who can open a file is its ACL.
func PermBits() bool { return runtime.GOOS != "windows" }

var (
	releasesMu sync.Mutex
	releases   = map[string][]byte{}
)

// Release is a program that stands in for a released companion: `--version` prints "herdr-anywhere <version>", or,
// with exit set, exits with that code; `wait` keeps it running. A real program on every OS, where a #!/bin/sh script
// doesn't run on Windows.
func Release(t testing.TB, version string, exit int) []byte {
	t.Helper()
	key := fmt.Sprintf("%s/%d", version, exit)
	releasesMu.Lock()
	defer releasesMu.Unlock()
	if b, ok := releases[key]; ok {
		return b
	}
	out := filepath.Join(t.TempDir(), "release"+Ext())
	flags := "-X main.version=" + version
	if exit != 0 {
		flags += fmt.Sprintf(" -X main.exit=%d", exit)
	}
	if b, err := exec.Command("go", "build", "-ldflags="+flags, "-o", out, "herdr-anywhere/companion/internal/testfake/fakerelease").CombinedOutput(); err != nil {
		t.Fatalf("building fakerelease: %v\n%s", err, b)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	releases[key] = b
	return b
}
