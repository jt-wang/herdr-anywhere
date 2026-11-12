package claude

// The live check of messages sent while Claude Code works: its own screen (livetui), as a pane runs it, against the
// fake API. Sent while a tool runs, a message is queued; Claude Code takes it when the tool ends and writes it only
// as an attachment line (queued_command), not as a user line. Opt in as for the other live checks:
//
//	CHAT_LIVE=1 go test ./internal/chat/claude -run LiveQueued            # check the installed version
//	CHAT_LIVE=1 go test ./internal/chat/claude -run LiveQueued -update    # write testdata/claude-code/<version>/queued.jsonl

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"herdr-anywhere/companion/internal/chat/fakeapi"
	"herdr-anywhere/companion/internal/chat/livetui"
)

func TestLiveQueued(t *testing.T) {
	bin := live(t)
	version := installedVersion(t, bin)
	dir := filepath.Join("testdata", "claude-code", version)
	root := t.TempDir()
	home, cfg, cwd := filepath.Join(root, "home"), filepath.Join(root, "cc"), filepath.Join(root, "acme")
	for _, d := range []string{home, cfg, cwd} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	os.WriteFile(filepath.Join(cwd, "notes.txt"), []byte("a\n"), 0o644)
	srv := httptest.NewServer(&fakeapi.Server{Cwd: cwd})
	t.Cleanup(srv.Close)
	// The screen asks nothing first: onboarding done, the folder trusted, and the fake steps' command allowed. The fake
	// API is reached with a made-up token (ANTHROPIC_AUTH_TOKEN): an API key would be asked about first.
	real, _ := filepath.EvalSymlinks(cwd)
	state, _ := json.Marshal(map[string]any{"hasCompletedOnboarding": true,
		"projects": map[string]any{cwd: map[string]any{"hasTrustDialogAccepted": true}, real: map[string]any{"hasTrustDialogAccepted": true}}})
	os.WriteFile(filepath.Join(cfg, ".claude.json"), state, 0o644)
	os.WriteFile(filepath.Join(cfg, "settings.json"), []byte(`{"permissions":{"allow":["Bash(python3:*)"],"defaultMode":"default"},"env":{"DISABLE_AUTOUPDATER":"1"}}`), 0o644)
	py, _ := exec.LookPath("python3")
	env := []string{"PATH=" + filepath.Dir(bin) + ":" + filepath.Dir(py) + ":/usr/bin:/bin", "HOME=" + home, "TERM=xterm-256color", "LANG=en_US.UTF-8",
		"CLAUDE_CONFIG_DIR=" + cfg, "ANTHROPIC_AUTH_TOKEN=fake-token", "ANTHROPIC_BASE_URL=" + srv.URL,
		"DISABLE_AUTOUPDATER=1", "DISABLE_UPDATES=1", "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1", "DISABLE_TELEMETRY=1"}
	u := livetui.Start(t, env, cwd, bin)
	// Manual mode, so no request goes to a permission classifier. 2.1.325 offers auto mode once, at the first start:
	// keep manual mode (its second choice). Anything else unseen stops the test before a key is sent.
	ready := func(s string) bool {
		return strings.Contains(s, "manual mode on") || strings.Contains(s, "? for shortcuts")
	}
	const offer = "Make auto mode your default permission mode?"
	keepManual := func() {
		u.Keys("down")
		u.Wait(5*time.Second, "manual mode picked", livetui.Has("❯ No, keep manual mode"))
		// An Enter right as the choice moves is lost (seen twice); by hand, a second later, it picks it.
		time.Sleep(time.Second)
		u.Keys("enter")
		u.Wait(20*time.Second, "the prompt in manual mode", func(s string) bool { return ready(s) && !strings.Contains(s, offer) })
	}
	if s := u.Wait(60*time.Second, "the prompt or the auto mode offer", func(s string) bool { return ready(s) || strings.Contains(s, offer) }); strings.Contains(s, offer) {
		keepManual()
	}
	record := func() string {
		m, _ := filepath.Glob(filepath.Join(cfg, "projects", "*", "*.jsonl"))
		if len(m) != 1 {
			return ""
		}
		b, _ := os.ReadFile(m[0])
		return string(b)
	}
	u.Settle()
	if strings.Contains(u.Screen(), offer) { // it can come up under a prompt already drawn
		keepManual()
		u.Settle()
	}
	u.Send("WORK through the three steps")
	u.Until(60*time.Second, "running its first step", func() bool { return strings.Contains(record(), "toolu_fake_w1") })
	// Two messages while the first step's command runs: both wait, then go in together when it ends.
	u.Send("ALSO keep the old colors")
	u.Send("AND keep the old fonts")
	u.Wait(10*time.Second, "both messages queued", livetui.Has("keep the old colors", "keep the old fonts"))
	u.Until(90*time.Second, "done with the turn", func() bool {
		r := record()
		return strings.Contains(r, "absorbed_mid_turn") && strings.Contains(r, "All three steps are done.")
	})
	clean := scrub(t, []byte(record()), root)
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
