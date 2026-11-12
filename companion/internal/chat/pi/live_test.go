package pi

// The installed Pi against a fake Responses API (fakeresponses), in a throwaway agent folder: its sessions read as
// the fixtures of its version say. Opt in, as it runs the real program:
//
//	PI_LIVE=1 go test ./internal/chat/pi -run Live            # check the installed version
//	PI_LIVE=1 go test ./internal/chat/pi -run Live -update    # write testdata/pi/<version>/ for it
//
// Pi runs in print mode (-p; later prompts -c, which continues the folder's last session) with its version check
// off and offline, so it neither asks nor updates.

import (
	"bytes"
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

var update = flag.Bool("update", false, "write the fixtures for the installed version")

var scenarios = []struct {
	name    string
	prompts []string
}{
	{name: "turns", prompts: []string{"LIST the files here", "EDIT a file for me", "HELLO again"}},
	{name: "steps", prompts: []string{"CHANGE the notes", "BREAK the build", "THINK about a plan", "LOOK at shot.png"}},
	{name: "files", prompts: []string{"RENDER the demo video"}},
}

func live(t *testing.T) string {
	t.Helper()
	if os.Getenv("PI_LIVE") == "" {
		t.Skip("PI_LIVE=1 runs the installed Pi")
	}
	bin, err := exec.LookPath("pi")
	if err != nil {
		t.Skip("no pi on PATH")
	}
	return bin
}

func shot() []byte {
	m := image.NewRGBA(image.Rect(0, 0, 32, 32))
	draw.Draw(m, m.Bounds(), &image.Uniform{color.RGBA{0x25, 0x63, 0xeb, 0xff}}, image.Point{}, draw.Src)
	var b bytes.Buffer
	png.Encode(&b, m)
	return b.Bytes()
}

// pi runs the installed Pi once in dir with env and returns what it printed.
func pi(t *testing.T, bin, dir string, env []string, args ...string) string {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Dir, cmd.Env, cmd.Stdin = dir, env, nil
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
			t.Fatalf("pi %v: %v\n%s", args, err, out.String())
		}
	case <-time.After(90 * time.Second):
		cmd.Process.Kill()
		t.Fatalf("pi %v: no end after 90 s\n%s", args, out.String())
	}
	return out.String()
}

type run struct {
	bin, root, cwd string
	env            []string
}

func liveRun(t *testing.T, bin string) run {
	root := t.TempDir()
	home, cwd := filepath.Join(root, "home"), filepath.Join(root, "acme")
	agent := filepath.Join(home, ".pi", "agent")
	for _, d := range []string{agent, cwd} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	os.WriteFile(filepath.Join(cwd, "notes.txt"), []byte("a\n"), 0o644)
	os.WriteFile(filepath.Join(cwd, "todo.txt"), []byte("b\n"), 0o644)
	os.WriteFile(filepath.Join(cwd, "shot.png"), shot(), 0o644)
	srv := httptest.NewServer(&fakeresponses.Server{})
	t.Cleanup(srv.Close)
	models := `{"providers": {"fake": {"baseUrl": "` + srv.URL + `/v1", "api": "openai-responses", "apiKey": "fake",
  "models": [{"id": "gpt-5.5", "name": "Fake GPT"}]}}}`
	os.WriteFile(filepath.Join(agent, "models.json"), []byte(models), 0o644)
	env := []string{"PATH=" + os.Getenv("PATH"), "HOME=" + home, "PI_CODING_AGENT_DIR=" + agent, "PI_SKIP_VERSION_CHECK=1",
		"PI_OFFLINE=1", "TERM=dumb", "TZ=UTC"}
	return run{bin: bin, root: root, cwd: cwd, env: env}
}

// session runs one scenario and returns its session file.
func (r run) session(t *testing.T, prompts []string) string {
	for i, p := range prompts {
		args := []string{"--model", "fake/gpt-5.5", "-p", p}
		if i > 0 {
			args = append([]string{"-c"}, args...)
		}
		pi(t, r.bin, r.cwd, r.env, args...)
	}
	files, _ := filepath.Glob(filepath.Join(r.root, "home", ".pi", "agent", "sessions", "*", "*.jsonl"))
	if len(files) != 1 {
		t.Fatalf("%d session files: %v", len(files), files)
	}
	return files[0]
}

// scrub: the run's folders become /work, and the computer's home /home (Pi's screen writes where Pi is installed into
// the system prompt it records).
func scrub(t *testing.T, b []byte, root string) []byte {
	real, _ := filepath.EvalSymlinks(root)
	home, _ := os.UserHomeDir()
	var out bytes.Buffer
	for _, line := range strings.Split(strings.TrimRight(string(b), "\n"), "\n") {
		if !json.Valid([]byte(line)) {
			t.Fatalf("not JSON: %.200s", line)
		}
		s := line
		for _, r := range []string{real, root} {
			if r != "" {
				s = strings.ReplaceAll(s, r, "/work")
			}
		}
		if home != "" && home != "/" {
			s = strings.ReplaceAll(s, home+"/", "/home/")
		}
		out.WriteString(s + "\n")
	}
	// On the made-up day (fixtime): a published fixture tells nothing of when it was recorded.
	shifted, _ := fixtime.Shift(out.Bytes())
	return shifted
}

func TestLiveSessions(t *testing.T) {
	bin := live(t)
	v := strings.TrimSpace(pi(t, bin, t.TempDir(), liveRun(t, bin).env, "--version"))
	if v == "" || strings.ContainsAny(v, " \n/") {
		t.Fatalf("pi --version said %q", v)
	}
	dir := filepath.Join("testdata", "pi", v)
	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			r := liveRun(t, bin)
			b, _ := os.ReadFile(r.session(t, sc.prompts))
			clean := scrub(t, b, r.root)
			if *update {
				os.MkdirAll(dir, 0o755)
				if err := os.WriteFile(filepath.Join(dir, sc.name+".jsonl"), clean, 0o644); err != nil {
					t.Fatal(err)
				}
				t.Logf("wrote %s/%s.jsonl; write or check its .want.json by hand", dir, sc.name)
				return
			}
			want := filepath.Join(dir, sc.name+".want.json")
			if _, err := os.Stat(want); err != nil {
				t.Fatalf("no fixtures for Pi %s (%s): look at a session, then run with -update", v, want)
			}
			p := filepath.Join(t.TempDir(), "s.jsonl")
			os.WriteFile(p, clean, 0o644)
			checkWant(t, p, want)
		})
	}
}

func checkWant(t *testing.T, path, want string) {
	t.Helper()
	b, err := os.ReadFile(want)
	if err != nil {
		t.Fatal(err)
	}
	var w []chat.Turn
	if err := json.Unmarshal(b, &w); err != nil {
		t.Fatalf("%s: %v", want, err)
	}
	got, err := (&Reader{}).Turns(path)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	if !reflect.DeepEqual(norm(got), norm(w)) {
		g, _ := json.MarshalIndent(norm(got), "", "  ")
		t.Fatalf("turns differ from %s; got\n%s", want, g)
	}
}
