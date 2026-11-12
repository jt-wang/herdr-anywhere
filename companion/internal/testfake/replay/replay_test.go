package replay

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"herdr-anywhere/companion/internal/chat"
	"herdr-anywhere/companion/internal/chat/claude"
	"herdr-anywhere/companion/internal/chat/codex"
	"herdr-anywhere/companion/internal/chat/opencode"
	"herdr-anywhere/companion/internal/chat/pi"
)

// The recordings each agent's reader is tested with; the fakes replay the same ones.
var cases = []struct {
	agent    string
	fixtures []string
	reader   func(dir string) chat.Reader
}{
	{"claude-code", fx("claude/testdata/claude-code/2.1.322", "turns.jsonl", "steps.jsonl", "image.jsonl"),
		func(dir string) chat.Reader { return &claude.Reader{Dir: dir} }},
	{"codex", fx("codex/testdata/codex/0.167.1", "turns.jsonl", "steps.jsonl", "files.jsonl"),
		func(dir string) chat.Reader { return &codex.Reader{Dir: dir} }},
	{"pi", fx("pi/testdata/pi/1.4.0", "turns.jsonl", "steps.jsonl", "files.jsonl"),
		func(dir string) chat.Reader { return &pi.Reader{} }},
	{"opencode", fx("opencode/testdata/opencode/2.2.0", "turns.json", "steps.json", "files.json"),
		func(dir string) chat.Reader {
			return &opencode.Reader{DB: filepath.Join(dir, "opencode", "opencode.db")}
		}},
}

func fx(dir string, names ...string) []string {
	var out []string
	for _, n := range names {
		out = append(out, filepath.Join("..", "..", "chat", filepath.FromSlash(dir), n))
	}
	return out
}

// A long conversation played in at once, then a prompt typed: the agent's own reader reads the last turns, numbered
// in order, the typed prompt last, every turn finished, no two turns or steps with the same id.
func TestLongConversationReadsBack(t *testing.T) {
	for _, c := range cases {
		t.Run(c.agent, func(t *testing.T) {
			s, err := Load(c.agent, c.fixtures...)
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			p, err := s.Start(Options{Dir: dir, Cwd: "/work/demo-shop", Pid: 4242})
			if err != nil {
				t.Fatal(err)
			}
			defer p.Close()
			if err := p.Preload(200); err != nil {
				t.Fatal(err)
			}
			if err := p.Play("HELLO from the test", 0); err != nil {
				t.Fatal(err)
			}
			turns, err := c.reader(dir).Turns(p.Path())
			if err != nil {
				t.Fatal(err)
			}
			if len(turns) != chat.MaxTurns {
				t.Fatalf("read %d turns, want the last %d", len(turns), chat.MaxTurns)
			}
			last := turns[len(turns)-1]
			if last.Said != "HELLO from the test" {
				t.Fatalf("last turn said %q", last.Said)
			}
			prev, numbered := 0, 0
			ids := map[string]bool{}
			for i, tn := range turns {
				if ids["turn "+tn.ID] {
					t.Fatalf("turn %d: id %s twice", i, tn.ID)
				}
				ids["turn "+tn.ID] = true
				for _, st := range tn.Steps {
					if ids["step "+st.ID] {
						t.Fatalf("turn %d: step id %s twice", i, st.ID)
					}
					ids["step "+st.ID] = true
				}
				if i < len(turns)-1 && tn.End != "done" {
					t.Fatalf("turn %d (%q) ends %q", i, tn.Said, tn.End)
				}
				if strings.Contains(tn.Said, "/work/acme") || strings.Contains(tn.Reply, "/work/acme") {
					t.Fatalf("turn %d still names the recording's folder", i)
				}
				var n int
				if tn.From == "you" && i < len(turns)-1 {
					if _, err := fmt.Sscanf(tn.Said, "%03d ", &n); err != nil {
						t.Fatalf("turn %d said %q, not numbered", i, tn.Said)
					}
					if n <= prev {
						t.Fatalf("turn %d is number %d after %d", i, n, prev)
					}
					prev = n
					numbered++
				}
			}
			if prev != 200 {
				t.Fatalf("the last numbered turn is %d, want 200", prev)
			}
			if numbered < chat.MaxTurns/2 {
				t.Fatalf("only %d numbered turns", numbered)
			}
		})
	}
}

