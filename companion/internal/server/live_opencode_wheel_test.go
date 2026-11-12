package server

// OpenCode's wheel, by column, through the companion as the phone reaches it: OpenCode 1.19.0 takes no wheel on its
// first two columns (its list starts at column 3), where a finger at the phone's left edge lands (iOS's swipe back
// strip is 24 pt). The terminal page sends a wheel no further left than column 3 (ui/terminal-page.ts scrollView),
// and e2e/page-touch.mjs's stand-in program ignores columns 1 and 2 as this OpenCode does. This test measures what both
// rest on, in the OpenCode it is given, against the fake Responses API: OpenCode answers ROWS (150 numbered rows),
// then wheel reports up at columns 30, 1, 2 and 3, and how far each moved the rows. Column 3 and 30 must move them;
// columns 1 and 2 are only reported (an OpenCode that takes them too needs no clamp, and the page's is harmless).
// Opt in with the binary to run:
//
//	OPENCODE_LIVE=/path/to/opencode go test ./internal/server -run LiveOpenCodeWheel -v

import (
	"context"
	"fmt"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"herdr-anywhere/companion/internal/chat/fakeresponses"
)

var ocRow = regexp.MustCompile(`acme-api line (\d+)`)

func TestLiveOpenCodeWheelColumns(t *testing.T) {
	bin := os.Getenv("OPENCODE_LIVE")
	if bin == "" {
		t.Skip("OPENCODE_LIVE=/path/to/opencode runs that OpenCode")
	}
	hc := realHC(t)
	root := t.TempDir()
	home, cwd := filepath.Join(root, "home"), filepath.Join(root, "acme-api")
	for _, d := range []string{filepath.Join(home, ".config", "opencode"), cwd} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	api := httptest.NewServer(&fakeresponses.Server{})
	t.Cleanup(api.Close)
	cfg := `{"autoupdate": false, "share": "disabled", "enabled_providers": ["fake"], "disabled_providers": ["opencode"],
 "provider": {"fake": {"npm": "@ai-sdk/openai", "name": "Fake", "options": {"baseURL": "` + api.URL + `/v1", "apiKey": "fake"},
   "models": {"gpt-5.5": {"name": "GPT-5.5", "tool_call": true}}}},
 "model": "fake/gpt-5.5"}`
	if err := os.WriteFile(filepath.Join(home, ".config", "opencode", "opencode.json"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{
		"HOME": home, "XDG_CONFIG_HOME": filepath.Join(home, ".config"), "XDG_DATA_HOME": filepath.Join(home, ".local", "share"),
		"XDG_STATE_HOME": filepath.Join(home, ".local", "state"), "XDG_CACHE_HOME": filepath.Join(root, "cache"),
		"TERM": "xterm-256color", "OPENCODE_DISABLE_AUTOUPDATE": "1", "OPENCODE_DISABLE_MODELS_FETCH": "1",
		"OPENCODE_DISABLE_SHARE": "1", "OPENCODE_DISABLE_LSP_DOWNLOAD": "1", "OPENCODE_DISABLE_CLAUDE_CODE": "1",
	}
	t.Cleanup(func() { // the background server this OpenCode started, by the pid in its own service file
		if b, err := os.ReadFile(filepath.Join(home, ".local", "state", "opencode", "service.json")); err == nil {
			if m := regexp.MustCompile(`"pid":\s*(\d+)`).FindSubmatch(b); m != nil {
				if pid, _ := strconv.Atoi(string(m[1])); pid > 0 {
					if p, err := os.FindProcess(pid); err == nil {
						p.Kill()
					}
				}
			}
		}
	})

	f := newFixture(t, func(c *Config) { c.HC = hc })
	_, token := f.pair(t)
	cl := newClient(t, f, token, false)
	if g := generation(cl.hello(map[string]any{"framed": true})); g != 1 {
		t.Fatalf("hello generation = %v, want 1", g)
	}
	run := cl.call("terminal.run", map[string]any{
		"argv": []string{"/bin/sh", "-c", `cd "$1" && exec "$2"`, "sh", cwd, bin},
		"keep": true,
		"env":  env,
	})
	addr, _ := run["address"].(string)
	if addr == "" {
		t.Fatalf("terminal.run returned %v", run)
	}
	att := cl.call("terminal.attach", map[string]any{"terminal": addr, "cols": 100, "rows": 40})
	chf, ok := att["channel"].(float64)
	if !ok || chf == 0 {
		t.Fatalf("terminal.attach returned %v", att)
	}
	ch := uint32(chf)
	// Reads what the core streams for a while: its credit goes back, so the program is never held up.
	drain := func(d time.Duration) {
		ctx, cancel := context.WithTimeout(context.Background(), d)
		defer cancel()
		for {
			if _, err := cl.next(ctx); err != nil {
				return
			}
		}
	}
	screen := func() string {
		out, err := exec.Command(hc, "read", addr).CombinedOutput()
		if err != nil {
			t.Fatalf("hc read %s: %v\n%s", addr, err, out)
		}
		return string(out)
	}
	until := func(d time.Duration, what string, ok func(string) bool) string {
		var s string
		for end := time.Now().Add(d); time.Now().Before(end); {
			drain(500 * time.Millisecond)
			if s = screen(); ok(s) {
				return s
			}
		}
		t.Fatalf("no %s within %v; screen:\n%s", what, d, s)
		return ""
	}
	// Nothing is typed until the screen shows the fake model and no question (an update or sign-in prompt takes Enter).
	s := until(60*time.Second, "OpenCode on the fake model", func(s string) bool { return strings.Contains(s, "GPT-5.5") })
	if q := regexp.MustCompile(`(?i)update now|sign in|log ?in`).FindString(s); q != "" {
		t.Fatalf("a question on OpenCode's screen (%q):\n%s", q, s)
	}
	if out, err := exec.Command(hc, "send", addr, "ROWS").CombinedOutput(); err != nil {
		t.Fatalf("hc send: %v\n%s", err, out)
	}
	until(60*time.Second, "row 150 of the answer", func(s string) bool { return strings.Contains(s, "acme-api line 150") })
	drain(2 * time.Second)

	// The first numbered row on the screen (hc read prints the screen only, no history).
	top := func() int {
		s := screen()
		m := ocRow.FindStringSubmatch(s)
		if m == nil {
			t.Fatalf("no numbered row on the screen:\n%s", s)
		}
		n, _ := strconv.Atoi(m[1])
		return n
	}
	moved := map[int]int{}
	for _, col := range []int{30, 1, 2, 3} {
		before := top()
		for i := 0; i < 3; i++ {
			cl.send(frame(ch, kData, []byte(fmt.Sprintf("\x1b[<64;%d;10M", col))))
			drain(300 * time.Millisecond)
		}
		drain(time.Second)
		moved[col] = before - top()
		t.Logf("3 wheel reports up at column %d, row 10: the rows moved %d (top row %d before)", col, moved[col], before)
	}
	if moved[30] <= 0 {
		t.Fatalf("a wheel at column 30 moved nothing (%v): this check cannot see a wheel work", moved)
	}
	if moved[3] <= 0 {
		t.Errorf("a wheel at column 3 moved nothing (%v): the page's clamp to column 3 (ui/terminal-page.ts) does not help this OpenCode", moved)
	}
	if moved[1] > 0 || moved[2] > 0 {
		t.Logf("this OpenCode takes a wheel on column 1 or 2 (%v): the page's clamp is no longer needed for it", moved)
	}
	cl.call("terminal.kill", map[string]any{"terminal": addr})
}
