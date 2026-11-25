package antigravity

import (
	"database/sql"
	_ "modernc.org/sqlite"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSavedTitle(t *testing.T) {
	root := t.TempDir()
	id := "11111111-2222-4333-8444-555555555555"
	p := filepath.Join(root, "brain", id, ".system_generated", "logs", "transcript.jsonl")
	os.MkdirAll(filepath.Dir(p), 0700)
	os.WriteFile(p, []byte(""), 0600)
	db := filepath.Join(root, "conversation_summaries.db")
	d, e := sql.Open("sqlite", db)
	if e != nil {
		t.Fatal(e)
	}
	defer d.Close()
	d.Exec("CREATE TABLE conversation_summaries(conversation_id TEXT PRIMARY KEY,title TEXT,parent_conversation_id TEXT)")
	d.Exec("INSERT INTO conversation_summaries VALUES(?, 'Repair fence',NULL)", id)
	r := &Reader{Dir: root}
	got, e := r.Title(p)
	if e != nil || got != "Repair fence" {
		t.Fatalf("%q %v", got, e)
	}
	d.Exec("UPDATE conversation_summaries SET title='New name'")
	if got, _ := r.Title(p); got != "New name" {
		t.Fatal("rename")
	}

	other := strings.Replace(p, id, "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee", 1)
	os.MkdirAll(filepath.Dir(other), 0700)
	os.WriteFile(other, []byte(""), 0600)
	if got, _ := r.Title(other); got != "" {
		t.Fatal("wrong conversation")
	}
	if _, e := d.Exec("PRAGMA journal_mode=WAL"); e != nil {
		t.Fatal(e)
	}
	d.Exec("UPDATE conversation_summaries SET title='WAL title'")
	if got, _ := r.Title(p); got != "WAL title" {
		t.Fatal("missing WAL title")
	}
	d.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
	if _, e := d.Exec("PRAGMA journal_mode=DELETE"); e != nil {
		t.Fatal(e)
	}
	d.Exec("UPDATE conversation_summaries SET parent_conversation_id='parent'")
	if got, _ := r.Title(p); got != "" {
		t.Fatal("helper")
	}
	if _, e := os.Stat(db + "-wal"); !os.IsNotExist(e) {
		t.Fatal("created WAL")
	}
}
