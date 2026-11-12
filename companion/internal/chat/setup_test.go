package chat

import (
	"path/filepath"
	"testing"
)

type pathSetup struct{ file string }

func (s pathSetup) Agent() string      { return "pi" }
func (s pathSetup) Name() string       { return "Pi" }
func (s pathSetup) File() string       { return s.file }
func (s pathSetup) Reload() string     { return "/reload" }
func (s pathSetup) Version() int       { return 1 }
func (s pathSetup) State() SetupState  { return Absent }
func (s pathSetup) Install(bool) error { return nil }
func (s pathSetup) Remove(bool) error  { return nil }

// The phone shows the file under the home folder as ~/…: shorter, and no user name on a screenshot.
func TestInfoShowsHomeAsTilde(t *testing.T) {
	home := filepath.Join(t.TempDir(), "alex")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	sep := string(filepath.Separator)
	for _, c := range []struct{ file, want string }{
		{filepath.Join(home, ".pi", "agent", "extensions", "herdr-anywhere.ts"), "~" + sep + filepath.Join(".pi", "agent", "extensions", "herdr-anywhere.ts")},
		{home + "x" + sep + "f.ts", home + "x" + sep + "f.ts"}, // a folder that only starts like home
		{filepath.Join(sep+"opt", "pi", "f.ts"), filepath.Join(sep+"opt", "pi", "f.ts")},
	} {
		if got := Info(pathSetup{c.file}).File; got != c.want {
			t.Errorf("Info(%q).File = %q, want %q", c.file, got, c.want)
		}
	}
}
