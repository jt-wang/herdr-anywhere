package codex

import (
	"fmt"
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
				id := fmt.Sprintf("t%d", i)
				ls = append(ls, started(id), said(id, "u"+id, "question"))
				if large {
					ls = append(ls, ran(id, "call"+id, "echo ok", strings.Repeat("x", 160<<10)))
				}
				ls = append(ls, answered(id, "a"+id, "answer"), ended(id))
			}
			p := writeLines(t, ls...)
			r := &Reader{}
			ts, err := r.Turns(p)
			if err != nil || len(ts) == 0 {
				t.Fatalf("tail: %v", err)
			}
			t.Logf("before: normal tail shows %d of 95 turns", len(ts))
			cursor, _ := r.Before(p, ts[0].ID)
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
					if seen[x.ID] || x.Reply != "answer" || x.End != "done" {
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
	files, _ := filepath.Glob("testdata/codex/*/*.jsonl")
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
