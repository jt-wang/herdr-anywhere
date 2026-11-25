package opencode

import (
	"database/sql"
	"strings"
	"testing"
)

func TestSavedTitle(t *testing.T) {
	p := db(t, "turns")
	id, want := session(t, "turns")
	r := &Reader{DB: p}
	got, e := r.Title(p + "#" + id)
	if e != nil || got != want {
		t.Fatalf("%q %v", got, e)
	}
	if got, _ := r.Title(p + "#missing"); got != "" {
		t.Fatal("wrong id")
	}
	d, _ := sql.Open("sqlite", p)
	defer d.Close()
	d.Exec("UPDATE session_v2 SET title='Renamed plot' WHERE id=?", id)
	if got, _ := r.Title(p + "#" + id); got != "Renamed plot" {
		t.Fatal("rename")
	}
	d.Exec("UPDATE session_v2 SET parent_id='parent' WHERE id=?", id)
	if got, _ := r.Title(p + "#" + id); got != "" {
		t.Fatal("helper title")
	}
}

func TestSavedTitleOtherDatabaseAndLength(t *testing.T) {
	p := db(t, "turns")
	id, _ := session(t, "turns")
	other := db(t, "turns")
	r := &Reader{DB: p}
	if got, _ := r.Title(other + "#" + id); got != "" {
		t.Fatal("other database")
	}
	d, _ := sql.Open("sqlite", p)
	defer d.Close()
	d.Exec("UPDATE session_v2 SET title=? WHERE id=?", strings.Repeat("x", 3000), id)
	if got, _ := r.Title(p + "#" + id); len(got) != 1024 {
		t.Fatal("unbounded title")
	}
}

func TestSavedTitleVersionOne(t *testing.T) {
	p, _ := db1(t, "turns")
	f := load1(t, "turns")
	r := &Reader{DB: p}
	got, e := r.Title(p + "#" + f.Sessions[0].ID)
	if e != nil || got != f.Sessions[0].Title || got == "" {
		t.Fatalf("old schema title_present=%t err=%v", got != "", e)
	}
}
