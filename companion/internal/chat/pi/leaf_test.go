package pi

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"herdr-anywhere/companion/internal/chat"
)

func forked() *conv {
	c := &conv{}
	c.user("u1", "first")
	c.answer("a1", "first answer")
	c.user("u2", "abandoned")
	c.answer("a2", "abandoned answer")
	return c
}

func wantTurns(t *testing.T, r *Reader, path string, ids ...string) {
	t.Helper()
	ts, err := r.Turns(path)
	if err != nil {
		t.Fatal(err)
	}
	got := []string{}
	if ids == nil {
		ids = []string{}
	}
	for _, turn := range ts {
		got = append(got, turn.ID)
	}
	if !reflect.DeepEqual(got, ids) {
		t.Fatalf("turn IDs %v, want %v", got, ids)
	}
}

func TestLiveTreeSwitchWithoutAppend(t *testing.T) {
	for _, agent := range []string{"pi", "omp"} {
		t.Run(agent, func(t *testing.T) {
			r, p, file := pane(t, map[string]string{chat.LabelPid: "4242", chat.LabelSession: "FILE"})
			r.Setup.ID = agent
			if err := r.Setup.Install(false); err != nil {
				t.Fatal(err)
			}
			contents, _ := os.ReadFile(forked().write(t))
			if err := os.WriteFile(file, contents, 0o600); err != nil {
				t.Fatal(err)
			}
			var previous string
			for _, snapshot := range []string{"a2:a2", "a1:a2", "~:a2", "a2:a2"} {
				p.Labels[leafLabel] = snapshot
				path, _, err := r.FindPane(p)
				if err != nil || path == previous {
					t.Fatalf("selection %s: path %q, %v", snapshot, path, err)
				}
				previous = path
				switch snapshot {
				case "a1:a2":
					wantTurns(t, r, path, "u1")
				case "~:a2":
					wantTurns(t, r, path)
				default:
					wantTurns(t, r, path, "u1", "u2")
				}
			}
			p.Labels[leafLabel] = "a1:a2"
			path, _, _ := r.FindPane(p)
			c := forked()
			c.last = "a1"
			c.user("u3", "new branch")
			c.answer("a3", "new answer")
			contents, _ = os.ReadFile(c.write(t))
			if err := os.WriteFile(file, contents, 0o600); err != nil {
				t.Fatal(err)
			}
			appended, _, err := r.FindPane(p)
			if err != nil || appended != path {
				t.Fatalf("appends must not reset selection identity: %q %q %v", path, appended, err)
			}
			wantTurns(t, r, path, "u1", "u3")
			page, err := r.History(path, "")
			if err != nil || len(page.Turns) != 2 || page.Turns[1].ID != "u3" {
				t.Fatalf("history: %+v %v", page, err)
			}
		})
	}
}

func TestTreeSwitchCacheReplacesJournalSelection(t *testing.T) {
	c := forked()
	file := c.write(t)
	r := &Reader{}
	for i := 0; i < 40; i++ {
		c.last = "a1"
		id := fmt.Sprint(i)
		c.user("u-new"+id, "new branch")
		c.answer("a-new"+id, "new answer")
		contents, err := os.ReadFile(c.write(t))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, contents, 0o600); err != nil {
			t.Fatal(err)
		}
		for _, leaf := range []string{"a1", "a-new" + id} {
			path, err := selectedPath(file, leaf+":a-new"+id, 0)
			if err != nil {
				t.Fatal(err)
			}
			if leaf == "a1" {
				wantTurns(t, r, path, "u1")
			} else {
				wantTurns(t, r, path, "u1", "u-new"+id)
			}
			if len(r.cache) != 1 {
				t.Fatalf("%d snapshots retained", len(r.cache))
			}
		}
	}
}

func TestAppendToNonActiveBranch(t *testing.T) {
	for _, leaf := range []string{`"a1"`, `null`, `"a2"`} {
		t.Run(leaf, func(t *testing.T) {
			c := forked()
			c.ls = append(c.ls, `{"type":"message","id":"off","parentId":"a2","activeLeafId":`+leaf+`,"message":{"role":"user","content":"not active"}}`)
			file := c.write(t)
			r := &Reader{}
			for _, snapshot := range []string{"", "a1:a2"} {
				path, err := selectedPath(file, snapshot, 0)
				if err != nil {
					t.Fatal(err)
				}
				switch leaf {
				case `null`:
					wantTurns(t, r, path)
				case `"a1"`:
					wantTurns(t, r, path, "u1")
				default:
					wantTurns(t, r, path, "u1", "u2")
				}
			}
		})
	}
	c := forked()
	c.ls = append(c.ls, `{"type":"message","id":"bad","activeLeafId":42}`)
	var format *chat.FormatError
	if _, err := (&Reader{}).Turns(c.write(t)); !errors.As(err, &format) {
		t.Fatalf("invalid persisted leaf: %v", err)
	}
}

func TestLiveLeafBeforeTailWindow(t *testing.T) {
	c := forked()
	c.user("large", strings.Repeat("x", chat.MaxTail+1024))
	c.user("lastUser", "another abandoned prompt")
	c.answer("last", "abandoned")
	file := c.write(t)
	path, err := selectedPath(file, "a1:last", 0)
	if err != nil {
		t.Fatal(err)
	}
	r := &Reader{}
	wantTurns(t, r, path, "u1")
	before, err := r.Before(path, "u1")
	if err != nil {
		t.Fatal(err)
	}
	page, err := r.History(path, before)
	if err != nil || len(page.Turns) != 0 || page.Before != "" {
		t.Fatalf("history at selected root: %+v %v", page, err)
	}
}

