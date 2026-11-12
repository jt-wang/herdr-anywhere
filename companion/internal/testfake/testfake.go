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
	// Calls, when set, is a file each call's arguments are added to, joined by spaces, one line a call.
	Calls string `json:"calls,omitempty"`
	// On says what a subcommand (the first argument) does: the first of its replies whose If file
	// exists, or that has no If. "{2}" in a reply stands for the second argument.
	On map[string][]Reply `json:"on,omitempty"`
	// Else is the exit code of a call whose subcommand On has no reply for.
	Else int `json:"else,omitempty"`
}

// Reply is one answer of a fake to a subcommand, done in this order: Write, Append, Print, Fail, Exec, Cat.
type Reply struct {
	If     string `json:"if,omitempty"`     // a file that must exist for this reply ("{2}" too)
	Write  string `json:"write,omitempty"`  // a file the call's arguments are written to, as one line
	Append string `json:"append,omitempty"` // a file the call's arguments are added to, as one line
	Print  string `json:"print,omitempty"`  // printed, with a newline after
	Cat    string `json:"cat,omitempty"`    // a file printed as it is
	// Fail: a file printed to stderr, and exit 5, as hc says an error (hc 0.1.19: {"error":{"kind":…}} on stderr).
	Fail string `json:"fail,omitempty"`
	// Sleep: a Go duration waited first, as an hc that is slow to answer.
	Sleep string `json:"sleep,omitempty"`
	// Exec, for `exec --on M --json …`: the program runs here with root/M as its home (and TMPDIR empty), printed
	// as hc's result; a machine without a folder under root is unknown (exit 4, as hc).
	Exec string `json:"exec,omitempty"`
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

// HC puts a fake hc named name (plus Ext) in dir, doing what s says, and returns its path. It keeps s in
// dir/name.json, so a test keeps its own files under other names.
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

// ADBSpec is what a fake adb does, for the e2e scripts' helpers (app/e2e/android-lib.sh).
type ADBSpec struct {
	// Dumps are the exit codes of the first `shell uiautomator dump` calls (137: killed on the phone); later ones,
	// and a 0, succeed. A failed dump leaves the last screen, as on a phone.
	Dumps []int `json:"dumps,omitempty"`
	// Screens are what `shell cat /sdcard/hm.xml` prints after the first, second, … successful dump; the last repeats.
	Screens []string `json:"screens,omitempty"`
	// Calls, when set, is a file each call's arguments are added to, joined by spaces, one line a call.
	Calls string `json:"calls,omitempty"`
}

var (
	adbOnce  sync.Once
	adbBuilt string
	adbErr   error
)

// ADB puts a fake adb in dir, doing what s says, and returns its path. It keeps s and its counts in dir/adb.*.
func ADB(t testing.TB, dir string, s ADBSpec) string {
	t.Helper()
	adbOnce.Do(func() {
		d, e := os.MkdirTemp("", "fakeadb")
		if e != nil {
			adbErr = e
			return
		}
		adbBuilt = filepath.Join(d, "fakeadb"+Ext())
		if out, e := exec.Command("go", "build", "-o", adbBuilt, "herdr-anywhere/companion/internal/testfake/fakeadb").CombinedOutput(); e != nil {
			adbErr = &buildError{e, out}
		}
	})
	if adbErr != nil {
		t.Fatal(adbErr)
	}
	b, err := os.ReadFile(adbBuilt)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "adb"+Ext())
	if err := os.WriteFile(p, b, 0o755); err != nil {
		t.Fatal(err)
	}
	j, _ := json.Marshal(s)
	if err := os.WriteFile(filepath.Join(dir, "adb.json"), j, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}
