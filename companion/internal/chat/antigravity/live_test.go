package antigravity

// The live check: the installed Antigravity CLI (agy) writes fresh conversations against a fake Gemini API
// (internal/chat/fakegemini) as its enterprise model gateway, under a throwaway HOME, and the reader must read them as
// it reads the fixtures of that version. A new version, or a format change, fails here instead of showing a wrong
// conversation on the phone.
//
//	CHAT_LIVE=1 go test ./internal/chat/antigravity -run Live            # check the installed version
//	CHAT_LIVE=1 go test ./internal/chat/antigravity -run Live -update    # write testdata/antigravity/<version>/ for it
//
// Print mode (-p, one run after another with -c) makes the plain turns. A question left open, a plan reviewed in
// /artifact, a message sent while it works, a pasted image and an interrupted command exist only on its screen: those
// run in a herdr-core of their own (livetui), typed into as the phone types, and check Find on the running process.

import (
	"bufio"
	"bytes"
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
	"herdr-anywhere/companion/internal/chat/fakegemini"
	"herdr-anywhere/companion/internal/chat/fixtime"
	"herdr-anywhere/companion/internal/chat/livetui"
)

var update = flag.Bool("update", false, "write the fixtures for the installed version")

func live(t *testing.T) string {
	if os.Getenv("CHAT_LIVE") != "1" {
		t.Skip("CHAT_LIVE=1 runs the installed agy")
	}
	bin, err := exec.LookPath("agy")
	if err != nil {
		h, _ := os.UserHomeDir()
		bin = filepath.Join(h, ".local", "bin", "agy")
		if _, err := os.Stat(bin); err != nil {
			t.Skip("no agy installed")
		}
	}
	return bin
}

func installedVersion(t *testing.T, bin string) string {
	out, err := exec.Command(bin, "--version").Output()
	if err != nil {
		t.Fatalf("agy --version: %v", err)
	}
	v := regexp.MustCompile(`\d+\.\d+\.\d+`).FindString(string(out))
	if v == "" {
		t.Fatalf("agy --version said %q", out)
	}
	return v
}

// world is one throwaway HOME and workspace, with the fake gateway.
type world struct {
	root, home, cwd string
	env             []string
}

func newWorld(t *testing.T, bin string) *world {
	// Under /tmp: herdr-core's socket path has to stay short, and agy resolves the folder it runs in.
	root, err := os.MkdirTemp("/tmp", "agylive")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	w := &world{root: root, home: filepath.Join(root, "home"), cwd: filepath.Join(root, "acme")}
	for _, d := range []string{w.home, w.cwd} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	os.WriteFile(filepath.Join(w.cwd, "notes.txt"), []byte("a\n"), 0o644)
	os.WriteFile(filepath.Join(w.cwd, "todo.txt"), []byte("buy milk\n"), 0o644)
	os.WriteFile(filepath.Join(w.cwd, "shot.png"), shot(), 0o644)
	srv := httptest.NewServer(&fakegemini.Server{Cwd: w.cwd})
	t.Cleanup(srv.Close)
	// The first start asks nothing: its onboarding done and the folder trusted, as agy itself writes them.
	cli := filepath.Join(w.home, ".gemini", "antigravity-cli")
	os.MkdirAll(filepath.Join(cli, "cache"), 0o755)
	os.WriteFile(filepath.Join(cli, "cache", "onboarding.json"), []byte(`{"consumerOnboardingComplete":false,"enterpriseOnboardingComplete":true,"onboardingComplete":false}`), 0o644)
	real, _ := filepath.EvalSymlinks(w.cwd)
	trusted, _ := json.Marshal(map[string]any{"trustedWorkspaces": []string{w.cwd, real}})
	os.WriteFile(filepath.Join(cli, "settings.json"), trusted, 0o644)
	py, _ := exec.LookPath("python3")
	// A test must not update the Mac's own agy (AGY_CLI_DISABLE_AUTO_UPDATE); TZ=UTC, so a fixture tells no time zone.
	w.env = []string{"PATH=" + filepath.Dir(bin) + ":" + filepath.Dir(py) + ":/usr/bin:/bin", "HOME=" + w.home, "TERM=xterm-256color",
		"LANG=en_US.UTF-8", "TZ=UTC", "AGY_CLI_DISABLE_AUTO_UPDATE=1", "AGY_LLM_GATEWAY_URL=" + srv.URL, "AGY_LLM_GATEWAY_API_KEY=fake-key",
		"BROWSER=/usr/bin/true"}
	return w
}

