package pi

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
			c := &conv{}
			for i := 0; i < 95; i++ {
				id := fmt.Sprint(i)
				c.user("u"+id, "question")
				if large {
					c.call("c"+id, "call"+id, "echo ok")
					c.result("r"+id, "call"+id, strings.Repeat("x", 160<<10))
				}
				c.answer("a"+id, "answer")
			}
			p := c.write(t)
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
	files, _ := filepath.Glob("testdata/pi/*/*.jsonl")
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

func TestHistoryFollowsParentsAcrossAbandonedBranch(t *testing.T) {
	c := &conv{}
	c.user("root", "shared question")
	c.answer("root-answer", "answer")
	for i := 0; i < 35; i++ {
		id := fmt.Sprint(i)
		c.user("abandoned"+id, "not on this branch")
		c.answer("abandoned-answer"+id, strings.Repeat("x", 160<<10))
	}
	c.last = "root-answer"
	for i := 0; i < 35; i++ {
		id := fmt.Sprint(i)
		c.user("active"+id, "active question")
		c.call("c"+id, "call"+id, "echo ok")
		c.result("r"+id, "call"+id, strings.Repeat("x", 160<<10))
		c.answer("a"+id, "answer")
	}
	p := c.write(t)
	r := &Reader{}
	ts, err := r.Turns(p)
	if err != nil {
		t.Fatal(err)
	}
	cursor, _ := r.Before(p, ts[0].ID)
	seen := map[string]bool{}
	for _, x := range ts {
		seen[x.ID] = true
	}
	for n := 0; cursor != ""; n++ {
		if n > 30 {
			t.Fatal("non-progressing branch cursor")
		}
		page, err := r.History(p, cursor)
		if err != nil {
			t.Fatal(err)
		}
		for _, x := range page.Turns {
			if strings.HasPrefix(x.ID, "abandoned") || seen[x.ID] {
				t.Fatalf("wrong branch or overlap: %s", x.ID)
			}
			seen[x.ID] = true
		}
		cursor = page.Before
	}
	if len(seen) != 36 || !seen["root"] {
		t.Fatalf("active ancestry: %d turns, root=%v", len(seen), seen["root"])
	}
}
