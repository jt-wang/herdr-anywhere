package pi

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"herdr-anywhere/companion/internal/chat"
)

// norm drops what changes from run to run: ids, times, durations.
func norm(ts []chat.Turn) []chat.Turn {
	out := []chat.Turn{}
	for _, t := range ts {
		t.ID, t.Secs, t.At = "", 0, ""
		steps := []chat.Step{}
		for _, s := range t.Steps {
			s.ID = ""
			steps = append(steps, s)
		}
		t.Steps = steps
		out = append(out, t)
	}
	return out
}

// Every fixture written by Pi reads as its .want.json says. The same prompts against the same fake model as Codex's
// fixtures, so the same turns.
func TestFixtures(t *testing.T) {
	files, _ := filepath.Glob(filepath.Join("testdata", "pi", "*", "*.jsonl"))
	if len(files) == 0 {
		t.Fatal("no fixtures")
	}
	for _, f := range files {
		t.Run(f, func(t *testing.T) {
			b, err := os.ReadFile(strings.TrimSuffix(f, ".jsonl") + ".want.json")
			if err != nil {
				t.Fatal(err)
			}
			var want []chat.Turn
			if err := json.Unmarshal(b, &want); err != nil {
				t.Fatal(err)
			}
			got, err := (&Reader{}).Turns(f)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(norm(got), norm(want)) {
				g, _ := json.MarshalIndent(norm(got), "", "  ")
				t.Fatalf("turns differ; got\n%s", g)
			}
		})
	}
}

func step(t *testing.T, path, title string) string {
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
	t.Fatalf("no step %q", title)
	return ""
}

const steps = "testdata/pi/1.4.0/steps.jsonl"

func TestDetails(t *testing.T) {
	r := &Reader{}
	d, err := r.Detail(steps, step(t, steps, "echo checking; echo 'one test failed' >&2; exit 3"))
	if err != nil {
		t.Fatal(err)
	}
	if d.Run == nil || !d.Run.Failed || d.Run.Code != 3 || d.Run.Output != "checking\none test failed" || d.Run.Lines != 2 {
		t.Fatalf("run %+v", d.Run)
	}
	d, err = r.Detail(steps, step(t, steps, "notes.txt"))
	if err != nil {
		t.Fatal(err)
	}
	want := &chat.Change{File: "notes.txt", Added: 2, Hunks: []chat.Hunk{{OldStart: 1, NewStart: 1, Lines: []string{" a", "+b", "+c"}}}}
	if !reflect.DeepEqual(d.Change, want) {
		t.Fatalf("change %+v", d.Change)
	}
	d, err = r.Detail("testdata/pi/1.4.0/turns.jsonl", step(t, "testdata/pi/1.4.0/turns.jsonl", "hello.txt"))
	if err != nil {
		t.Fatal(err)
	}
	want = &chat.Change{File: "hello.txt", Added: 1, Hunks: []chat.Hunk{{OldStart: 0, NewStart: 1, Lines: []string{"+hello"}}}}
	if !reflect.DeepEqual(d.Change, want) {
		t.Fatalf("write %+v", d.Change)
	}
	d, err = r.Detail(steps, step(t, steps, "Weighing two plans: the short one first."))
	if err != nil || d.Output != "Weighing two plans: the short one first." {
		t.Fatalf("think %+v %v", d, err)
	}
	if _, err := r.Detail(steps, "nope"); !errors.Is(err, chat.ErrNoStep) {
		t.Fatalf("unknown step: %v", err)
	}
}

func TestImageOfARead(t *testing.T) {
	r := &Reader{}
	id := step(t, steps, "shot.png")
	d, err := r.Detail(steps, id)
	if err != nil || len(d.Images) != 1 {
		t.Fatalf("read %+v %v", d, err)
	}
	media, b, err := r.Image(steps, d.Images[0].ID)
	if err != nil || media != "image/png" || len(b) < 8 || string(b[1:4]) != "PNG" {
		t.Fatalf("image %s %d %v", media, len(b), err)
	}
}

// --- which conversation a pane shows ---------------------------------------------------------------------------

func pane(t *testing.T, labels map[string]string) (*Reader, chat.Pane, string) {
	s, _ := setup(t)
	f := filepath.Join(s.Dir, "sessions", "--work--", "2026-01-01T12-00-00-000Z_019b796d.jsonl")
	os.MkdirAll(filepath.Dir(f), 0o755)
	os.WriteFile(f, []byte(`{"type":"session","version":3,"id":"019b796d","cwd":"/work/acme"}`+"\n"), 0o644)
	for k, v := range labels {
		labels[k] = strings.ReplaceAll(v, "FILE", f)
	}
	return &Reader{Setup: s}, chat.Pane{Pid: 4242, Cwd: "/work/acme", Labels: labels}, f
}

func miss(err error) string {
	var m *chat.Miss
	if errors.As(err, &m) {
		return m.Reason
	}
	return ""
}

func TestFindPaneByWhatTheFileSaid(t *testing.T) {
	r, p, f := pane(t, map[string]string{chat.LabelPid: "4242", chat.LabelSession: "FILE"})
	r.Setup.Install(false)
	got, _, err := r.FindPane(p)
	if err != nil || got != f {
		t.Fatalf("got %q %v", got, err)
	}
	// Said by an earlier Pi in that terminal: not this one's.
	p.Pid = 5151
	if _, _, err := r.FindPane(p); miss(err) != chat.SetupNotLoaded {
		t.Fatalf("another process: %v", err)
	}
}

// Without the file there is nothing to go by: the phone offers to put it there.
func TestFindPaneWithoutTheFile(t *testing.T) {
	r, p, _ := pane(t, map[string]string{})
	if _, _, err := r.FindPane(p); miss(err) != chat.SetupNeeded {
		t.Fatalf("no file: %v", err)
	}
	// The file is there, but this Pi started before it: /reload or a restart.
	r.Setup.Install(false)
	if _, _, err := r.FindPane(p); miss(err) != chat.SetupNotLoaded {
		t.Fatalf("not loaded: %v", err)
	}
}

// A conversation file not yet written (Pi writes it at the first prompt) is a record to come.
func TestFindPaneBeforeTheFirstPrompt(t *testing.T) {
	r, p, f := pane(t, map[string]string{chat.LabelPid: "4242", chat.LabelSession: "FILE"})
	r.Setup.Install(false)
	os.Remove(f)
	if _, _, err := r.FindPane(p); miss(err) != chat.NoRecordYet {
		t.Fatalf("no file yet: %v", err)
	}
}

// Only a conversation file in Pi's own sessions folder is read, whatever a label says.
func TestFindPaneReadsOnlyPisSessions(t *testing.T) {
	r, p, _ := pane(t, map[string]string{chat.LabelPid: "4242", chat.LabelSession: "/etc/passwd"})
	r.Setup.Install(false)
	if _, _, err := r.FindPane(p); miss(err) != chat.NoMatch {
		t.Fatalf("outside: %v", err)
	}
}
