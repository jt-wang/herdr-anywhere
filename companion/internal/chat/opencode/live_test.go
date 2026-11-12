package opencode

// An OpenCode 2 binary against a fake Responses API (fakeresponses), with a throwaway home and data folders: the
// sessions it writes read as the fixtures of its version say. Opt in with the binary to run, as the one on PATH may
// be another major version:
//
//	OPENCODE_LIVE=/path/to/opencode go test ./internal/chat/opencode -run Live            # check that version
//	OPENCODE_LIVE=/path/to/opencode go test ./internal/chat/opencode -run Live -update    # write testdata/opencode/<version>/
//
// It runs OpenCode's own screen (what a pane runs) in a herdr-core of its own (hc, with its own session and folders),
// as `opencode run` writes the prompt with quotes around it. Autoupdate, the model list fetch, sharing and LSP
// downloads are off and the fake provider is the only one, so it neither asks nor installs; nothing is typed until
// the screen shows the fake model and no question. Each run stops its herdr-core and the background server OpenCode
// starts (`opencode serve --service`).

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"herdr-anywhere/companion/internal/chat"
	"herdr-anywhere/companion/internal/chat/fakeresponses"
	"herdr-anywhere/companion/internal/chat/fixtime"
)

var update = flag.Bool("update", false, "write the fixtures for the binary's version")

var scenarios = []struct {
	name    string
	prompts []string
}{
	{name: "turns", prompts: []string{"LIST the files here", "EDIT a file for me", "HELLO again"}},
	{name: "steps", prompts: []string{"CHANGE the notes", "BREAK the build", "THINK about a plan", "LOOK at shot.png"}},
	{name: "files", prompts: []string{"RENDER the demo video"}},
}

func shot() []byte {
	m := image.NewRGBA(image.Rect(0, 0, 32, 32))
	draw.Draw(m, m.Bounds(), &image.Uniform{color.RGBA{0x25, 0x63, 0xeb, 0xff}}, image.Point{}, draw.Src)
	var b bytes.Buffer
	png.Encode(&b, m)
	return b.Bytes()
}

type ocRun struct {
	bin, root, cwd, home string
	env                  []string
}

func liveRun(t *testing.T, bin string) ocRun {
	root := t.TempDir()
	home, cwd := filepath.Join(root, "home"), filepath.Join(root, "acme")
	for _, d := range []string{filepath.Join(home, ".config", "opencode"), cwd} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	os.WriteFile(filepath.Join(cwd, "notes.txt"), []byte("a\n"), 0o644)
	os.WriteFile(filepath.Join(cwd, "todo.txt"), []byte("b\n"), 0o644)
	os.WriteFile(filepath.Join(cwd, "shot.png"), shot(), 0o644)
	srv := httptest.NewServer(&fakeresponses.Server{})
	t.Cleanup(srv.Close)
	cfg := `{"autoupdate": false, "share": "disabled", "enabled_providers": ["fake"], "disabled_providers": ["opencode"],
 "provider": {"fake": {"npm": "@ai-sdk/openai", "name": "Fake", "options": {"baseURL": "` + srv.URL + `/v1", "apiKey": "fake"},
   "models": {"gpt-5.5": {"name": "GPT-5.5", "tool_call": true}}}},
 "model": "fake/gpt-5.5"}`
	os.WriteFile(filepath.Join(home, ".config", "opencode", "opencode.json"), []byte(cfg), 0o644)
	env := []string{"PATH=/usr/bin:/bin", "HOME=" + home, "XDG_CONFIG_HOME=" + filepath.Join(home, ".config"),
		"XDG_DATA_HOME=" + filepath.Join(home, ".local", "share"), "XDG_STATE_HOME=" + filepath.Join(home, ".local", "state"),
		"XDG_CACHE_HOME=" + filepath.Join(root, "cache"), "TERM=dumb", "TZ=UTC",
		"OPENCODE_DISABLE_AUTOUPDATE=1", "OPENCODE_DISABLE_MODELS_FETCH=1", "OPENCODE_DISABLE_SHARE=1",
		"OPENCODE_DISABLE_LSP_DOWNLOAD=1", "OPENCODE_DISABLE_CLAUDE_CODE=1"}
	r := ocRun{bin: bin, root: root, cwd: cwd, home: home, env: env}
	t.Cleanup(r.stopService)
	return r
}