// transcripts: the conversations' records under the HOME, by conversation.
func (w *world) transcripts() []string {
	m, _ := filepath.Glob(filepath.Join(w.home, ".gemini", "antigravity-cli", "brain", "*", ".system_generated", "logs", "transcript.jsonl"))
	return m
}

func (w *world) record() string {
	m := w.transcripts()
	if len(m) != 1 {
		return ""
	}
	b, _ := os.ReadFile(m[0])
	return string(b)
}

// shot is a 32×32 blue square, the image the scenarios send.
func shot() []byte {
	m := image.NewRGBA(image.Rect(0, 0, 32, 32))
	draw.Draw(m, m.Bounds(), &image.Uniform{color.RGBA{0x25, 0x63, 0xeb, 0xff}}, image.Point{}, draw.Src)
	var b bytes.Buffer
	png.Encode(&b, m)
	return b.Bytes()
}

// printed is a conversation of prompts sent one `agy -p` after another (-c).
type printed struct {
	name    string
	prompts []string
}

var printeds = []printed{
	{name: "turns", prompts: []string{"HELLO there", "LIST the files here", "CHANGE the notes", "WRITE a new file", "BREAK the build"}},
	{name: "gallery", prompts: []string{"THINK about a plan", "MARK up the notes", "HELP me with a helper", "LONG list please", "FAIL on purpose"}},
}

func runPrinted(t *testing.T, bin string, sc printed) *world {
	w := newWorld(t, bin)
	for i, p := range sc.prompts {
		args := []string{"-p", p, "--dangerously-skip-permissions", "--print-timeout", "90s"}
		if i > 0 {
			args = append([]string{"-c"}, args...)
		}
		cmd := exec.Command(bin, args...)
		cmd.Dir, cmd.Env = w.cwd, w.env
		out, err := cmd.CombinedOutput()
		if err != nil && !strings.Contains(p, "FAIL") {
			t.Fatalf("agy %v: %v\n%s", args, err, out)
		}
	}
	return w
}

// main is the conversation the person had: the one with their first prompt (a subagent has one of its own).
func (w *world) main(t *testing.T, first string) string {
	t.Helper()
	var got []string
	for _, f := range w.transcripts() {
		b, _ := os.ReadFile(f)
		line, _, _ := bytes.Cut(b, []byte("\n"))
		if bytes.Contains(line, []byte(`"source":"USER_EXPLICIT"`)) && bytes.Contains(line, []byte(first)) {
			got = append(got, f)
		}
	}
	if len(got) != 1 {
		t.Fatalf("%d conversations begin with %q, want 1", len(got), first)
	}
	return got[0]
}

func TestLivePrinted(t *testing.T) {
	bin := live(t)
	version := installedVersion(t, bin)
	for _, sc := range printeds {
		t.Run(sc.name, func(t *testing.T) {
			w := runPrinted(t, bin, sc)
			keep(t, w, w.main(t, sc.prompts[0]), version, sc.name)
		})
	}
}

// keep writes a record (and its full copy) as a fixture with -update; otherwise it checks the fresh record reads as
// the fixture's .want.json says.
func keep(t *testing.T, w *world, record, version, name string) {
	t.Helper()
	dir := filepath.Join("testdata", "antigravity", version, name)
	full := filepath.Join(filepath.Dir(record), "transcript_full.jsonl")
	short, _ := os.ReadFile(record)
	long, _ := os.ReadFile(full)
	out := dir
	if !*update {
		out = filepath.Join(t.TempDir(), name)
	}
	os.MkdirAll(out, 0o755)
	// Both files move by the same amount, so a step's times stay the same in each.
	s, d := fixtime.Shift(scrub(t, short, w.root))
	if err := os.WriteFile(filepath.Join(out, "transcript.jsonl"), s, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(out, "transcript_full.jsonl"), fixtime.By(scrub(t, long, w.root), d), 0o644); err != nil {
		t.Fatal(err)
	}
	if *update {
		t.Logf("wrote %s; write or check its want.json by hand", dir)
		return
	}
	checkWant(t, filepath.Join(out, "transcript.jsonl"), filepath.Join(dir, "want.json"))
}

