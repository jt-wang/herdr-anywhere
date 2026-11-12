package testfake

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"herdr-anywhere/companion/internal/autostart"
)

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
	account, reset, loaded := FakeServiceManager(t)
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
	// The person's state folder (~/.config/herdr-anywhere): a run makes, removes or writes none of it, while the
	// person's Companion goes on writing its log and state.json.
	state := filepath.Join(account, ".config", "herdr-anywhere")
	write := func(name, text string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(state, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Run("the person's state folder made during the run", func(t *testing.T) {
		reset()
		g := GuardAutostart(own)
		os.MkdirAll(state, 0o700)
		saysAll(t, g.Check(), "the person's state folder "+state+" changed", "made during the run")
	})
	t.Run("a file written in the person's state folder", func(t *testing.T) {
		reset()
		os.MkdirAll(state, 0o700)
		write("progress.json", `{"on":false}`)
		g := GuardAutostart(own)
		write("progress.json", `{"on":true}`+"\n")
		write("track.json", "{}")
		saysAll(t, g.Check(), "the person's state folder "+state+" changed", "progress.json", "added track.json")
	})
	t.Run("the person's Companion writes its log and state", func(t *testing.T) {
		reset()
		os.MkdirAll(state, 0o700)
		write("serve.log", "a\n")
		write("state.json", "{}")
		write("progress.json", `{"on":false}`)
		g := GuardAutostart(own)
		write("serve.log", "a\nb\n")
		write("state.json.123.tmp", "{}")
		write("state.json", `{"devices":[]}`)
		saysAll(t, g.Check())
	})
}
