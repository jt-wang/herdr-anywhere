package opencode

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"herdr-anywhere/companion/internal/chat"
)

func TestHistoryLongSession(t *testing.T) {
	for _, v := range []int{1, 2} {
		t.Run(fmt.Sprint(v), func(t *testing.T) {
			var p string
			var d *sql.DB
			if v == 1 {
				p, d = db1(t)
				_, err := d.Exec("INSERT INTO session (id, project_id, slug, directory, title, version, time_created, time_updated) VALUES ('history', 'p', 's', '/work/sample', 'history', '1.19.0', 1, 1)")
				if err != nil {
					t.Fatal(err)
				}
			} else {
				p = db(t)
				d, _ = sql.Open("sqlite", p)
				defer d.Close()
				add(t, d, "history", "/work/sample", "history", "2.2.0", "")
			}
			for i := 0; i < 95; i++ {
				if v == 2 {
					insert(t, d, "history", message{ID: fmt.Sprintf("u%03d", i), Type: "user", Seq: int64(i * 4), Created: int64(i * 4), Data: json.RawMessage(`{"text":"question"}`)})
					insert(t, d, "history", message{ID: fmt.Sprintf("q%03d", i), Type: "user", Seq: int64(i*4 + 1), Created: int64(i*4 + 1), Data: json.RawMessage(`{"text":"queued"}`)})
					insert(t, d, "history", message{ID: fmt.Sprintf("a%03d", i), Type: "assistant", Seq: int64(i*4 + 2), Created: int64(i*4 + 2), Data: json.RawMessage(`{"content":[{"type":"text","text":"answer"}]}`)})
					insert(t, d, "history", message{ID: fmt.Sprintf("z%03d", i), Type: "idle", Seq: int64(i*4 + 3), Created: int64(i*4 + 3), Data: json.RawMessage(`{"outcome":"succeeded"}`)})
				} else {
					for j, role := range []string{"user", "user", "assistant"} {
						id := fmt.Sprintf("m%03d_%d", i, j)
						data := fmt.Sprintf(`{"role":%q,"finish":"stop","time":{"created":%d,"completed":%d}}`, role, i*4+j, i*4+j+1)
						if _, err := d.Exec("INSERT INTO message (id,session_id,time_created,time_updated,data) VALUES (?,'history',?,?,?)", id, i*4+j, i*4+j, data); err != nil {
							t.Fatal(err)
						}
						text := "question"
						if j == 1 {
							text = "queued"
						}
						if j == 2 {
							text = "answer"
						}
						if _, err := d.Exec("INSERT INTO part (id,message_id,session_id,time_created,time_updated,data) VALUES (?,?,'history',?,?,?)", "p"+id, id, i*4+j, i*4+j, fmt.Sprintf(`{"type":"text","text":%q}`, text)); err != nil {
							t.Fatal(err)
						}
					}
				}
			}
			r := &Reader{}
			path := p + "#history"
			ts, err := r.Turns(path)
			if err != nil {
				t.Fatal(err)
			}
			if len(ts) != 30 {
				t.Fatalf("tail %d", len(ts))
			}
			t.Log("before: normal tail shows 30 of 95 turns")
			cursor, _ := r.Before(path, ts[0].ID)
			seen := map[string]bool{}
			for _, x := range ts {
				seen[x.ID] = true
			}
			for n := 0; cursor != ""; n++ {
				if n > 10 {
					t.Fatal("non-progressing cursor")
				}
				page, err := r.History(path, cursor)
				if err != nil {
					t.Fatal(err)
				}
				if len(page.Turns) > chat.MaxTurns {
					t.Fatal("unbounded page")
				}
				for _, x := range page.Turns {
					if seen[x.ID] || x.Reply != "answer" || len(x.Steps) != 1 || x.Steps[0].Kind != chat.StepYou {
						t.Fatalf("overlap/partial/queued turn split: %+v", x)
					}
					seen[x.ID] = true
				}
				cursor = page.Before
			}
			if len(seen) != 95 {
				t.Fatalf("recovered %d of 95", len(seen))
			}
			t.Log("after: recovered all 95 turns, queued messages stay within their turn")
		})
	}
}

func TestHistoryRecordedFixtures(t *testing.T) {
	for _, v := range []int{1, 2} {
		names := []string{"turns", "steps", "files"}
		if v == 2 {
			names = append(names, "ask")
		}
		for _, name := range names {
			t.Run(fmt.Sprintf("%d/%s", v, name), func(t *testing.T) {
				var p, id string
				if v == 1 {
					p, _ = db1(t, name)
					id = load1(t, name).Sessions[0].ID
				} else {
					p = db(t, name)
					id, _ = session(t, name)
				}
				r := &Reader{}
				path := p + "#" + id
				ms, err := rows(path)
				if err != nil {
					t.Fatal(err)
				}
				want := parse(ms)
				got, err := r.History(path, "")
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(got.Turns, want) {
					t.Fatalf("recorded fixture differs: got %+v want %+v", got.Turns, want)
				}
			})
		}
	}
}

func TestHistoryReadBudget(t *testing.T) {
	p := db(t)
	d, err := sql.Open("sqlite", p)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	add(t, d, "large", "/work/sample", "large", "2.2.0", "")
	tx, err := d.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4100; i++ {
		kind, data := "assistant", `{"content":[]}`
		if i == 0 {
			kind, data = "user", `{"text":"question"}`
		}
		if _, err = tx.Exec("INSERT INTO session_message (id,session_id,type,seq,time_created,time_updated,data) VALUES (?,'large',?,?,1,1,?)", fmt.Sprint(i), kind, i, data); err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	r := &Reader{}
	if _, err = r.History(p+"#large", ""); !errors.Is(err, chat.ErrHistoryLimit) {
		t.Fatalf("history read budget: %v", err)
	}
	if _, err = r.Turns(p + "#large"); !errors.Is(err, chat.ErrHistoryLimit) {
		t.Fatalf("latest read budget: %v", err)
	}
}
