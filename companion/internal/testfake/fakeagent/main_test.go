package main

// Against the real herdr-core, in a core of its own (as internal/server's integration tests): each fake agent runs
// in a terminal, and the Companion's chat service finds it by what it reports and reads its record as the agent's
// own. Skipped without hc (HM_TEST_HC, else ~/.local/bin/hc).

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"herdr-anywhere/companion/internal/chat"
	"herdr-anywhere/companion/internal/chat/claude"
	"herdr-anywhere/companion/internal/chat/codex"
	"herdr-anywhere/companion/internal/chat/opencode"
	"herdr-anywhere/companion/internal/chat/pi"
	"herdr-anywhere/companion/internal/hcbin"
	"herdr-anywhere/companion/internal/testfake"
)

var recordings = map[string][]string{
	"claude-code": {"claude/testdata/claude-code/2.1.322/turns.jsonl", "claude/testdata/claude-code/2.1.322/steps.jsonl", "claude/testdata/claude-code/2.1.322/image.jsonl"},
	"codex":       {"codex/testdata/codex/0.167.1/turns.jsonl", "codex/testdata/codex/0.167.1/steps.jsonl"},
	"pi":          {"pi/testdata/pi/1.4.0/turns.jsonl", "pi/testdata/pi/1.4.0/steps.jsonl"},
	"opencode":    {"opencode/testdata/opencode/2.2.0/turns.json", "opencode/testdata/opencode/2.2.0/steps.json"},
}

func TestFakesInHerdrCore(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("herdr-core runs on macOS and Linux")
	}
	home, _ := os.UserHomeDir()
	hc, _, err := hcbin.Resolve(os.Getenv("HM_TEST_HC"), func(string) (string, error) { return "", exec.ErrNotFound }, home)
	if err != nil {
		t.Skipf("no hc: %v", err)
	}
	scratch := t.TempDir()
	bin := filepath.Join(scratch, "fakeagent")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v: %s", err, out)
	}
	// herdr-core gives every terminal the environment of what started it, and the readers read the same settings:
	// all set before the core starts.
	b := make([]byte, 4)
	rand.Read(b)
	dirs := map[string]string{}
	for agent, env := range map[string]string{"claude-code": "CLAUDE_CONFIG_DIR", "codex": "CODEX_HOME", "pi": "PI_CODING_AGENT_DIR", "opencode": "XDG_DATA_HOME"} {
		dirs[agent] = filepath.Join(scratch, agent)
		t.Setenv(env, dirs[agent])
	}
	for k, v := range map[string]string{"HC_SESSION": "hmfake-" + hex.EncodeToString(b), "HC_SOCKET": "",
		"XDG_STATE_HOME": filepath.Join(scratch, "state"), "XDG_CONFIG_HOME": filepath.Join(scratch, "config"),
		"XDG_RUNTIME_DIR": testfake.ShortDir(t, "hm"), "HOME": filepath.Join(scratch, "home")} {
		t.Setenv(k, v)
	}
	os.MkdirAll(filepath.Join(scratch, "home"), 0o755)
	t.Cleanup(func() { exec.Command(hc, "server", "stop").Run() })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc := &chat.Service{HC: hc, Ctx: ctx, Readers: map[string]chat.Reader{
		"claude-code": &claude.Reader{Dir: dirs["claude-code"]},
		"codex":       &codex.Reader{Dir: dirs["codex"]},
		"pi":          &pi.Reader{Setup: &pi.Setup{Dir: dirs["pi"], HC: hc}},
		"opencode":    &opencode.Reader{},
	}}
	for _, agent := range []string{"claude-code", "codex", "pi", "opencode"} {
		t.Run(agent, func(t *testing.T) {
			cwd := filepath.Join(scratch, "home", "src", "demo-"+agent)
			os.MkdirAll(cwd, 0o755)
			name := "fa-" + strings.ReplaceAll(agent, "-", "")
			args := []string{"run", "--bg", "--keep", "--cwd", cwd, "--name", name, "--", bin, "-agent", agent,
				"-preload", "40", "-long", "500", "-delay", "50ms", "-hc", hc}
			for _, r := range recordings[agent] {
				p, _ := filepath.Abs(filepath.Join("..", "..", "chat", filepath.FromSlash(r)))
				args = append(args, p)
			}
			if out, err := exec.Command(hc, args...).CombinedOutput(); err != nil {
				t.Fatalf("hc run: %v: %s", err, out)
			}
			// OpenCode says which conversation only while it works: nothing until the first prompt.
			if agent != "opencode" {
				a := wait(t, svc, name, func(a chat.Answer) bool { return len(a.Turns) == chat.MaxTurns })
				if got := a.Turns[len(a.Turns)-1].Said; !strings.HasPrefix(got, "040 ") {
					t.Fatalf("the last preloaded turn said %q", got)
				}
			}
			if out, err := exec.Command(hc, "send", name, "HELLO from the phone").CombinedOutput(); err != nil {
				t.Fatalf("hc send: %v: %s", err, out)
			}
			a := wait(t, svc, name, func(a chat.Answer) bool {
				return len(a.Turns) > 0 && a.Turns[len(a.Turns)-1].Said == "HELLO from the phone" && a.Turns[len(a.Turns)-1].End == "done"
			})
			// The long listing: the command's details count all 500 lines.
			for _, tn := range a.Turns {
				for _, st := range tn.Steps {
					if st.Kind == "run" {
						d, err := svc.Step(name, st.ID)
						if err == nil && d.Run != nil && d.Run.Lines >= 500 {
							return
						}
					}
				}
			}
			t.Fatalf("no command with the long listing in %d turns", len(a.Turns))
		})
	}
}

// wait asks as the phone does, every 300 ms, until ok or 20 s.
func wait(t *testing.T, s *chat.Service, name string, ok func(chat.Answer) bool) chat.Answer {
	t.Helper()
	var a chat.Answer
	for end := time.Now().Add(20 * time.Second); time.Now().Before(end); time.Sleep(300 * time.Millisecond) {
		if a = s.Chat(name); ok(a) {
			return a
		}
	}
	last := ""
	if len(a.Turns) > 0 {
		x := a.Turns[len(a.Turns)-1]
		last = fmt.Sprintf("%q %s", x.Said, x.End)
	}
	t.Fatalf("after 20 s: chat %v, unknown %q, %d turns, last %s", a.Chat, a.Unknown, len(a.Turns), last)
	return a
}
