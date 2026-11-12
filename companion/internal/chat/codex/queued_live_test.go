package codex

// The live check of a message sent while Codex works: its own screen (livetui), as a pane runs it, against the fake
// Responses API. Sent with Enter while a command runs, a message steers the turn: Codex puts it in after the next tool
// call, as a second user message of the same turn. Opt in as for the other live checks:
//
//	CODEX_LIVE=1 go test ./internal/chat/codex -run LiveQueued            # check the installed version
//	CODEX_LIVE=1 go test ./internal/chat/codex -run LiveQueued -update    # write testdata/codex/<version>/queued.jsonl

import (
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"herdr-anywhere/companion/internal/chat/fakeresponses"
	"herdr-anywhere/companion/internal/chat/livetui"
)

func TestLiveQueued(t *testing.T) {
	bin := live(t)
	root := t.TempDir()
	home, cwd := filepath.Join(root, "codex-home"), filepath.Join(root, "acme")
	for _, d := range []string{home, cwd} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	os.WriteFile(filepath.Join(cwd, "notes.txt"), []byte("a\n"), 0o644)
	srv := httptest.NewServer(&fakeresponses.Server{})
	t.Cleanup(srv.Close)
	real, _ := filepath.EvalSymlinks(cwd)
	// No update check (its prompt picks "Update now" on Enter), no approvals, the folder trusted, and the notices of
	// the first start already seen, as 0.167.1 writes them once they are answered (its new-model notice kept the
	// configured model): answered by keys, the notice switched the model and took the first message.
	cfg := "check_for_update_on_startup = false\nmodel = \"gpt-5.5\"\nmodel_provider = \"fake\"\nsandbox_mode = \"workspace-write\"\napproval_policy = \"never\"\n" +
		"[model_providers.fake]\nname = \"Fake\"\nbase_url = \"" + srv.URL + "/v1\"\nwire_api = \"responses\"\nenv_key = \"FAKE_API_KEY\"\n" +
		"[projects.\"" + cwd + "\"]\ntrust_level = \"trusted\"\n[projects.\"" + real + "\"]\ntrust_level = \"trusted\"\n" +
		"[tui]\nscreen_reader_detection_done = true\n[tui.model_availability_nux]\n\"gpt-6.1-sol\" = 3\n[notice.model_migrations]\n\"gpt-5.5\" = \"gpt-6-sol\"\n"
	os.WriteFile(filepath.Join(home, "config.toml"), []byte(cfg), 0o644)
	// Codex leaves app-server processes of its home running after it ends: stopped with the test.
	t.Cleanup(func() { exec.Command("pkill", "-f", home).Run(); time.Sleep(time.Second) })
	py, _ := exec.LookPath("python3")
	env := []string{"PATH=" + filepath.Dir(bin) + ":" + filepath.Dir(py) + ":/usr/bin:/bin", "HOME=" + root, "CODEX_HOME=" + home,
		"FAKE_API_KEY=fake", "TERM=xterm-256color", "LANG=en_US.UTF-8"}
	u := livetui.Start(t, env, cwd, bin)
	// Anything else on the screen first (a notice of a newer version) stops the test before a key is sent.
	u.Wait(60*time.Second, "the prompt", livetui.Has("Ask Codex to do anything"))
	record := func() string {
		m, _ := filepath.Glob(filepath.Join(home, "sessions", "*", "*", "*", "rollout-*.jsonl"))
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
	u.Wait(10*time.Second, "the message waiting", livetui.Has("Messages to be submitted after next tool call", "keep the old colors"))
	u.Until(90*time.Second, "done with the turn", func() bool {
		r := record()
		return strings.Contains(r, "All three steps are done.") && strings.Contains(r, `"task_complete"`)
	})
	f, _ := filepath.Glob(filepath.Join(home, "sessions", "*", "*", "*", "rollout-*.jsonl"))
	clean := scrub(t, []byte(record()), root)
	dir := filepath.Join("testdata", "codex", version(f[0]))
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
