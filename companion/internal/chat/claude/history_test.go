package claude

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"herdr-anywhere/companion/internal/chat"
	"herdr-anywhere/companion/internal/testfake"
)

type historyPane struct {
	Reader
	path string
}

func (r *historyPane) Find(int) (string, string, error) { return r.path, "2.1.325", nil }

func TestHistoryButtonBoundaries(t *testing.T) {
	hc := testfake.HC(t, t.TempDir(), "hc", testfake.Spec{On: map[string][]testfake.Reply{
		"get": {{Print: `{"status":{"app":"claude-code","state":"working"},"foreground":{"pid":4242}}`}},
	}})
	for _, n := range []int{1, 4, 30, 31, 60, 61} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			var ls []string
			for i := 0; i < n; i++ {
				ls = append(ls, prompt(fmt.Sprintf("u%d", i), fmt.Sprintf("Inspect cedar row %d", i)))
				if i < n-1 {
					ls = append(ls, say(fmt.Sprintf("a%d", i), fmt.Sprintf("m%d", i), "Ready", "end_turn"))
				} else {
					for j := 0; j < 5; j++ {
						ls = append(ls, use(fmt.Sprintf("c%d", j), fmt.Sprintf("m%d", j), fmt.Sprintf("call%d", j), "Bash", `{"command":"echo cedar"}`))
					}
				}
			}
			r := &historyPane{path: write(t, ls...)}
			s := &chat.Service{HC: hc, Readers: map[string]chat.Reader{"claude-code": r}}
			a, err := s.ChatPage("t1", "")
			if err != nil || len(a.Turns) != min(n, 30) || (a.Before != "") != (n > 30) {
				t.Fatalf("initial: turns=%d before=%q err=%v", len(a.Turns), a.Before, err)
			}
			last := a.Turns[len(a.Turns)-1]
			if last.End != "open" || len(last.Steps) != 5 {
				t.Fatalf("working turn: %+v", last)
			}
			seen := len(a.Turns)
			for a.Before != "" {
				a, err = s.ChatPage("t1", a.Before)
				if err != nil || len(a.Turns) == 0 {
					t.Fatalf("empty offered page: %+v %v", a, err)
				}
				seen += len(a.Turns)
			}
			if seen != n {
				t.Fatalf("recovered %d of %d", seen, n)
			}
		})
	}
	t.Run("oversize earlier record preserves latest work", func(t *testing.T) {
		r := &historyPane{path: write(t, longTurn(chat.MaxBack>>20+2)...)}
		s := &chat.Service{HC: hc, Readers: map[string]chat.Reader{"claude-code": r}}
		a, err := s.ChatPage("t1", "")
		if err != nil || len(a.Turns) != 1 || a.Turns[0].End != "open" || len(a.Turns[0].Steps) == 0 {
			t.Fatalf("latest working turn lost to history probe: %+v %v", a, err)
		}
		if a.Before != "" || a.HistoryError != "history-limit" {
			t.Fatalf("unreadable older history must explain its limit, not offer an empty page: %+v", a)
		}
	})
}

func TestHistoryLongSession(t *testing.T) {
	for _, large := range []bool{false, true} {
		t.Run(fmt.Sprint(large), func(t *testing.T) {
			var ls []string
			for i := 0; i < 95; i++ {
				ls = append(ls, prompt(fmt.Sprintf("u%d", i), fmt.Sprintf("question %d", i)))
				if large {
					ls = append(ls, use(fmt.Sprintf("c%d", i), fmt.Sprintf("m%d", i), fmt.Sprintf("call%d", i), "Bash", `{"command":"echo ok"}`), result(fmt.Sprintf("r%d", i), fmt.Sprintf("call%d", i), strings.Repeat("x", 160<<10)))
				}
				ls = append(ls, say(fmt.Sprintf("a%d", i), fmt.Sprintf("reply%d", i), "answer", "end_turn"))
			}
			p := write(t, ls...)
			r := &Reader{}
			ts, err := r.Turns(p)
			if err != nil || len(ts) == 0 || len(ts) > chat.MaxTurns {
				t.Fatalf("tail: %d %v", len(ts), err)
			}
			t.Logf("before: normal tail shows %d of 95 turns", len(ts))
			cursor, err := r.Before(p, ts[0].ID)
			if err != nil {
				t.Fatal(err)
			}
			// Appending while browsing must not move the frozen page or repeat the current tail.
			f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0)
			fmt.Fprintln(f, prompt("new", "new prompt"))
			f.Close()
			seen := map[string]bool{}
			for _, x := range ts {
				seen[x.ID] = true
			}
			for n := 0; cursor != ""; n++ {
				if n > 100 {
					t.Fatal("non-progressing cursor")
				}
				page, err := r.History(p, cursor)
				if err != nil {
					t.Fatal(err)
				}
				if len(page.Turns) > chat.MaxTurns {
					t.Fatal("unbounded page")
				}
				for _, x := range page.Turns {
					if seen[x.ID] || x.ID == "new" || x.Reply != "answer" {
						t.Fatalf("overlap/partial: %+v", x)
					}
					seen[x.ID] = true
				}
				cursor = page.Before
			}
			if len(seen) != 95 {
				t.Fatalf("recovered %d of 95", len(seen))
			}
			t.Logf("after: recovered all %d turns", len(seen))
		})
	}
}

func TestHistoryRecordedFixtures(t *testing.T) {
	files, _ := filepath.Glob("testdata/claude-code/*/*.jsonl")
	if len(files) == 0 {
		t.Fatal("no fixtures")
	}
	for _, p := range files {
		t.Run(p, func(t *testing.T) {
			r := &Reader{}
			want, err := r.Turns(p)
			if err != nil {
				t.Fatal(err)
			}
			got, err := r.History(p, "")
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(norm(got.Turns), norm(want)) {
				t.Fatalf("recorded fixture differs: got %+v want %+v", got.Turns, want)
			}
		})
	}
}