// The fields of a step the reader reads; the rest (the model's id, token counts) is left out of a fixture.
var kept = []string{"step_index", "source", "type", "status", "created_at", "content", "thinking", "tool_calls", "media", "truncated_fields", "error"}

var mediaName = regexp.MustCompile(`uploaded_media_\d+`)

// scrub keeps what the reader reads and makes the rest anonymous: the run's folder becomes /work, and the times move
// to fixtime's made-up day (in keep).
func scrub(t *testing.T, b []byte, root string) []byte {
	var out bytes.Buffer
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 0, 1<<20), 64<<20)
	real, _ := filepath.EvalSymlinks(root)
	for sc.Scan() {
		var d map[string]json.RawMessage
		if err := json.Unmarshal(sc.Bytes(), &d); err != nil {
			t.Fatalf("scrub: %v", err)
		}
		var line bytes.Buffer
		line.WriteByte('{')
		first := true
		for _, k := range kept {
			v, ok := d[k]
			if !ok {
				continue
			}
			if !first {
				line.WriteByte(',')
			}
			first = false
			fmt.Fprintf(&line, "%q:%s", k, v)
		}
		line.WriteByte('}')
		s := line.String()
		for _, r := range []string{real, root} {
			if r != "" {
				s = strings.ReplaceAll(s, r, "/work")
			}
		}
		s = mediaName.ReplaceAllString(s, "uploaded_media_1")
		out.WriteString(s)
		out.WriteByte('\n')
	}
	return out.Bytes()
}

// --- on its screen -------------------------------------------------------------------------------------------------

func startTUI(t *testing.T, bin string, w *world, args ...string) *livetui.TUI {
	u := livetui.Start(t, w.env, w.cwd, bin, args...)
	u.Wait(60*time.Second, "the prompt", livetui.Has("? for shortcuts"))
	u.Settle()
	return u
}

// pid: the agent's process, as herdr-core reports it.
func pid(t *testing.T, u *livetui.TUI) int {
	t.Helper()
	var g struct {
		Foreground struct {
			Pid int `json:"pid"`
		} `json:"foreground"`
	}
	if err := json.Unmarshal([]byte(u.Get()), &g); err != nil || g.Foreground.Pid <= 0 {
		t.Fatalf("hc get: %v", err)
	}
	return g.Foreground.Pid
}

// found checks Find names the one record of the running agent.
func found(t *testing.T, r *Reader, p int, want string) {
	t.Helper()
	got, _, err := r.Find(p)
	if err != nil {
		t.Fatalf("Find(%d): %v", p, err)
	}
	g, _ := filepath.EvalSymlinks(got)
	wnt, _ := filepath.EvalSymlinks(want)
	if g != wnt {
		t.Fatalf("Find(%d) = %s, want %s", p, got, want)
	}
}

