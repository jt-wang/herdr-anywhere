package pi

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"herdr-anywhere/companion/internal/chat"
)

func setup(t *testing.T) (*Setup, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), ".pi", "agent")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return &Setup{Dir: dir, HC: "/opt/hc dir/hc"}, filepath.Join(dir, "extensions", FileName)
}

func TestInstallPutsOneFileAndRemoveTakesItAway(t *testing.T) {
	s, file := setup(t)
	if st := s.State(); st != chat.Absent {
		t.Fatalf("before: %s", st)
	}
	if err := s.Install(false); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Placed by Herdr Anywhere", "herdr-anywhere chat-setup remove pi", `const HC = "/opt/hc dir/hc";`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("file lacks %q", want)
		}
	}
	if strings.Contains(string(b), "{{") {
		t.Errorf("a placeholder is left in the file")
	}
	if st := s.State(); st != chat.Installed {
		t.Fatalf("after install: %s", st)
	}
	if err := s.Remove(false); err != nil {
		t.Fatal(err)
	}
	// Nothing is left: not the file, not the folder it made.
	if _, err := os.Stat(filepath.Dir(file)); !os.IsNotExist(err) {
		t.Fatalf("the extensions folder it made is still there: %v", err)
	}
	if st := s.State(); st != chat.Absent {
		t.Fatalf("after remove: %s", st)
	}
}

// A folder that was there before stays, with the user's other files.
func TestRemoveKeepsTheFolderItFound(t *testing.T) {
	s, file := setup(t)
	os.MkdirAll(filepath.Dir(file), 0o755)
	other := filepath.Join(filepath.Dir(file), "mine.ts")
	os.WriteFile(other, []byte("// mine\n"), 0o644)
	s.Install(false)
	s.Remove(false)
	if _, err := os.Stat(other); err != nil {
		t.Fatalf("the user's own file went: %v", err)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatalf("our file is still there")
	}
}

// Without Pi on the computer there is nothing to offer and nothing is made.
func TestNoPiNoFile(t *testing.T) {
	s := &Setup{Dir: filepath.Join(t.TempDir(), ".pi", "agent"), HC: "/hc"}
	if st := s.State(); st != chat.NoAgent {
		t.Fatalf("state %s", st)
	}
	if err := s.Install(false); !errors.Is(err, chat.ErrNoAgent) {
		t.Fatalf("install: %v", err)
	}
	if _, err := os.Stat(filepath.Dir(s.Dir)); !os.IsNotExist(err) {
		t.Fatalf("made %s", filepath.Dir(s.Dir))
	}
}

// A file someone changed is not overwritten or deleted unless asked again with force.
func TestAChangedFileIsLeftUntilAsked(t *testing.T) {
	s, file := setup(t)
	s.Install(false)
	os.WriteFile(file, []byte("// my own version\n"), 0o644)
	if st := s.State(); st != chat.Changed {
		t.Fatalf("state %s", st)
	}
	if err := s.Install(false); !errors.Is(err, chat.ErrChanged) {
		t.Fatalf("install over it: %v", err)
	}
	if err := s.Remove(false); !errors.Is(err, chat.ErrChanged) {
		t.Fatalf("remove it: %v", err)
	}
	if b, _ := os.ReadFile(file); string(b) != "// my own version\n" {
		t.Fatalf("the changed file was touched: %q", b)
	}
	if err := s.Install(true); err != nil || s.State() != chat.Installed {
		t.Fatalf("install with force: %v %s", err, s.State())
	}
	os.WriteFile(file, []byte("// mine again\n"), 0o644)
	if err := s.Remove(true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatalf("still there after a forced remove")
	}
}

// The file as written for another hc path is still ours, unchanged: a moved hc is an update, not a change.
func TestOursWithAnotherHCIsNotChanged(t *testing.T) {
	s, _ := setup(t)
	s.Install(false)
	moved := &Setup{Dir: s.Dir, HC: "/elsewhere/hc"}
	if st := moved.State(); st != chat.Old {
		t.Fatalf("state %s", st)
	}
	if err := moved.Install(false); err != nil || moved.State() != chat.Installed {
		t.Fatalf("update: %v %s", err, moved.State())
	}
}

// A checkout on Windows can turn the embedded file's line ends into CRLF (git's autocrlf, as on CI's Windows runners):
// the file written for another hc is still ours.
func TestOursWithCRLFLineEnds(t *testing.T) {
	was := current
	t.Cleanup(func() { current, templates = was, map[int]string{version: was} })
	current = strings.ReplaceAll(was, "\n", "\r\n")
	templates = map[int]string{version: current}
	s, _ := setup(t)
	s.Install(false)
	moved := &Setup{Dir: s.Dir, HC: "/elsewhere/hc"}
	if st := moved.State(); st != chat.Old {
		t.Fatalf("state %s", st)
	}
}

// A file an older Companion wrote is ours: installing updates it without asking.
func TestAnOlderVersionIsOursToUpdate(t *testing.T) {
	s, file := setup(t)
	templates[0] = "// an older file\nconst HC = \"{{HC}}\";\n"
	t.Cleanup(func() { delete(templates, 0) })
	os.MkdirAll(filepath.Dir(file), 0o755)
	os.WriteFile(file, []byte(content(templates[0], "/old/hc")), 0o644)
	if st := s.State(); st != chat.Old {
		t.Fatalf("state %s", st)
	}
	if err := s.Install(false); err != nil || s.State() != chat.Installed {
		t.Fatalf("update: %v %s", err, s.State())
	}
}