// stopService stops the background server this run's OpenCode started, by the pid in its own service file.
func (r ocRun) stopService() {
	b, err := os.ReadFile(filepath.Join(r.home, ".local", "state", "opencode", "service.json"))
	if err != nil {
		return
	}
	var s struct{ Pid int }
	if json.Unmarshal(b, &s) == nil && s.Pid > 0 {
		if p, err := os.FindProcess(s.Pid); err == nil {
			p.Kill()
		}
	}
}

func (r ocRun) oc(t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.Command(r.bin, args...)
	cmd.Dir, cmd.Env, cmd.Stdin = r.cwd, r.env, nil
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("opencode %v: %v\n%s", args, err, out.String())
		}
	case <-time.After(90 * time.Second):
		cmd.Process.Kill()
		t.Fatalf("opencode %v: no end after 90 s\n%s", args, out.String())
	}
	return out.String()
}

// dump: the rows of the run's one session, as a fixture, with the run's folders as /work.
func (r ocRun) dump(t *testing.T) []byte {
	db := filepath.Join(r.home, ".local", "share", "opencode", "opencode.db")
	d, err := sql.Open("sqlite", "file:"+db+"?mode=ro&_pragma=busy_timeout(2000)")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	type sess struct {
		ID        string  `json:"id"`
		Directory string  `json:"directory"`
		Title     *string `json:"title"`
		Version   string  `json:"version"`
		Created   int64   `json:"time_created"`
		Updated   int64   `json:"time_updated"`
	}
	type msg struct {
		ID      string          `json:"id"`
		Session string          `json:"session_id"`
		Type    string          `json:"type"`
		Seq     int64           `json:"seq"`
		Created int64           `json:"time_created"`
		Updated int64           `json:"time_updated"`
		Data    json.RawMessage `json:"data"`
	}
	var out struct {
		Sessions []sess `json:"sessions"`
		Messages []msg  `json:"messages"`
	}
	if !table(d, "session_v2") {
		return r.scrub(dump1(t, d))
	}
	rs, err := d.Query("SELECT id, directory, title, version, time_created, time_updated FROM session_v2 ORDER BY time_created")
	if err != nil {
		t.Fatalf("session_v2: %v", err)
	}
	for rs.Next() {
		var s sess
		var title sql.NullString
		if err := rs.Scan(&s.ID, &s.Directory, &title, &s.Version, &s.Created, &s.Updated); err != nil {
			t.Fatal(err)
		}
		if title.Valid {
			s.Title = &title.String
		}
		out.Sessions = append(out.Sessions, s)
	}
	rs.Close()
	if len(out.Sessions) != 1 {
		t.Fatalf("%d sessions", len(out.Sessions))
	}
	ms, err := d.Query("SELECT id, session_id, type, seq, time_created, time_updated, data FROM session_message WHERE session_id = ? ORDER BY seq", out.Sessions[0].ID)
	if err != nil {
		t.Fatalf("session_message: %v", err)
	}
	for ms.Next() {
		var m msg
		var data string
		if err := ms.Scan(&m.ID, &m.Session, &m.Type, &m.Seq, &m.Created, &m.Updated, &data); err != nil {
			t.Fatal(err)
		}
		m.Data = json.RawMessage(data)
		out.Messages = append(out.Messages, m)
	}
	ms.Close()
	b, err := json.MarshalIndent(out, "", " ")
	if err != nil {
		t.Fatal(err)
	}
	return r.scrub(b)
}

// scrub: the run's folders as /work, every time on the made-up day.
func (r ocRun) scrub(b []byte) []byte {
	s := string(b)
	real, _ := filepath.EvalSymlinks(r.root)
	// OpenCode 1 also names a file from / without the slash (apply_patch's relativePath, its output): first those.
	for _, p := range []string{real, r.root} {
		if p != "" {
			s = strings.ReplaceAll(s, " "+strings.TrimPrefix(p, "/"), " work")
			s = strings.ReplaceAll(s, `"`+strings.TrimPrefix(p, "/"), `"work`)
		}
	}
	for _, p := range []string{real, r.root} {
		if p != "" {
			s = strings.ReplaceAll(s, p, "/work")
		}
	}
	// On the made-up day (fixtime): a published fixture tells nothing of when it was recorded.
	shifted, _ := fixtime.Shift([]byte(s + "\n"))
	return shifted
}

