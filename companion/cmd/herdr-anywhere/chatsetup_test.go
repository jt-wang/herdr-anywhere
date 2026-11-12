package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"herdr-anywhere/companion/internal/testfake"
)

func piCmd(t *testing.T, hc string, args ...string) (int, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := run(append(append([]string{"chat-setup"}, args...), "--hc", hc), &out, &errb)
	return code, out.String() + errb.String()
}

// On the computer, the same as the phone's Install and Remove: one step each, nothing left after remove.
func TestChatSetupCommand(t *testing.T) {
	d := t.TempDir()
	dir := filepath.Join(d, "pi-agent")
	os.MkdirAll(dir, 0o755)
	t.Setenv("PI_CODING_AGENT_DIR", dir)
	// A folder with a backslash, as every path on Windows has: the file has the path as a string, escaped.
	hc := testfake.HC(t, filepath.Join(d, `bin\hc`), "hc", testfake.Spec{Version: "hc 0.1.20"})
	file := filepath.Join(dir, "extensions", "herdr-anywhere.ts")

	if code, out := piCmd(t, hc, "status"); code != 0 || !strings.Contains(out, "pi") || !strings.Contains(out, "not installed") {
		t.Fatalf("status: %d %s", code, out)
	}
	if code, out := piCmd(t, hc, "install", "pi"); code != 0 || !strings.Contains(out, file) {
		t.Fatalf("install: %d %s", code, out)
	}
	q, _ := json.Marshal(hc)
	if b, _ := os.ReadFile(file); !strings.Contains(string(b), `const HC = `+string(q)+`;`) {
		t.Fatalf("the file doesn't call this hc")
	}
	if code, out := piCmd(t, hc, "status"); code != 0 || !strings.Contains(out, "installed") || strings.Contains(out, "not installed") {
		t.Fatalf("status after: %d %s", code, out)
	}
	if code, out := piCmd(t, hc, "remove", "pi"); code != 0 || !strings.Contains(out, "removed") {
		t.Fatalf("remove: %d %s", code, out)
	}
	if _, err := os.Stat(filepath.Dir(file)); !os.IsNotExist(err) {
		t.Fatalf("left %s", filepath.Dir(file))
	}
}

// A changed file is not touched without --force.
func TestChatSetupCommandLeavesAChangedFile(t *testing.T) {
	d := t.TempDir()
	dir := filepath.Join(d, "pi-agent")
	file := filepath.Join(dir, "extensions", "herdr-anywhere.ts")
	os.MkdirAll(filepath.Dir(file), 0o755)
	os.WriteFile(file, []byte("// mine\n"), 0o644)
	t.Setenv("PI_CODING_AGENT_DIR", dir)
	hc := testfake.HC(t, d, "hc", testfake.Spec{Version: "hc 0.1.20"})
	if code, out := piCmd(t, hc, "remove", "pi"); code == 0 || !strings.Contains(out, "--force") {
		t.Fatalf("remove: %d %s", code, out)
	}
	if b, _ := os.ReadFile(file); string(b) != "// mine\n" {
		t.Fatalf("touched")
	}
	if code, out := piCmd(t, hc, "remove", "pi", "--force"); code != 0 {
		t.Fatalf("forced: %d %s", code, out)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatalf("still there")
	}
}

// Without Pi on the computer nothing is made.
func TestChatSetupCommandWithoutTheAgent(t *testing.T) {
	d := t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", filepath.Join(d, "pi-agent"))
	hc := testfake.HC(t, d, "hc", testfake.Spec{Version: "hc 0.1.20"})
	if code, out := piCmd(t, hc, "install", "pi"); code == 0 || !strings.Contains(out, "Pi") {
		t.Fatalf("install: %d %s", code, out)
	}
	if _, err := os.Stat(filepath.Join(d, "pi-agent")); !os.IsNotExist(err) {
		t.Fatalf("made Pi's folder")
	}
}

func TestChatSetupCommandNeedsAKnownAgent(t *testing.T) {
	d := t.TempDir()
	hc := testfake.HC(t, d, "hc", testfake.Spec{Version: "hc 0.1.20"})
	if code, out := piCmd(t, hc, "install", "nope"); code == 0 || !strings.Contains(out, "pi") {
		t.Fatalf("unknown agent: %d %s", code, out)
	}
	if code, _ := piCmd(t, hc, "install"); code == 0 {
		t.Fatalf("no agent named")
	}
}
