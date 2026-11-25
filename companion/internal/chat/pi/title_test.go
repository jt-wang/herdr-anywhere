package pi

import (
	"herdr-anywhere/companion/internal/chat"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func titleFixture(t *testing.T, agent, data string) (*Reader, string) {
	t.Helper()
	d := t.TempDir()
	p := filepath.Join(d, "sessions", "sample", "record.jsonl")
	os.MkdirAll(filepath.Dir(p), 0700)
	os.WriteFile(p, []byte(data), 0600)
	return &Reader{Setup: &Setup{Dir: d, ID: agent}}, p
}
func TestSavedTitle(t *testing.T) {
	r, p := titleFixture(t, "pi", `{"type":"session","id":"a"}
{"type":"session_info","name":"Old"}
{"type":"session_info","name":"Renamed plot"}`)
	got, e := r.Title(p)
	if e != nil || got != "Renamed plot" {
		t.Fatalf("%q %v", got, e)
	}
	st, _ := os.Stat(p)
	selected, _ := selectedPath(p, "a:b", chat.Inode(st))
	got, e = r.Title(selected)
	if e != nil || got != "Renamed plot" {
		t.Fatal("selected record")
	}
	os.WriteFile(p, []byte(`{"type":"session","id":"a"}`+"\n"+`{"type":"message","name":"Wrong"}`), 0600)
	if got, _ := r.Title(p); got != "" {
		t.Fatal("message became title")
	}
}
func TestSavedTitleOMP(t *testing.T) {
	r, p := titleFixture(t, "omp", `{"type":"title","v":1,"title":"Saved OMP name"}
{"type":"session","id":"b","title":"Old header"}`)
	if got, _ := r.Title(p); got != "Saved OMP name" {
		t.Fatal("slot")
	}
	os.WriteFile(p, []byte(`{"type":"session","id":"b","title":"Header name"}`), 0600)
	if got, _ := r.Title(p); got != "Header name" {
		t.Fatal("legacy header")
	}
}
func TestSavedTitleBounds(t *testing.T) {
	r, p := titleFixture(t, "pi", `{"type":"session","id":"a"}`+"\n"+`{"type":"session_info","name":"`+strings.Repeat("x", 65536)+`"}`)
	if got, _ := r.Title(p); got != "" {
		t.Fatal("oversized metadata")
	}
	_, outside := titleFixture(t, "pi", `{"type":"session","id":"b"}`+"\n"+`{"type":"session_info","name":"Wrong"}`)
	os.Remove(p)
	if e := os.Symlink(outside, p); e != nil {
		t.Skip(e)
	}
	if got, _ := r.Title(p); got != "" {
		t.Fatal("escape")
	}
}

func TestSavedTitleTail(t *testing.T) {
	header := `{"type":"session","id":"a"}` + "\n"
	r, p := titleFixture(t, "pi", header+strings.Repeat("x", titleWindow+512)+"\n"+`{"type":"session_info","name":"Recent name"}`)
	if got, _ := r.Title(p); got != "Recent name" {
		t.Fatal("tail title")
	}
	os.WriteFile(p, []byte(header+`{"type":"session_info","name":"Too old"}`+"\n"+strings.Repeat("x", titleWindow+512)), 0600)
	if got, _ := r.Title(p); got != "" {
		t.Fatal("outside window")
	}
	os.WriteFile(p, []byte(`{"type":"message","name":"Wrong"}`+"\n"+`{"type":"session_info","name":"Wrong"}`), 0600)
	if got, _ := r.Title(p); got != "" {
		t.Fatal("missing session header")
	}
}

func TestSavedTitleRejectsReplacedSelection(t *testing.T) {
	r, p := titleFixture(t, "pi", `{"type":"session","id":"a"}`+"\n"+`{"type":"session_info","name":"Wrong replacement"}`)
	st, _ := os.Stat(p)
	selected, _ := selectedPath(p, "a:b", chat.Inode(st)+1)
	if got, _ := r.Title(selected); got != "" {
		t.Fatal("stale inode selection")
	}
}
