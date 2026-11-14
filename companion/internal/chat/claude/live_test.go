package claude

// The live check: the installed Claude Code writes fresh sessions against a fake Anthropic API (internal/chat/fakeapi)
// in a throwaway CLAUDE_CONFIG_DIR, and the reader must read them as it reads the fixtures of that version. A new
// version, or a format change, fails here instead of showing a wrong conversation on the phone.
//
//	CHAT_LIVE=1 go test ./internal/chat/claude -run Live            # check the installed version
//	CHAT_LIVE=1 go test ./internal/chat/claude -run Live -update    # write testdata/claude-code/<version>/ for it
//
// It runs `claude -p` (print mode): with a made-up config, interactive Claude Code stops at its sign-in screen
// (2.1.319), so the pid file (sessions/<pid>.json) is not made here; TestLivePidFiles checks the format of
// the ones the running sessions on this computer have, reading only their field names and start times.

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"herdr-anywhere/companion/internal/chat"
	"herdr-anywhere/companion/internal/chat/fakeapi"
	"herdr-anywhere/companion/internal/chat/fixtime"
)

var claudeTemp = regexp.MustCompile(`/(private/)?tmp/claude-\d+`)

var update = flag.Bool("update", false, "write the fixtures for the installed version")

func live(t *testing.T) string {
	if os.Getenv("CHAT_LIVE") != "1" {
		t.Skip("CHAT_LIVE=1 runs the installed claude")
	}
	bin, err := exec.LookPath("claude")
	if err != nil {
		t.Skip("no claude installed")
	}
	return bin
}

func installedVersion(t *testing.T, bin string) string {
	out, err := exec.Command(bin, "--version").Output()
	if err != nil {
		t.Fatalf("claude --version: %v", err)
	}
	v := regexp.MustCompile(`\d+\.\d+\.\d+`).FindString(string(out))
	if v == "" {
		t.Fatalf("claude --version said %q", out)
	}
	return v
}

// scenario is a session: prompts sent one `claude -p` after another into the same session (-c), and optionally a
// last one killed once the agent has started a tool.
type scenario struct {
	name    string
	prompts []string
	kill    string // a prompt whose run is killed when this tool id is in the file
	image   bool   // the prompts go in as stream-json, each with shot.png attached, as a program sends images
}

var scenarios = []scenario{
	{name: "turns", prompts: []string{"LIST the files here", "EDIT a file for me", "HELLO again"}},
	{name: "killed", prompts: []string{"HELLO first", "SLOW down"}, kill: "toolu_fake_slow"},
	{name: "gallery", prompts: []string{"THINK about a plan", "LOOK at shot.png", "FIND the todos", "HELP me with a helper", "WRITE up the notes", "FAIL on purpose"}},
	{name: "image", prompts: []string{"HELLO, what is in this picture?"}, image: true},
	{name: "steps", prompts: []string{"CHANGE the notes", "BREAK the build"}},
	{name: "files", prompts: []string{"RENDER the demo video"}},
	{name: "command", prompts: []string{"HELLO first", "/model sonnet", "/cost", "HELLO again"}},
}

// shot is a 32×32 blue square, the image the scenarios read and send.
func shot() []byte {
	m := image.NewRGBA(image.Rect(0, 0, 32, 32))
	draw.Draw(m, m.Bounds(), &image.Uniform{color.RGBA{0x25, 0x63, 0xeb, 0xff}}, image.Point{}, draw.Src)
	var b bytes.Buffer
	png.Encode(&b, m)
	return b.Bytes()
}

