package testfake

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"herdr-anywhere/companion/internal/autostart"
)

// fakeManager puts launchctl and systemctl stand-ins (fakeservice) first on PATH and the person's home in a folder
// of the test's, so nothing here reaches the service manager or home of whoever runs it. It returns the account's
// home, a func that empties what the stand-ins have loaded, and one that lists it.
func fakeManager(t *testing.T) (string, func(), func() []string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the Run key has no launchctl or systemctl to stand in for")
	}
	bin := t.TempDir()
	exe := filepath.Join(bin, "launchctl")
	if b, err := exec.Command("go", "build", "-o", exe, "herdr-anywhere/companion/internal/testfake/fakeservice").CombinedOutput(); err != nil {
		t.Fatalf("building fakeservice: %v\n%s", err, b)
	}
	b, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "systemctl"), b, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	account := t.TempDir()
	t.Setenv("HM_AUTOSTART_HOME", account)
	t.Setenv("XDG_CONFIG_HOME", "")
	reset := func() {
		os.RemoveAll(filepath.Join(bin, "launchctl.d"))
		os.RemoveAll(filepath.Join(bin, "systemctl.d"))
		os.RemoveAll(filepath.Join(account, "Library"))
		os.RemoveAll(filepath.Join(account, ".config"))
	}
	loaded := func() []string {
		var names []string
		for _, d := range []string{"launchctl.d", "systemctl.d"} {
			entries, _ := os.ReadDir(filepath.Join(bin, d))
			for _, e := range entries {
				names = append(names, e.Name())
			}
		}
		return names
	}
	return account, reset, loaded
}

func installStart(t *testing.T, name, home string) {
	t.Helper()
	s := &autostart.Service{Name: name, Home: home}
	if _, err := s.Install(autostart.Entry{Exe: "/bin/true"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
}

func saysAll(t *testing.T, bad []string, words ...string) {
	t.Helper()
	all := strings.Join(bad, "\n")
	if len(words) == 0 && len(bad) > 0 {
		t.Fatalf("the guard found something: %s", all)
	}
	for _, w := range words {
		if !strings.Contains(all, w) {
			t.Fatalf("the guard said %q, not %q", all, w)
		}
	}
}

func TestGuardAutostart(t *testing.T) {
	account, reset, loaded := fakeManager(t)
	own := "io.example.herdranywhere.test.guard"
	def := autostart.DefaultName

	t.Run("the default name in the account's home", func(t *testing.T) {
		reset()
		g := GuardAutostart(own)
		installStart(t, def, account)
		saysAll(t, g.Check(), "the person's entry", "loaded during the run: "+def)
	})
	t.Run("the default name in a home of its own", func(t *testing.T) {
		reset()
		g := GuardAutostart(own)
		installStart(t, def, t.TempDir())
		saysAll(t, g.Check(), "loaded during the run: "+def)
	})
	t.Run("its own name in the account's home", func(t *testing.T) {
		reset()
		g := GuardAutostart(own)
		installStart(t, own, account)
		saysAll(t, g.Check(), "written in the account's home")
		if _, err := os.Stat((&autostart.Service{Name: own, Home: account}).Path()); err == nil {
			t.Fatal("the guard left the run's own entry in the account's home")
		}
	})
	t.Run("its own name in a home of its own", func(t *testing.T) {
		reset()
		g := GuardAutostart(own)
		installStart(t, own, t.TempDir())
		saysAll(t, g.Check())
		if l := loaded(); len(l) > 0 {
			t.Fatalf("the run's own entry is still loaded after the check: %v", l)
		}
	})
	t.Run("the person's Companion loaded, then stopped under the default name", func(t *testing.T) {
		reset()
		installStart(t, def, t.TempDir())
		g := GuardAutostart(own)
		home := t.TempDir()
		installStart(t, def, home) // a stop on Linux only removes a unit it finds
		(&autostart.Service{Name: def, Home: home}).Remove()
		saysAll(t, g.Check(), "unloaded during the run: "+def)
	})
	t.Run("another run's test entry comes and goes", func(t *testing.T) {
		reset()
		g := GuardAutostart(own)
		installStart(t, "io.example.herdranywhere.test.someone-else", t.TempDir())
		saysAll(t, g.Check())
	})
}
