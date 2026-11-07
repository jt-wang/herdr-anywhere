package testfake

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestFakeHC(t *testing.T) {
	dir := t.TempDir()
	v := HC(t, dir, "hc", Spec{Version: "hc 9.9.9", Connect: "echo"})
	if out, err := exec.Command(v, "--version").Output(); err != nil || string(out) != "hc 9.9.9\n" {
		t.Fatalf("--version = %q, %v", out, err)
	}
	pidfile := filepath.Join(dir, "pid")
	cmd := exec.Command(v, "connect")
	cmd.Env = append(os.Environ(), "HM_PIDFILE="+pidfile)
	cmd.Stdin = strings.NewReader("ping")
	if out, err := cmd.Output(); err != nil || string(out) != "ping" {
		t.Fatalf("connect = %q, %v", out, err)
	}
	if b, err := os.ReadFile(pidfile); err != nil || len(strings.TrimSpace(string(b))) == 0 {
		t.Fatalf("pidfile %q, %v", b, err)
	}

	bye := HC(t, filepath.Join(dir, "b"), "hc", Spec{Version: "hc 9", Connect: "print:bye"})
	if out, err := exec.Command(bye, "connect").Output(); err != nil || string(out) != "bye" {
		t.Fatalf("print connect = %q, %v", out, err)
	}
	fails := HC(t, filepath.Join(dir, "c"), "hc", Spec{Exit: 1})
	if err := exec.Command(fails, "exec").Run(); err == nil {
		t.Fatal("Exit: 1 exited 0")
	}
}
