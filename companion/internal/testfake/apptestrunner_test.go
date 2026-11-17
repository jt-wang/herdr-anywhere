package testfake

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The real wrapper must preserve a runner's exit status even when it leaves no output.
func TestAppTestRunnerStatus(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test.sh runs on Unix CI")
	}
	script, err := filepath.Abs("../../../app/scripts/test.sh")
	if err != nil {
		t.Fatal(err)
	}
	// Copy the real wrapper and one real source check into an isolated tree. The
	// other repository checks are unrelated to reporting a child process's status.
	app := t.TempDir()
	if err := os.Mkdir(filepath.Join(app, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"test.sh", "check-awaited.sh"} {
		data, err := os.ReadFile(filepath.Join(filepath.Dir(script), name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(app, "scripts", name), data, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	script = filepath.Join(app, "scripts", "test.sh")
	for _, c := range []struct {
		name   string
		code   int
		output string
		failed bool
	}{
		{"pass", 0, "58 tests: 58 passed, 0 failed", false},
		{"silent 139", 139, "", true},
		{"silent 137", 137, "", true},
		{"assertion", 1, "assertion failed", true},
		{"late rejection", 0, "unhandled rejection: the app is closed", true},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			calls := filepath.Join(dir, "calls")
			HC(t, dir, "nsh", Spec{Exit: c.code, Calls: calls, On: map[string][]Reply{"test": {{Print: c.output}}}})
			cmd := exec.Command("sh", script, "first.test.ts", "second.test.ts")
			cmd.Env = append(os.Environ(), "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			out, err := cmd.CombinedOutput()
			if (err != nil) != c.failed {
				t.Fatalf("exit: %v; output: %s", err, out)
			}
			if c.failed {
				want := fmt.Sprintf("first.test.ts: FAILED (nsh exit %d)", c.code)
				if !strings.Contains(string(out), want) {
					t.Errorf("missing %q in %q", want, out)
				}
			}
			if c.output != "" && !strings.Contains(string(out), c.output) {
				t.Errorf("lost runner output: %s", out)
			}
			got, err := os.ReadFile(calls)
			if err != nil || string(got) != "test first.test.ts\ntest second.test.ts\n" {
				t.Fatalf("did not run both files: %v, %q", err, got)
			}
		})
	}
}