func TestSelectedLeafBeyondBackWindowAndHistory(t *testing.T) {
	c := &conv{}
	for i := 0; i < 65; i++ {
		id := fmt.Sprint(i)
		c.user("u"+id, "selected question")
		if i > 0 {
			c.ls[len(c.ls)-1] = strings.TrimSuffix(c.ls[len(c.ls)-1], "}") + `,"activeLeafId":"a0"}`
		}
		c.answer("a"+id, "selected reply")
	}
	for i := 0; i < (chat.MaxBack>>20)+2; i++ {
		id := fmt.Sprint(i)
		c.user("filler-user"+id, "abandoned question")
		c.answer("filler-answer"+id, strings.Repeat("x", 1<<20))
	}
	file := c.write(t)
	path, err := selectedPath(file, "a64:"+c.last, 0)
	if err != nil {
		t.Fatal(err)
	}
	r := &Reader{}
	turns, err := r.Turns(path)
	if err != nil || len(turns) != chat.MaxTurns {
		t.Fatalf("selected turns: %d %v", len(turns), err)
	}
	seen := map[string]bool{}
	check := func(turns []chat.Turn) {
		for _, turn := range turns {
			if !strings.HasPrefix(turn.ID, "u") || turn.Said != "selected question" || turn.Reply != "selected reply" || seen[turn.ID] {
				t.Fatalf("wrong, empty or repeated turn: %+v", turn)
			}
			seen[turn.ID] = true
		}
	}
	check(turns)
	before, err := r.Before(path, turns[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	for n := 0; before != ""; n++ {
		if n > 10 {
			t.Fatal("history did not progress")
		}
		page, err := r.History(path, before)
		if err != nil {
			t.Fatal(err)
		}
		check(page.Turns)
		before = page.Before
	}
	if len(seen) != 65 {
		t.Fatalf("recovered %d of 65 turns", len(seen))
	}
}

func TestMissingSelectedLeafDoesNotEmitPlaceholder(t *testing.T) {
	file := forked().write(t)
	path, _ := selectedPath(file, "missing:a2", 0)
	turns, err := (&Reader{}).Turns(path)
	if len(turns) != 0 {
		t.Fatalf("empty placeholder: %+v", turns)
	}
	if err != nil && !errors.Is(err, chat.ErrCursor) {
		t.Fatal(err)
	}
	if len(parseMessages(nil, "", true)) != 0 {
		t.Fatal("empty truncated window became a turn")
	}
	if len(parseMessages([]msg{{m: message{Role: "toolResult"}}}, "", true)) != 0 {
		t.Fatal("an unmatched result became an empty placeholder")
	}
}

func TestSelectedLeafBeforeOversizedPhysicalTail(t *testing.T) {
	c := forked()
	c.user("large-user", "abandoned")
	c.answer("large-answer", strings.Repeat("x", chat.MaxBack+1024))
	file := c.write(t)
	path, err := selectedPath(file, "a1:large-answer", 0)
	if err != nil {
		t.Fatal(err)
	}
	r := &Reader{}
	wantTurns(t, r, path, "u1")
	before, err := r.Before(path, "u1")
	if err != nil {
		t.Fatal(err)
	}
	page, err := r.History(path, before)
	if err != nil || len(page.Turns) != 0 || page.Before != "" {
		t.Fatalf("old branch history: %+v %v", page, err)
	}
}

func TestLeafSnapshotValidation(t *testing.T) {
	r, p, _ := pane(t, map[string]string{chat.LabelPid: "4242", chat.LabelSession: "FILE"})
	r.Setup.Install(false)
	for _, value := range []string{"../secret:a2", "a1:a2/../x", "a1", ":", "a1:a2\x00other", "a1:a2\n", strings.Repeat("x", 129) + ":a2"} {
		p.Labels[leafLabel] = value
		if _, _, err := r.FindPane(p); miss(err) != chat.NoMatch {
			t.Fatalf("accepted %q: %v", value, err)
		}
	}
}

func TestPreviousExtensionVersionsAreOld(t *testing.T) {
	for _, agent := range []string{"pi", "omp"} {
		t.Run(agent, func(t *testing.T) {
			s, file := setup(t)
			s.ID = agent
			if agent == "omp" {
				s.templates = map[int]string{version: ompCurrent, 1: ompPrevious, 0: strings.Replace(ompPrevious, "after /restart.", "after /reload or a restart.", 1)}
				file = s.File()
			}
			if err := s.Install(false); err != nil {
				t.Fatal(err)
			}
			for v, tmpl := range s.variants() {
				if v == version {
					continue
				}
				if err := os.WriteFile(file, []byte(content(tmpl, s.HC)), 0o600); err != nil {
					t.Fatal(err)
				}
				if s.State() != chat.Old {
					t.Fatalf("v%d: %s", v, s.State())
				}
				if err := s.Install(false); err != nil || s.State() != chat.Installed {
					t.Fatalf("update v%d: %v %s", v, err, s.State())
				}
			}
		})
	}
}
