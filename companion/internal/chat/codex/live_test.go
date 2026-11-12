package codex

// The installed Codex against a fake Responses API (fakeresponses), in a throwaway CODEX_HOME: its sessions read as
// the fixtures of its version say. Opt in, as it runs the real program:
//
//	CODEX_LIVE=1 go test ./internal/chat/codex -run Live            # check the installed version
//	CODEX_LIVE=1 go test ./internal/chat/codex -run Live -update    # write testdata/codex/<version>/ for it
//
// The throwaway config turns off Codex's update check (its update prompt picks "Update now" on Enter) and lets it
// write in the session's folder, resumed turns too (`exec resume` takes no --sandbox).

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
	"regexp"
	"strings"
	"testing"
	"time"

	"herdr-anywhere/companion/internal/chat/fakeresponses"
	"herdr-anywhere/companion/internal/chat/fixtime"
)

var update = flag.Bool("update", false, "write the fixtures for the installed version")

type scenario struct {
	name    string
	prompts []string
}

var scenarios = []scenario{
	{name: "turns", prompts: []string{"LIST the files here", "EDIT a file for me", "HELLO again"}},
	{name: "steps", prompts: []string{"CHANGE the notes", "BREAK the build", "THINK about a plan", "LOOK at shot.png"}},
	{name: "files", prompts: []string{"RENDER the demo video"}},
}

func live(t *testing.T) string {
	t.Helper()
	if os.Getenv("CODEX_LIVE") == "" {
		t.Skip("CODEX_LIVE=1 runs the installed Codex")
	}
	bin, err := exec.LookPath("codex")
	if err != nil {
		t.Skip("no codex on PATH")
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

// session runs one scenario and returns its rollout file and the folder it ran in.
func session(t *testing.T, bin string, sc scenario) (string, string) {
	root := t.TempDir()
	home, cwd := filepath.Join(root, "codex-home"), filepath.Join(root, "acme")
	for _, d := range []string{home, cwd} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	os.WriteFile(filepath.Join(cwd, "notes.txt"), []byte("a\n"), 0o644)
	os.WriteFile(filepath.Join(cwd, "todo.txt"), []byte("b\n"), 0o644)
	os.WriteFile(filepath.Join(cwd, "shot.png"), shot(), 0o644)
	srv := httptest.NewServer(&fakeresponses.Server{})
	t.Cleanup(srv.Close)
	cfg := "check_for_update_on_startup = false\nmodel = \"gpt-5.5\"\nmodel_provider = \"fake\"\nsandbox_mode = \"workspace-write\"\n" +
		"[model_providers.fake]\nname = \"Fake\"\nbase_url = \"" + srv.URL + "/v1\"\nwire_api = \"responses\"\nenv_key = \"FAKE_API_KEY\"\n"
	os.WriteFile(filepath.Join(home, "config.toml"), []byte(cfg), 0o644)
	env := []string{"PATH=" + os.Getenv("PATH"), "HOME=" + home, "CODEX_HOME=" + home, "FAKE_API_KEY=fake", "TERM=dumb", "TZ=UTC"}
	for i, p := range sc.prompts {
		args := []string{"exec", "--skip-git-repo-check", p}
		if i > 0 {
			args = []string{"exec", "resume", "--skip-git-repo-check", "--last", p}
		}
		cmd := exec.Command(bin, args...)
		cmd.Dir, cmd.Env, cmd.Stdin = cwd, env, nil
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
				t.Fatalf("%s: %v\n%s", p, err, out.String())
			}
		case <-time.After(90 * time.Second):
			cmd.Process.Kill()
			t.Fatalf("%s: no end after 90 s\n%s", p, out.String())
		}
	}
	files, _ := filepath.Glob(filepath.Join(home, "sessions", "*", "*", "*", "rollout-*.jsonl"))
	if len(files) != 1 {
		t.Fatalf("%d rollout files: %v", len(files), files)
	}
	return files[0], root
}

var long = regexp.MustCompile(`"(base_instructions|encrypted_content|developer_instructions)":("(?:[^"\\]|\\.)*"|\{(?:[^{}]|\{[^{}]*\})*\})`)

// today is the day Codex tells the model it is (in its environment context): left out of the fixtures, as it says
// when they were made.
var today = regexp.MustCompile(`(<current_date>)[^<]*(</current_date>)|("current_date":")[^"]*(")`)

// scrub: the run's folders become /work; the long texts Codex sends the model as its own instructions, and the day it
// was run, go.
func scrub(t *testing.T, b []byte, root string) []byte {
	real, _ := filepath.EvalSymlinks(root)
	var out bytes.Buffer
	for _, line := range strings.Split(strings.TrimRight(string(b), "\n"), "\n") {
		if !json.Valid([]byte(line)) {
			t.Fatalf("not JSON: %.200s", line)
		}
		s := long.ReplaceAllString(line, `"$1":"(removed)"`)
		s = today.ReplaceAllString(s, "$1$3(removed)$2$4")
		for _, r := range []string{real, root} {
			if r != "" {
				s = strings.ReplaceAll(s, r, "/work")
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
	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			f, root := session(t, bin, sc)
			b, _ := os.ReadFile(f)
			clean := scrub(t, b, root)
			v := version(f)
			if v == "" {
				t.Fatalf("no cli_version in %s", f)
			}
			dir := filepath.Join("testdata", "codex", v)
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
