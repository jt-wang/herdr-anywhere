// Package livetui runs an installed agent's own screen in a herdr-core of its own (hc, with its own session and
// folders), for live tests only: what a pane runs, typed into as the phone types into it (`hc send`: the text pasted,
// then Enter) and pressed as the phone's keys press it (`hc keys`). Some records exist only this way: a message sent
// while the agent works is queued by its screen, and print modes have no screen.
//
// Nothing is sent until the test has looked at the screen (Wait): an agent's update or sign-in prompt picks its
// default on Enter.
package livetui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TUI is one program running in its own herdr-core.
type TUI struct {
	t   *testing.T
	hc  string
	env []string
}

// Start runs bin with args in dir, in a herdr-core of its own, with exactly env (and what hc needs). It stops the
// herdr-core, and the program with it, when the test ends.
func Start(t *testing.T, env []string, dir, bin string, args ...string) *TUI {
	t.Helper()
	hc, err := exec.LookPath("hc")
	if err != nil {
		h, _ := os.UserHomeDir()
		hc = filepath.Join(h, ".local", "bin", "hc")
	}
	if _, err := os.Stat(hc); err != nil {
		t.Skip("no hc (herdr-core) on this computer")
	}
	run, err := os.MkdirTemp("/tmp", "hmlt") // herdr-core's socket path has to stay short
	if err != nil {
		t.Fatal(err)
	}
	u := &TUI{t: t, hc: hc, env: append(append([]string{}, env...), "HC_SESSION=hmlt-"+filepath.Base(run), "HC_SOCKET=", "HC_TERMINAL_ID=",
		"XDG_RUNTIME_DIR="+run, "SHELL=/bin/sh")}
	t.Cleanup(func() { u.hcRun("server", "stop"); os.RemoveAll(run) })
	// hc runs the line as a shell would: a path with spaces goes through a link. The link keeps the program's name,
	// which is how herdr-core knows the agent (named otherwise, a send waits for output that never settles).
	link := filepath.Join(run, filepath.Base(bin))
	if err := os.Symlink(bin, link); err != nil {
		t.Fatal(err)
	}
	line := append([]string{"run", "--bg", "--no-parent", "--no-notify", "--cwd", dir, "--name", "agent", link}, args...)
	if out, err := u.hcRun(line...); err != nil {
		t.Fatalf("hc run: %v\n%s", err, out)
	}
	return u
}

func (u *TUI) hcRun(args ...string) (string, error) {
	cmd := exec.Command(u.hc, args...)
	cmd.Env = u.env
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// Screen is what the program shows now, with its history.
func (u *TUI) Screen() string {
	s, _ := u.hcRun("read", "agent")
	return s
}

// Wait waits up to d for ok to hold of the screen, and fails the test with the screen when it doesn't.
func (u *TUI) Wait(d time.Duration, what string, ok func(screen string) bool) string {
	u.t.Helper()
	deadline := time.Now().Add(d)
	for {
		s := u.Screen()
		if ok(s) {
			return s
		}
		if time.Now().After(deadline) {
			u.t.Fatalf("after %s, the screen doesn't show %s:\n%s", d, what, s)
		}
		time.Sleep(300 * time.Millisecond)
	}
}

// Settle waits until the screen hasn't changed for 3 s (up to 30 s). herdr-core takes a send to a terminal that is
// working only once its output has settled since it started ("still starting: its output has not settled"): a pane
// the phone types into has long been running, so a test settles its new one before its first message.
func (u *TUI) Settle() {
	u.t.Helper()
	last, since := "", time.Now()
	for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); time.Sleep(300 * time.Millisecond) {
		s := u.Screen()
		if s != last {
			last, since = s, time.Now()
		} else if time.Since(since) >= 3*time.Second {
			return
		}
	}
	u.t.Fatalf("the screen didn't settle in 30 s:\n%s", last)
}

// Until waits up to d for ok, checked every 300 ms, and fails the test with the screen when it doesn't hold.
func (u *TUI) Until(d time.Duration, what string, ok func() bool) {
	u.t.Helper()
	deadline := time.Now().Add(d)
	for !ok() {
		if time.Now().After(deadline) {
			u.t.Fatalf("after %s, still not %s; screen:\n%s", d, what, u.Screen())
		}
		time.Sleep(300 * time.Millisecond)
	}
}

// Send pastes text and presses Enter, as the phone's composer does. A send herdr-core turns away untyped because the
// program's output hasn't settled yet ("still starting", seen with Pi while it works) is tried again for up to 15 s.
func (u *TUI) Send(text string) {
	u.t.Helper()
	for deadline := time.Now().Add(15 * time.Second); ; time.Sleep(500 * time.Millisecond) {
		out, err := u.hcRun("send", "agent", text)
		if err == nil {
			return
		}
		if !strings.Contains(out, "still starting") || time.Now().After(deadline) {
			u.t.Fatalf("hc send %q: %v\n%s\nscreen:\n%s", text, err, out, u.Screen())
		}
	}
}

// Keys presses keys by hc's names (enter, esc, down, alt+enter, a, …).
func (u *TUI) Keys(keys ...string) {
	u.t.Helper()
	if out, err := u.hcRun(append([]string{"keys", "agent"}, keys...)...); err != nil {
		u.t.Fatalf("hc keys %v: %v\n%s\nscreen:\n%s", keys, err, out, u.Screen())
	}
}

// Type presses the keys of plain text without Enter (letters, digits and spaces).
func (u *TUI) Type(text string) {
	u.t.Helper()
	var keys []string
	for _, r := range text {
		if r == ' ' {
			keys = append(keys, "space")
		} else {
			keys = append(keys, string(r))
		}
	}
	u.Keys(keys...)
}

// Has: whether the screen shows every one of words.
func Has(words ...string) func(string) bool {
	return func(s string) bool {
		for _, w := range words {
			if !strings.Contains(s, w) {
				return false
			}
		}
		return true
	}
}