// tui runs OpenCode's screen in its own herdr-core and sends it the prompts, one after the other, each when the last
// one's answer ended (an idle row in the database). It returns hc and the environment that reaches its herdr-core.
func (r ocRun) tui(t *testing.T, prompts []string) (string, []string) {
	t.Helper()
	hc, err := exec.LookPath("hc")
	if err != nil {
		h, _ := os.UserHomeDir()
		hc = filepath.Join(h, ".local", "bin", "hc")
	}
	if _, err := os.Stat(hc); err != nil {
		t.Skip("no hc (herdr-core) on this computer")
	}
	run, err := os.MkdirTemp("/tmp", "hmoc") // herdr-core's socket path has to stay short
	if err != nil {
		t.Fatal(err)
	}
	env := append(append([]string{}, r.env...), "HC_SESSION=hmoclive-"+filepath.Base(run), "HC_SOCKET=", "XDG_RUNTIME_DIR="+run, "SHELL=/bin/sh")
	h := func(args ...string) (string, error) {
		cmd := exec.Command(hc, args...)
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	t.Cleanup(func() { h("server", "stop"); os.RemoveAll(run) })
	// hc runs the line as a shell would: a path with spaces goes through a link.
	link := filepath.Join(r.root, "bin", "opencode")
	os.MkdirAll(filepath.Dir(link), 0o755)
	if err := os.Symlink(r.bin, link); err != nil {
		t.Fatal(err)
	}
	if out, err := h("run", "--bg", "--cwd", r.cwd, "--name", "oc", link); err != nil {
		t.Fatalf("hc run: %v\n%s", err, out)
	}
	screen := ""
	for i := 0; i < 60 && !strings.Contains(screen, "GPT-5.5"); i++ {
		time.Sleep(500 * time.Millisecond)
		screen, _ = h("read", "oc")
	}
	low := strings.ToLower(screen)
	if !strings.Contains(screen, "GPT-5.5") {
		t.Fatalf("OpenCode's screen shows no fake model:\n%s", screen)
	}
	for _, w := range []string{"update", "upgrade", "log in", "login", "sign in"} {
		if strings.Contains(low, w) {
			t.Fatalf("OpenCode's screen asks something (%q); nothing typed:\n%s", w, screen)
		}
	}
	for _, p := range prompts {
		before := r.idle(t)
		if out, err := h("send", "oc", p); err != nil {
			t.Fatalf("hc send: %v\n%s", err, out)
		}
		deadline := time.Now().Add(90 * time.Second)
		for r.idle(t) <= before {
			if time.Now().After(deadline) {
				screen, _ = h("read", "oc")
				t.Fatalf("%q: no answer after 90 s; screen:\n%s", p, screen)
			}
			time.Sleep(500 * time.Millisecond)
		}
	}
	return hc, env
}

// idle: how many answers have ended in the run's database (0 before it exists).
func (r ocRun) idle(t *testing.T) int {
	db := filepath.Join(r.home, ".local", "share", "opencode", "opencode.db")
	if _, err := os.Stat(db); err != nil {
		return 0
	}
	d, err := sql.Open("sqlite", "file:"+db+"?mode=ro&_pragma=busy_timeout(2000)")
	if err != nil {
		return 0
	}
	defer d.Close()
	var n int
	if table(d, "session_v2") {
		d.QueryRow("SELECT count(*) FROM session_message WHERE type = 'idle'").Scan(&n)
		return n
	}
	// OpenCode 1: one assistant message per model call; the turn's last ends with "stop".
	d.QueryRow(`SELECT count(*) FROM message WHERE json_extract(data, '$.role') = 'assistant' AND json_extract(data, '$.finish') = 'stop'
		AND json_extract(data, '$.time.completed') IS NOT NULL`).Scan(&n)
	return n
}

// Before OpenCode has worked it has reported no session: the chat says so (not-reported), not "can't tell for sure".
func TestLiveBeforeFirstTurn(t *testing.T) {
	bin := os.Getenv("OPENCODE_LIVE")
	if bin == "" {
		t.Skip("OPENCODE_LIVE=/path/to/opencode runs that OpenCode")
	}
	r := liveRun(t, bin)
	hc, env := r.tui(t, nil)
	for _, kv := range env {
		if k, v, ok := strings.Cut(kv, "="); ok && strings.HasPrefix(k, "HC_") || k == "XDG_RUNTIME_DIR" {
			t.Setenv(k, v)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	s := &chat.Service{HC: hc, Ctx: ctx, Readers: map[string]chat.Reader{
		"opencode": &Reader{DB: filepath.Join(r.home, ".local", "share", "opencode", "opencode.db")}}}
	// As the phone asks: from the screen's first showing, for the first 15 s.
	for end := time.Now().Add(15 * time.Second); time.Now().Before(end); time.Sleep(time.Second) {
		if a := s.Chat("oc"); a.Unknown != chat.NotReported {
			t.Fatalf("before the first turn: %+v, want unknown %q", a, chat.NotReported)
		}
	}
}

// OpenCode 1 reports nothing to herdr-core: after its turns, its terminal title alone finds its session, and the chat
// is what its fixtures say. (OpenCode 2 is found by what it reports while it works: the phone e2e has that.)
func TestLiveChatByTitle(t *testing.T) {
	bin := os.Getenv("OPENCODE_LIVE")
	if bin == "" {
		t.Skip("OPENCODE_LIVE=/path/to/opencode runs that OpenCode")
	}
	r := liveRun(t, bin)
	said := strings.Fields(r.oc(t, "--version"))
	if len(said) == 0 || !strings.HasPrefix(strings.TrimPrefix(said[len(said)-1], "v"), "1.") {
		t.Skip("for OpenCode 1")
	}
	v := strings.TrimPrefix(said[len(said)-1], "v")
	hc, env := r.tui(t, scenarios[0].prompts)
	for _, kv := range env {
		if k, v, ok := strings.Cut(kv, "="); ok && strings.HasPrefix(k, "HC_") || k == "XDG_RUNTIME_DIR" {
			t.Setenv(k, v)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	s := &chat.Service{HC: hc, Ctx: ctx, Readers: map[string]chat.Reader{
		"opencode": &Reader{DB: filepath.Join(r.home, ".local", "share", "opencode", "opencode.db")}}}
	b, err := os.ReadFile(filepath.Join("testdata", "opencode", v, scenarios[0].name+".want.json"))
	if err != nil {
		t.Fatal(err)
	}
	var want []chat.Turn
	json.Unmarshal(b, &want)
	var a chat.Answer
	for end := time.Now().Add(10 * time.Second); time.Now().Before(end); time.Sleep(500 * time.Millisecond) {
		if a = s.Chat("oc"); a.Turns != nil {
			break
		}
	}
	if !a.Chat || !reflect.DeepEqual(norm(a.Turns), norm(want)) {
		g, _ := json.MarshalIndent(a, "", "  ")
		t.Fatalf("chat after its turns:\n%s", g)
	}
}

func TestLiveSessions(t *testing.T) {
	bin := os.Getenv("OPENCODE_LIVE")
	if bin == "" {
		t.Skip("OPENCODE_LIVE=/path/to/opencode runs that OpenCode")
	}
	said := strings.Fields(liveRun(t, bin).oc(t, "--version")) // "opencode v2.2.0"
	v := ""
	if len(said) > 0 {
		v = strings.TrimPrefix(said[len(said)-1], "v")
	}
	if !(strings.HasPrefix(v, "1.") || strings.HasPrefix(v, "2.")) || strings.Contains(v, "/") {
		t.Fatalf("opencode --version said %q; this reader is for OpenCode 1 and 2", said)
	}
	dir := filepath.Join("testdata", "opencode", v)
	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			r := liveRun(t, bin)
			r.tui(t, sc.prompts)
			clean := r.dump(t)
			r.stopService()
			if *update {
				os.MkdirAll(dir, 0o755)
				if err := os.WriteFile(filepath.Join(dir, sc.name+".json"), clean, 0o644); err != nil {
					t.Fatal(err)
				}
				if strings.HasPrefix(v, "1.") {
					writeSchema(t, r, filepath.Join(dir, "schema.sql"))
				}
				t.Logf("wrote %s/%s.json; write or check its .want.json by hand", dir, sc.name)
				return
			}
			want := filepath.Join(dir, sc.name+".want.json")
			b, err := os.ReadFile(want)
			if err != nil {
				t.Fatalf("no fixtures for OpenCode %s (%s): look at a session, then run with -update", v, want)
			}
			var w []chat.Turn
			if err := json.Unmarshal(b, &w); err != nil {
				t.Fatalf("%s: %v", want, err)
			}
			got := fromFixture(t, clean)
			if !reflect.DeepEqual(norm(got), norm(w)) {
				g, _ := json.MarshalIndent(norm(got), "", "  ")
				t.Fatalf("turns differ from %s; got\n%s", want, g)
			}
		})
	}
}

// dump1: OpenCode 1's rows of the run's one session (session, message, part), as a fixture.
func dump1(t *testing.T, d *sql.DB) []byte {
	t.Helper()
	var out fixture1
	rs, err := d.Query("SELECT id, directory, title, version, time_created, time_updated FROM session ORDER BY time_created")
	if err != nil {
		t.Fatalf("session: %v", err)
	}
	for rs.Next() {
		var s sess1
		if err := rs.Scan(&s.ID, &s.Directory, &s.Title, &s.Version, &s.Created, &s.Updated); err != nil {
			t.Fatal(err)
		}
		out.Sessions = append(out.Sessions, s)
	}
	rs.Close()
	if len(out.Sessions) != 1 {
		t.Fatalf("%d sessions", len(out.Sessions))
	}
	id := out.Sessions[0].ID
	ms, err := d.Query("SELECT id, session_id, time_created, time_updated, data FROM message WHERE session_id = ? ORDER BY time_created, id", id)
	if err != nil {
		t.Fatalf("message: %v", err)
	}
	for ms.Next() {
		var m row1
		var data string
		if err := ms.Scan(&m.ID, &m.Session, &m.Created, &m.Updated, &data); err != nil {
			t.Fatal(err)
		}
		m.Data = json.RawMessage(data)
		out.Messages = append(out.Messages, m)
	}
	ms.Close()
	ps, err := d.Query("SELECT id, message_id, session_id, time_created, time_updated, data FROM part WHERE session_id = ? ORDER BY message_id, id", id)
	if err != nil {
		t.Fatalf("part: %v", err)
	}
	for ps.Next() {
		var p row1
		var data string
		if err := ps.Scan(&p.ID, &p.Message, &p.Session, &p.Created, &p.Updated, &data); err != nil {
			t.Fatal(err)
		}
		p.Data = json.RawMessage(data)
		out.Parts = append(out.Parts, p)
	}
	ps.Close()
	b, err := json.MarshalIndent(out, "", " ")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// writeSchema: the session and message tables of the run's database, as OpenCode made them (sqlite_master).
func writeSchema(t *testing.T, r ocRun, to string) {
	t.Helper()
	d, err := sql.Open("sqlite", "file:"+filepath.Join(r.home, ".local", "share", "opencode", "opencode.db")+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	var v string
	d.QueryRow("SELECT version FROM session LIMIT 1").Scan(&v)
	b := "-- OpenCode " + v + "'s session and message tables (its opencode.db, sqlite_master), written by its live check.\n"
	for _, n := range []string{"session", "session_message", "message", "part"} {
		var q string
		if d.QueryRow("SELECT sql FROM sqlite_master WHERE type = 'table' AND name = ?", n).Scan(&q) == nil {
			b += q + ";\n"
		}
	}
	if err := os.WriteFile(to, []byte(b), 0o644); err != nil {
		t.Fatal(err)
	}
}
