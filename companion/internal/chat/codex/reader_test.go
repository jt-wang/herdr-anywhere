package codex

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"herdr-anywhere/companion/internal/chat"
)

// norm drops what changes from run to run: ids, times, durations.
func norm(ts []chat.Turn) []chat.Turn {
	out := []chat.Turn{}
	for _, t := range ts {
		t.ID, t.Secs, t.At = "", 0, ""
		var steps []chat.Step
		for _, s := range t.Steps {
			s.ID = ""
			steps = append(steps, s)
		}
		if steps == nil {
			steps = []chat.Step{}
		}
		t.Steps = steps
		out = append(out, t)
	}
	return out
}

func checkWant(t *testing.T, path, want string) {
	t.Helper()
	b, err := os.ReadFile(want)
	if err != nil {
		t.Fatalf("no %s: %v", want, err)
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
		t.Fatalf("%s: turns differ from %s; got\n%s", path, want, g)
	}
}

// Every fixture written by an installed Codex reads as its .want.json says.
func TestFixtures(t *testing.T) {
	files, _ := filepath.Glob(filepath.Join("testdata", "codex", "*", "*.jsonl"))
	if len(files) == 0 {
		t.Fatal("no fixtures")
	}
	for _, f := range files {
		t.Run(f, func(t *testing.T) { checkWant(t, f, strings.TrimSuffix(f, ".jsonl")+".want.json") })
	}
}

// stepByTitle: the id of the first step titled so in a fixture.
func stepByTitle(t *testing.T, path, title string) string {
	t.Helper()
	ts, err := (&Reader{}).Turns(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, tu := range ts {
		for _, s := range tu.Steps {
			if s.Title == title {
				return s.ID
			}
		}
	}
	t.Fatalf("no step %q in %s", title, path)
	return ""
}

// A command shows what it printed and its exit code; a change its lines, numbered from its hunk; a look the image
// Codex sent the model.
func TestDetails(t *testing.T) {
	steps := filepath.Join("testdata", "codex", "0.167.1", "steps.jsonl")
	r := &Reader{}
	d, err := r.Detail(steps, stepByTitle(t, steps, "echo checking; echo 'one test failed' >&2; exit 3"))
	if err != nil || d.Run == nil {
		t.Fatalf("%v %+v", err, d)
	}
	if !d.Run.Failed || d.Run.Code != 3 || d.Run.Output != "checking\none test failed\n" || d.Run.Lines != 2 {
		t.Errorf("run: %+v", d.Run)
	}
	d, err = r.Detail(steps, stepByTitle(t, steps, "notes.txt"))
	if err != nil || d.Change == nil {
		t.Fatalf("%v %+v", err, d)
	}
	want := chat.Change{File: "notes.txt", Added: 2, Hunks: []chat.Hunk{{OldStart: 1, NewStart: 1, Lines: []string{" a", "+b", "+c"}}}}
	if !reflect.DeepEqual(*d.Change, want) {
		t.Errorf("change: %+v", *d.Change)
	}
	look := stepByTitle(t, steps, "shot.png")
	d, err = r.Detail(steps, look)
	if err != nil || len(d.Images) != 1 || d.Images[0].Source != "/work/acme/shot.png" {
		t.Fatalf("look: %v %+v", err, d)
	}
	media, b, err := r.Image(steps, d.Images[0].ID)
	if err != nil || media != "image/png" || len(b) < 8 || string(b[1:4]) != "PNG" {
		t.Fatalf("image: %v %q %d bytes", err, media, len(b))
	}
	turns := filepath.Join("testdata", "codex", "0.167.1", "turns.jsonl")
	d, err = r.Detail(turns, stepByTitle(t, turns, "hello.txt"))
	if err != nil || d.Change == nil || !reflect.DeepEqual(d.Change.Hunks, []chat.Hunk{{OldStart: 0, NewStart: 1, Lines: []string{"+hello"}}}) {
		t.Fatalf("a new file: %v %+v", err, d.Change)
	}
	if _, err := r.Detail(steps, "made-up"); err != chat.ErrNoStep {
		t.Fatalf("a made-up step: %v", err)
	}
}

// A process that holds a file open, for Find: the test binary itself, run again (CODEX_TEST_HOLD names the file).
func TestMain(m *testing.M) {
	if p := os.Getenv("CODEX_TEST_HOLD"); p != "" {
		f, err := os.Open(p)
		if err != nil {
			os.Exit(2)
		}
		defer f.Close()
		os.Stdout.WriteString("open\n")
		var b [1]byte
		os.Stdin.Read(b[:]) // until the test closes our stdin
		return
	}
	os.Exit(m.Run())
}

func hold(t *testing.T, path string) int {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), "CODEX_TEST_HOLD="+path)
	in, _ := cmd.StdinPipe()
	out, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { in.Close(); cmd.Wait() })
	b := make([]byte, 5)
	if _, err := io.ReadFull(out, b); err != nil || string(b) != "open\n" {
		t.Fatalf("the helper did not open %s: %v %q", path, err, b)
	}
	return cmd.Process.Pid
}