func TestLiveScreen(t *testing.T) {
	bin := live(t)
	version := installedVersion(t, bin)
	t.Run("ask", func(t *testing.T) {
		w := newWorld(t, bin)
		u := startTUI(t, bin, w)
		r := &Reader{Dir: filepath.Join(w.home, ".gemini", "antigravity-cli")}
		// Before its first prompt, agy has no conversation yet.
		if _, _, err := r.Find(pid(t, u)); err == nil || err.(*chat.Miss).Reason != chat.NoRecordYet {
			t.Fatalf("Find before a prompt: %v, want %s", err, chat.NoRecordYet)
		}
		u.Send("please ASK now")
		u.Wait(30*time.Second, "its first question", livetui.Has("Question 1/2", "(Recommended) Blue"))
		found(t, r, pid(t, u), w.transcripts()[0])
		keep(t, w, w.transcripts()[0], version, "ask")
		u.Keys("2")
		u.Wait(10*time.Second, "its second question", livetui.Has("Question 2/2"))
		u.Keys("1")
		u.Keys("2")
		u.Keys("enter")
		u.Until(30*time.Second, "done with the turn", func() bool { return strings.Contains(w.record(), "Noted your choice.") })
		found(t, r, pid(t, u), w.transcripts()[0])
		keep(t, w, w.transcripts()[0], version, "ask-answered")
		// /clear starts another conversation in the same process: Find follows it, though the old one stays open.
		old := w.transcripts()[0]
		u.Send("/clear")
		u.Settle()
		u.Send("please HELLO again")
		u.Until(30*time.Second, "a second conversation", func() bool { return len(w.transcripts()) == 2 })
		u.Settle()
		for _, f := range w.transcripts() {
			if f != old {
				found(t, r, pid(t, u), f)
			}
		}
	})
	t.Run("plan", func(t *testing.T) {
		w := newWorld(t, bin)
		u := startTUI(t, bin, w, "--mode", "plan")
		u.Send("please PLAN now")
		u.Wait(30*time.Second, "the plan file to allow", livetui.Has("Allow creation of this file?"))
		u.Keys("1")
		u.Until(30*time.Second, "the plan written", func() bool { return strings.Contains(w.record(), "ready for your review") })
		u.Settle()
		// Waiting for the review, agy tells herdr-core it is blocked ("Approve plan"), its prompt open.
		if !strings.Contains(u.Get(), `"msg":"Approve plan"`) {
			t.Fatalf("status while the plan waits: %s", u.Get())
		}
		u.SendForce("/artifact")
		u.Wait(10*time.Second, "the review", livetui.Has("Action required"))
		u.Keys("y")
		u.Keys("esc")
		u.Wait(30*time.Second, "the edit to accept", livetui.Has("Accept this file edit?"))
		u.Until(60*time.Second, "the plan carried out", func() bool {
			if sc := u.Screen(); strings.Contains(sc, "Accept this file edit?") && strings.Contains(sc, "1. Yes") {
				u.Keys("1")
				time.Sleep(time.Second)
			}
			return strings.Contains(w.record(), "Merged the notes.")
		})
		keep(t, w, w.transcripts()[0], version, "plan")
	})
	t.Run("queued", func(t *testing.T) {
		w := newWorld(t, bin)
		u := startTUI(t, bin, w, "--dangerously-skip-permissions")
		u.Send("please WORK now")
		u.Until(30*time.Second, "its first step", func() bool { return strings.Contains(w.record(), "First step") })
		u.Send("also HELLO there")
		u.Until(90*time.Second, "both done", func() bool {
			r := w.record()
			return strings.Contains(r, "All three steps are done.") && strings.Contains(r, "Hello from the fake model.")
		})
		u.Settle()
		keep(t, w, w.transcripts()[0], version, "queued")
	})
	t.Run("image", func(t *testing.T) {
		w := newWorld(t, bin)
		u := startTUI(t, bin, w, "--dangerously-skip-permissions")
		// A pasted image file's path alone is taken as the image, and waits for the prompt it goes with (as the phone
		// sends a picture: its path, then the words).
		u.Send(filepath.Join(w.cwd, "shot.png"))
		u.Wait(10*time.Second, "the image attached", livetui.Has("media attached"))
		u.Send("please HELLO, what is in this picture?")
		u.Until(30*time.Second, "the answer", func() bool { return strings.Contains(w.record(), "Hello from the fake model.") })
		keep(t, w, w.transcripts()[0], version, "image")
	})
	t.Run("stopped", func(t *testing.T) {
		w := newWorld(t, bin)
		u := startTUI(t, bin, w, "--dangerously-skip-permissions")
		u.Send("please SLOW now")
		u.Until(30*time.Second, "the slow command", func() bool { return strings.Contains(w.record(), "Minute wait") })
		time.Sleep(time.Second)
		u.Keys("esc")
		u.Wait(10*time.Second, "interrupted", livetui.Has("Interrupted"))
		u.Settle()
		u.Send("please HELLO again")
		u.Until(30*time.Second, "the answer", func() bool { return strings.Contains(w.record(), "Hello from the fake model.") })
		keep(t, w, w.transcripts()[0], version, "stopped")
	})
}
