package update

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A file something still has open (Go opens files without FILE_SHARE_DELETE, so it can't be renamed meanwhile) goes
// in place once it is let go, as the new program's file does a moment after its --version ran on Windows on Arm.
func TestReplaceWaitsForTheNewFileToBeLetGo(t *testing.T) {
	dir := t.TempDir()
	file, exe := filepath.Join(dir, "new.exe"), filepath.Join(dir, "herdr-anywhere.exe")
	if err := os.WriteFile(file, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(file)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(file, filepath.Join(dir, "probe.exe")); err == nil || !held(err) {
		t.Fatalf("renaming an open file: %v; this test needs it to fail as held", err)
	}
	time.AfterFunc(500*time.Millisecond, func() { f.Close() })
	if err := replace(file, exe); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(exe); string(got) != "new" {
		t.Fatalf("exe holds %q", got)
	}
}
