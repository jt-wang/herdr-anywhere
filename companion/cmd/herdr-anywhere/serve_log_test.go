package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"herdr-anywhere/companion/internal/testfake"
)

// serve --log is how the Windows Run entry starts it, with no console: what it prints goes to the file, added to
// what an earlier run left there.
func TestServeLogGoesToTheFile(t *testing.T) {
	bin := t.TempDir()
	buildVersion(t, "0.0.1", filepath.Join(bin, "herdr-anywhere"))
	exe := filepath.Join(bin, "herdr-anywhere"+testfake.Ext())
	hc := testfake.HC(t, bin, "hc", testfake.Spec{Version: "hc 0.1.19"})
	dir := shortStateDir(t)
	log := filepath.Join(dir, "serve.log")
	os.WriteFile(log, []byte("an earlier run\n"), 0o600)
	cmd := exec.Command(exe, "serve", "--listen", "127.0.0.1:0", "--hc", hc, "--state", dir, "--log", log)
	out := &syncBuffer{}
	cmd.Stdout, cmd.Stderr = out, out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	waitFor(t, 20*time.Second, func() bool { return readServeURLs(dir) != nil }, "serve to answer", fileLog(log))
	b, _ := os.ReadFile(log)
	if !strings.HasPrefix(string(b), "an earlier run\n") || !strings.Contains(string(b), "listening on http://127.0.0.1:") {
		t.Fatalf("serve.log:\n%s", b)
	}
	if out.String() != "" {
		t.Fatalf("printed outside the log: %q", out)
	}
}

// fileLog shows a log file in a test's failure message.
type fileLog string

func (f fileLog) String() string { b, _ := os.ReadFile(string(f)); return string(b) }