// Find: the rollout file under Codex's home that the process has open; none yet before the first prompt; another
// home's file is not this Codex's.
func TestFind(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("open files are not read on Windows")
	}
	home := t.TempDir()
	day := filepath.Join(home, "sessions", "2026", "01", "01")
	os.MkdirAll(day, 0o755)
	file := filepath.Join(day, "rollout-2026-01-01T21-00-00-019b796d-d848-7930-b89b-e8779ff67643.jsonl")
	src, _ := os.ReadFile(filepath.Join("testdata", "codex", "0.167.1", "turns.jsonl"))
	os.WriteFile(file, src, 0o644)
	other := filepath.Join(t.TempDir(), "notes.txt")
	os.WriteFile(other, []byte("a\n"), 0o644)

	r := &Reader{Dir: home}
	got, v, err := r.Find(hold(t, file))
	real, _ := filepath.EvalSymlinks(file)
	if err != nil || got != real || v != "0.167.1" {
		t.Fatalf("Find: %q %q %v, want %q", got, v, err, real)
	}
	_, _, err = r.Find(hold(t, other))
	var m *chat.Miss
	if !errors.As(err, &m) || m.Reason != chat.NoRecordYet {
		t.Fatalf("a Codex with no record yet: %v", err)
	}
	if _, _, err := (&Reader{Dir: t.TempDir()}).Find(hold(t, file)); !errors.As(err, &m) || m.Reason != chat.NoRecordYet {
		t.Fatalf("a file under another home: %v", err)
	}
}

// A Codex running helper agents holds their rollouts open too (their session_meta source is a subagent): the one
// root thread among them is the conversation; two roots stay ambiguous.
func TestFindPicksTheRootThread(t *testing.T) {
	home := t.TempDir()
	day := filepath.Join(home, "sessions", "2026", "01", "01")
	os.MkdirAll(day, 0o755)
	src, _ := os.ReadFile(filepath.Join("testdata", "codex", "0.167.1", "turns.jsonl"))
	write := func(id, source string) string {
		f := filepath.Join(day, "rollout-2026-01-01T21-00-00-"+id+".jsonl")
		meta := `{"timestamp":"2026-01-01T21:00:00.000Z","type":"session_meta","payload":{"id":"` + id + `","cli_version":"0.167.1","source":` + source + `}}` + "\n"
		os.WriteFile(f, append([]byte(meta), src...), 0o644)
		real, _ := filepath.EvalSymlinks(f)
		return real
	}
	root := write("019b796d-d848-7930-b89b-e8779ff67643", `"cli"`)
	helper := write("019b796d-d848-7930-b89b-e8779ff67644", `{"subagent":{"thread_spawn":{"parent_thread_id":"019b796d-d848-7930-b89b-e8779ff67643","depth":1}}}`)
	other := write("019b796d-d848-7930-b89b-e8779ff67645", `"vscode"`)
	open := func(files ...string) *Reader {
		return &Reader{Dir: home, openFiles: func([]int) ([]string, error) { return files, nil }}
	}
	if got, _, err := open(helper, root).Find(1); err != nil || got != root {
		t.Fatalf("root and helper: %q %v, want %q", got, err, root)
	}
	var m *chat.Miss
	if _, _, err := open(root, other, helper).Find(1); !errors.As(err, &m) || m.Reason != chat.Ambiguous {
		t.Fatalf("two roots: %v", err)
	}
	if _, _, err := open(helper).Find(1); !errors.As(err, &m) || m.Reason != chat.Ambiguous {
		t.Fatalf("only a helper's: %v", err)
	}
}