// session runs one scenario and returns the session file and the folder it ran in.
func session(t *testing.T, bin string, sc scenario) (string, string) {
	root := t.TempDir()
	cfg, cwd := filepath.Join(root, "cc"), filepath.Join(root, "acme")
	for _, d := range []string{cfg, cwd} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	os.WriteFile(filepath.Join(cwd, "notes.txt"), []byte("a\n"), 0o644)
	os.WriteFile(filepath.Join(cwd, "todo.txt"), []byte("b\n"), 0o644)
	os.WriteFile(filepath.Join(cwd, "shot.png"), shot(), 0o644)
	srv := httptest.NewServer(&fakeapi.Server{Cwd: cwd})
	t.Cleanup(srv.Close)
	api := srv.URL
	// ANTHROPIC_AUTH_TOKEN, not ANTHROPIC_API_KEY: with an API key, a second run in the same config dir says
	// "Not logged in" (2.1.319); the token works every time.
	env := append(os.Environ(), "CLAUDE_CONFIG_DIR="+cfg, "ANTHROPIC_BASE_URL="+api, "ANTHROPIC_AUTH_TOKEN=fake-token",
		"BROWSER=/usr/bin/true", "HC_TERMINAL_ID=", "HERDR_ENV=",
		// A test must not update the Mac's own Claude Code (its updater installs into ~/.local/share/claude).
		"DISABLE_AUTOUPDATER=1")
	for i, p := range sc.prompts {
		args := []string{"-p", p, "--allowedTools", "Bash", "Write", "Edit", "Read", "Grep", "Glob", "Agent"}
		if sc.image {
			args = []string{"-p", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose"}
		}
		if i > 0 {
			args = append([]string{"-c"}, args...)
		}
		cmd := exec.Command(bin, args...)
		cmd.Dir, cmd.Env = cwd, env
		if sc.image {
			in, _ := json.Marshal(map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": []any{
				map[string]any{"type": "text", "text": p},
				map[string]any{"type": "image", "source": map[string]any{"type": "base64", "media_type": "image/png", "data": base64.StdEncoding.EncodeToString(shot())}},
			}}})
			cmd.Stdin = bytes.NewReader(append(in, '\n'))
		}
		var out bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &out
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		if i == len(sc.prompts)-1 && sc.kill != "" {
			deadline := time.Now().Add(60 * time.Second)
			for time.Now().Before(deadline) {
				if f := one(t, cfg); f != "" {
					if b, _ := os.ReadFile(f); bytes.Contains(b, []byte(sc.kill)) {
						break
					}
				}
				time.Sleep(200 * time.Millisecond)
			}
			cmd.Process.Kill()
			cmd.Wait()
			continue
		}
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		select {
		case err := <-done:
			if err != nil && !strings.Contains(p, "FAIL") {
				t.Fatalf("claude %v: %v\n%s", args, err, out.String())
			}
		case <-time.After(120 * time.Second):
			cmd.Process.Kill()
			t.Fatalf("claude %v took over 120 s\n%s", args, out.String())
		}
	}
	f := one(t, cfg)
	if f == "" {
		t.Fatalf("%s: no session file, or more than one", sc.name)
	}
	return f, root
}

func one(t *testing.T, cfg string) string {
	m, _ := filepath.Glob(filepath.Join(cfg, "projects", "*", "*.jsonl"))
	if len(m) != 1 {
		return ""
	}
	return m[0]
}

// scrub keeps what the reader reads and makes the rest anonymous: the run's folder becomes /work, and lines without
// a message (and the request copies, whose message is the whole system prompt) keep only their type and ids.
func scrub(t *testing.T, b []byte, root string) []byte {
	var out bytes.Buffer
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 0, 1<<20), 64<<20)
	real, _ := filepath.EvalSymlinks(root)
	for sc.Scan() {
		line := sc.Bytes()
		var d map[string]any
		if err := json.Unmarshal(line, &d); err != nil {
			t.Fatalf("scrub: %v", err)
		}
		_, hasMsg := d["message"]
		if !hasMsg || d["type"] == "api-request-blob" {
			keep := map[string]any{}
			for _, k := range []string{"type", "subtype", "uuid", "parentUuid", "timestamp", "sessionId", "version"} {
				if v, ok := d[k]; ok {
					keep[k] = v
				}
			}
			if a, ok := d["attachment"].(map[string]any); ok {
				keep["attachment"] = map[string]any{"type": a["type"]}
			}
			if hasMsg {
				m, _ := d["message"].(map[string]any)
				keep["message"] = map[string]any{"role": m["role"], "content": []any{map[string]any{"type": "text", "text": "(removed)"}}}
			}
			d = keep
		}
		for _, k := range []string{"gitBranch", "slug"} {
			delete(d, k)
		}
		nb, _ := json.Marshal(d)
		s := string(nb)
		// Claude Code keeps pasted images under its own temp folder, named with the user's id.
		s = claudeTemp.ReplaceAllString(s, "/tmp/claude")
		for _, r := range []string{real, root} {
			if r != "" {
				s = strings.ReplaceAll(s, r, "/work")
				s = strings.ReplaceAll(s, strings.NewReplacer("/", "-", ".", "-").Replace(r), "-work")
			}
		}
		out.WriteString(s + "\n")
	}
	// On the made-up day (fixtime): a published fixture tells nothing of when it was recorded.
	shifted, _ := fixtime.Shift(out.Bytes())
	return shifted
}

