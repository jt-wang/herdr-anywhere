package pi

// The live check of messages sent while Pi works: its own screen (livetui), as a pane runs it, against the fake
// Responses API. Sent with Enter while a tool runs, a message steers the turn: Pi puts it in after that tool's result,
// as a user message of the same turn. Sent with ⌥Enter, it is a follow-up: Pi sends it once the turn is done, as a
// new turn. Opt in as for the other live checks:
//
//	PI_LIVE=1 go test ./internal/chat/pi -run LiveQueued            # check the installed version
//	PI_LIVE=1 go test ./internal/chat/pi -run LiveQueued -update    # write testdata/pi/<version>/queued.jsonl

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"herdr-anywhere/companion/internal/chat/livetui"
)

func TestLiveQueued(t *testing.T) {
	bin := live(t)
	r := liveRun(t, bin)
	v := strings.TrimSpace(pi(t, bin, t.TempDir(), r.env, "--version"))
	if v == "" || strings.ContainsAny(v, " \n/") {
		t.Fatalf("pi --version said %q", v)
	}
	env := []string{"TERM=xterm-256color", "LANG=en_US.UTF-8"}
	for _, e := range r.env {
		if !strings.HasPrefix(e, "TERM=") {
			env = append(env, e)
		}
	}
	py, _ := exec.LookPath("python3")
	for i, e := range env {
		if strings.HasPrefix(e, "PATH=") {
			env[i] = e + ":" + filepath.Dir(py)
		}
	}
	u := livetui.Start(t, env, r.cwd, bin, "--model", "fake/gpt-5.5")
	// Anything else on the screen first stops the test before a key is sent.
	u.Wait(60*time.Second, "the prompt", livetui.Has("gpt-5.5"))
	record := func() string {
		m, _ := filepath.Glob(filepath.Join(r.root, "home", ".pi", "agent", "sessions", "*", "*.jsonl"))
		if len(m) != 1 {
			return ""
		}
		b, _ := os.ReadFile(m[0])
		return string(b)
	}
	u.Settle()
	u.Send("WORK through the three steps")
	u.Until(60*time.Second, "running its first step", func() bool { return strings.Contains(record(), "call_fake_w1") })
	u.Send("ALSO keep the old colors")
	u.Wait(10*time.Second, "the message steering", livetui.Has("Steering: ALSO keep the old colors"))
	// Typed key by key (hc keys sends letters lower case), so no word of the fake model's: it answers on from the last
	// prompt that has one, WORK, after its three tool results — "All three steps are done." again.
	u.Type("then say thanks")
	u.Keys("alt+enter")
	u.Wait(10*time.Second, "the follow-up waiting", livetui.Has("Follow-up: then say thanks"))
	u.Until(90*time.Second, "done with both turns", func() bool {
		r := record()
		return strings.Count(r, "All three steps are done.") == 2
	})
	clean := scrub(t, []byte(record()), r.root)
	dir := filepath.Join("testdata", "pi", v)
	if *update {
		os.MkdirAll(dir, 0o755)
		if err := os.WriteFile(filepath.Join(dir, "queued.jsonl"), clean, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %s/queued.jsonl; write or check its .want.json by hand", dir)
		return
	}
	p := filepath.Join(t.TempDir(), "s.jsonl")
	os.WriteFile(p, clean, 0o644)
	checkWant(t, p, filepath.Join(dir, "queued.want.json"))
}
