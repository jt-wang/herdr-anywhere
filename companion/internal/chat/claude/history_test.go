package claude

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"herdr-anywhere/companion/internal/chat"
)

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