// Long: the LIST command printed this many lines, all invented; its step's details count them all.
func TestLongOutput(t *testing.T) {
	for _, c := range cases {
		t.Run(c.agent, func(t *testing.T) {
			s, err := Load(c.agent, c.fixtures[0])
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			p, err := s.Start(Options{Dir: dir, Cwd: "/work/demo-shop", Pid: 4242, Long: 3000})
			if err != nil {
				t.Fatal(err)
			}
			defer p.Close()
			if err := p.Play("LIST the files here", 0); err != nil {
				t.Fatal(err)
			}
			r := c.reader(dir)
			turns, err := r.Turns(p.Path())
			if err != nil {
				t.Fatal(err)
			}
			for _, tn := range turns {
				for _, st := range tn.Steps {
					if st.Kind != "run" {
						continue
					}
					d, err := r.Detail(p.Path(), st.ID)
					if err != nil {
						t.Fatal(err)
					}
					if d.Run == nil || d.Run.Lines < 3000 {
						t.Fatalf("the command's details: %+v", d.Run)
					}
					if !strings.Contains(d.Run.Output, "line 3000 of the long listing") {
						t.Fatalf("the output's end: %q", tail(d.Run.Output))
					}
					return
				}
			}
			t.Fatalf("no command step in %d turns", len(turns))
		})
	}
}

// The step that looked at a picture keeps it: the reader finds the image again by its id.
func TestImagesStay(t *testing.T) {
	for _, c := range cases {
		t.Run(c.agent, func(t *testing.T) {
			s, err := Load(c.agent, c.fixtures...)
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			p, err := s.Start(Options{Dir: dir, Cwd: "/work/demo-shop", Pid: 4242})
			if err != nil {
				t.Fatal(err)
			}
			defer p.Close()
			if err := p.Preload(60); err != nil {
				t.Fatal(err)
			}
			r := c.reader(dir)
			turns, err := r.Turns(p.Path())
			if err != nil {
				t.Fatal(err)
			}
			found := 0
			for _, tn := range turns {
				for _, im := range tn.Images {
					if _, data, err := r.Image(p.Path(), im.ID); err != nil || len(data) == 0 {
						t.Fatalf("turn image %s: %v", im.ID, err)
					}
					found++
				}
				for _, st := range tn.Steps {
					d, err := r.Detail(p.Path(), st.ID)
					if err != nil {
						continue
					}
					for _, im := range d.Images {
						if _, data, err := r.Image(p.Path(), im.ID); err != nil || len(data) == 0 {
							t.Fatalf("step image %s: %v", im.ID, err)
						}
						found++
					}
				}
			}
			if found < 2 {
				t.Fatalf("%d images in the last turns of 60 played, each recording turn twice or more", found)
			}
		})
	}
}

func tail(s string) string {
	if len(s) > 200 {
		return s[len(s)-200:]
	}
	return s
}

// A prompt that starts with a recorded prompt's first word plays that turn (a test picks which), whatever came
// before; one that matches none plays the next turn.
func TestPlayFor(t *testing.T) {
	for _, c := range cases {
		t.Run(c.agent, func(t *testing.T) {
			s, err := Load(c.agent, c.fixtures...)
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			p, err := s.Start(Options{Dir: dir, Cwd: "/work/demo-shop", Pid: 4242})
			if err != nil {
				t.Fatal(err)
			}
			defer p.Close()
			if err := p.Preload(5); err != nil {
				t.Fatal(err)
			}
			if err := p.PlayFor("BREAK it again, please", 0); err != nil {
				t.Fatal(err)
			}
			turns, err := c.reader(dir).Turns(p.Path())
			if err != nil {
				t.Fatal(err)
			}
			last := turns[len(turns)-1]
			failed := false
			for _, st := range last.Steps {
				failed = failed || st.Failed
			}
			if last.Said != "BREAK it again, please" || !failed {
				t.Fatalf("last turn %q, a failed step: %v (the BREAK turn has one)", last.Said, failed)
			}
		})
	}
}
