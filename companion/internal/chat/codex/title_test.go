package codex

import (
	"errors"
	"herdr-anywhere/companion/internal/chat"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func titleRecord(t *testing.T, d, id string) string {
	t.Helper()
	p := filepath.Join(d, "sessions", "2026", "01", "02", "rollout-"+id+".jsonl")
	os.MkdirAll(filepath.Dir(p), 0700)
	os.WriteFile(p, []byte(`{"type":"session_meta","payload":{"id":"`+id+`","source":"cli"}}`+"\n"), 0600)
	actual, e := filepath.EvalSymlinks(p)
	if e != nil {
		t.Fatal(e)
	}
	return actual
}
func TestSavedTitle(t *testing.T) {
	d := t.TempDir()
	p := titleRecord(t, d, "thread-a")
	r := &Reader{Dir: d}
	os.WriteFile(filepath.Join(d, "session_index.jsonl"), []byte(`{"id":"thread-a","thread_name":"Old"}
{"id":"thread-b","thread_name":"Wrong"}
{"id":"thread-a","thread_name":"Repair gate"}
{partial`), 0600)
	got, e := r.Title(p)
	if e != nil || got != "Repair gate" {
		t.Fatalf("%q %v", got, e)
	}
	got, e = r.Title(titleRecord(t, d, "thread-c"))
	if e != nil || got != "" {
		t.Fatalf("wrong session %q %v", got, e)
	}
}
func TestSavedTitleRejectsMetaAndEscape(t *testing.T) {
	d := t.TempDir()
	p := titleRecord(t, d, "thread-a")
	os.WriteFile(filepath.Join(d, "session_index.jsonl"), []byte(`{"id":"thread-a","thread_name":"Wrong"}`), 0600)
	for _, m := range []string{`{"type":"event_msg","payload":{"id":"thread-a"}}`, `{"type":"session_meta","payload":{}}`, `bad`} {
		os.WriteFile(p, []byte(m), 0600)
		if got, _ := (&Reader{Dir: d}).Title(p); got != "" {
			t.Fatal("invalid meta")
		}
	}
	outside := titleRecord(t, t.TempDir(), "thread-a")
	os.Remove(p)
	os.Symlink(outside, p)
	if got, _ := (&Reader{Dir: d}).Title(p); got != "" {
		t.Fatal("symlink escaped")
	}
}

func TestSavedTitleIndexLimits(t *testing.T) {
	d := t.TempDir()
	p := titleRecord(t, d, "thread-a")
	index := filepath.Join(d, "session_index.jsonl")
	r := &Reader{Dir: d}
	for _, data := range []string{strings.Repeat("x", titleLineLimit+1), strings.Repeat("x", titleIndexLimit+1)} {
		if e := os.WriteFile(index, []byte(data), 0600); e != nil {
			t.Fatal(e)
		}
		if got, _ := r.Title(p); got != "" {
			t.Fatal("oversize title")
		}
	}
	outside := filepath.Join(t.TempDir(), "index.jsonl")
	os.WriteFile(outside, []byte(`{"id":"thread-a","thread_name":"Outside"}`), 0600)
	os.Remove(index)
	if e := os.Symlink(outside, index); e != nil {
		t.Skip(e)
	}
	if got, _ := r.Title(p); got != "" {
		t.Fatal("index escaped")
	}
}
func TestSavedTitleAmbiguousRollouts(t *testing.T) {
	d := t.TempDir()
	a := titleRecord(t, d, "thread-a")
	b := titleRecord(t, d, "thread-b")
	r := &Reader{Dir: d, openFiles: func([]int) ([]string, error) { return []string{a, b}, nil }}
	_, _, e := r.Find(1)
	var miss *chat.Miss
	if !errors.As(e, &miss) || miss.Reason != chat.Ambiguous {
		t.Fatalf("multiple roots accepted: %v", e)
	}
}