func TestLiveSessions(t *testing.T) {
	bin := live(t)
	version := installedVersion(t, bin)
	dir := filepath.Join("testdata", "claude-code", version)
	if !*update {
		if _, err := os.Stat(dir); err != nil {
			t.Fatalf("Claude Code %s has no fixtures: the reader is untested on it. Look at a session of it, then run with -update and check the turns in %s", version, dir)
		}
	}
	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			f, root := session(t, bin, sc)
			b, _ := os.ReadFile(f)
			clean := scrub(t, b, root)
			if *update {
				os.MkdirAll(dir, 0o755)
				if err := os.WriteFile(filepath.Join(dir, sc.name+".jsonl"), clean, 0o644); err != nil {
					t.Fatal(err)
				}
				t.Logf("wrote %s/%s.jsonl; write or check its .want.json by hand", dir, sc.name)
				return
			}
			p := filepath.Join(t.TempDir(), "s.jsonl")
			os.WriteFile(p, clean, 0o644)
			checkWant(t, p, filepath.Join(dir, sc.name+".want.json"))
		})
	}
}

// The pid files of the Claude Code sessions running on this computer: each must lead to exactly one transcript (or
// none yet). Reads ~/.claude only; prints pids and reasons, nothing of the conversations.
func TestLivePidFiles(t *testing.T) {
	live(t)
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".claude")
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		dir = d
	}
	files, _ := filepath.Glob(filepath.Join(dir, "sessions", "*.json"))
	r := &Reader{Dir: dir}
	found, skipped, byVersion := 0, 0, map[string]int{}
	for _, f := range files {
		var pid int
		if _, err := fmt.Sscanf(filepath.Base(f), "%d.json", &pid); err != nil {
			continue
		}
		if exec.Command("kill", "-0", fmt.Sprint(pid)).Run() != nil {
			continue
		}
		// Claude's desktop app runs its own Claude Code, outside any terminal: no pane of it reaches the phone.
		if desktop(f) {
			skipped++
			continue
		}
		p, v, err := r.Find(pid)
		switch {
		case err == nil:
			found++
			byVersion[v]++
			if _, err := r.Turns(p); err != nil {
				t.Errorf("pid %d (Claude Code %s): %v", pid, v, err)
			}
		case strings.Contains(err.Error(), chat.NoRecordYet):
		default:
			t.Errorf("pid %d: %v", pid, err)
		}
	}
	t.Logf("%d running sessions found and read, by version %v; %d of Claude's desktop app left out", found, byVersion, skipped)
}

// desktop: the pid file says the session was started by Claude's desktop app (its first JSON object; Claude Code
// rewrites the file in place and may leave an old end).
func desktop(pidFile string) bool {
	b, err := os.ReadFile(pidFile)
	if err != nil {
		return false
	}
	var p struct {
		Entrypoint string `json:"entrypoint"`
	}
	return json.NewDecoder(bytes.NewReader(b)).Decode(&p) == nil && p.Entrypoint == "claude-desktop"
}
