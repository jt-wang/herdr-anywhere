package claude

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const titleSID = "11111111-2222-4333-8444-555555555555"

func titleFixture(t *testing.T, lines string) (*Reader, string) {
	t.Helper()
	d := t.TempDir()
	p := filepath.Join(d, "projects", "sample-project", titleSID+".jsonl")
	if e := os.MkdirAll(filepath.Dir(p), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(p, []byte(lines), 0600); e != nil {
		t.Fatal(e)
	}
	return &Reader{Dir: d}, p
}
func aiTitle(s string) string {
	return `{"type":"ai-title","aiTitle":"` + s + `","sessionId":"` + titleSID + `"}` + "\n"
}
func customTitle(s string) string {
	return `{"type":"custom-title","customTitle":"` + s + `","sessionId":"` + titleSID + `"}` + "\n"
}
func TestSavedTitle(t *testing.T) {
	r, p := titleFixture(t, aiTitle("Build garden")+aiTitle("Repair shed"))
	got, e := r.Title(p)
	if e != nil || got != "Repair shed" {
		t.Fatalf("%q %v", got, e)
	}
	os.WriteFile(p, []byte(aiTitle("Old")+customTitle("User chosen name")+customTitle("Renamed gate")+aiTitle("Generated name")), 0600)
	got, e = r.Title(p)
	if e != nil || got != "Renamed gate" {
		t.Fatalf("custom title %q %v", got, e)
	}
}
func TestSavedTitleIgnoresOtherSessionAndContent(t *testing.T) {
	for _, line := range []string{strings.Replace(aiTitle("Wrong"), titleSID, "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee", 1), `{"type":"ai-title","aiTitle":"Wrong"}`, `{"type":"assistant","sessionId":"` + titleSID + `","aiTitle":"Wrong","message":{"content":"Do not use content"}}`, strings.Replace(aiTitle("Wrong"), `"type":`, `"isSidechain":true,"type":`, 1)} {
		r, p := titleFixture(t, line)
		if got, _ := r.Title(p); got != "" {
			t.Fatal("unrelated title")
		}
	}
}
func TestSavedTitleTail(t *testing.T) {
	r, p := titleFixture(t, aiTitle("Old")+strings.Repeat("x", (4<<20)+1024)+"\n"+aiTitle("Recent")+`{partial`)
	got, e := r.Title(p)
	if e != nil || got != "Recent" {
		t.Fatalf("large record %q %v", got, e)
	}
	os.WriteFile(p, []byte(aiTitle("Too old")+strings.Repeat("x", (4<<20)+1024)), 0600)
	if got, _ := r.Title(p); got != "" {
		t.Fatal("read beyond tail budget")
	}
}
func TestSavedTitleRejectsHelperAndEscape(t *testing.T) {
	r, p := titleFixture(t, aiTitle("Root"))
	helper := filepath.Join(filepath.Dir(p), titleSID, "subagents", "agent-helper.jsonl")
	os.MkdirAll(filepath.Dir(helper), 0700)
	os.WriteFile(helper, []byte(aiTitle("Wrong")), 0600)
	if got, _ := r.Title(helper); got != "" {
		t.Fatal("helper accepted")
	}
	_, outside := titleFixture(t, aiTitle("Wrong"))
	os.Remove(p)
	if e := os.Symlink(outside, p); e != nil {
		t.Skip(e)
	}
	if got, _ := r.Title(p); got != "" {
		t.Fatal("symlink escaped")
	}
}

func TestSavedTitleRejectsOversizedMetadata(t *testing.T) {
	r, p := titleFixture(t, aiTitle(strings.Repeat("x", titleLineLimit)))
	if got, _ := r.Title(p); got != "" {
		t.Fatal("oversized valid metadata accepted")
	}
}
