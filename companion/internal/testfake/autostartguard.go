package testfake

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"herdr-anywhere/companion/internal/autostart"
)

// AutostartGuard is what a test run found of the person's autostart entry at its start, to check at its end that the
// run left it alone. pair (with no serve running), start and stop reach the service manager, whose domain is the
// user's (launchd's gui/<uid>, systemd's user instance) whatever HOME is; app/e2e/autostart-guard.sh is the same
// check for the e2e suites.
type AutostartGuard struct {
	own, home, file, stamp string
	loaded                 []string
}

// GuardAutostart notes the person's entry (in the account's home from the user database, or $HM_AUTOSTART_HOME) and
// which herdranywhere entries are loaded. own is the name the run gives its own entries (NameEnv).
func GuardAutostart(own string) *AutostartGuard {
	home := os.Getenv("HM_AUTOSTART_HOME")
	if home == "" {
		if u, err := user.Current(); err == nil {
			home = u.HomeDir
		}
	}
	g := &AutostartGuard{own: own, home: home}
	if runtime.GOOS == "windows" || home == "" {
		return g // the Run key is no file; Windows is not checked
	}
	g.file = (&autostart.Service{Name: autostart.DefaultName, Home: home}).Path()
	g.stamp = stamp(g.file)
	g.loaded = loadedEntries()
	return g
}

// Check unloads and deletes the run's own entry, and says what the run changed: the person's entry, entries loaded or
// unloaded (other runs' io.example.herdranywhere.test. entries aside), or its own entry written in the account's home.
func (g *AutostartGuard) Check() []string {
	if g.file == "" {
		return nil
	}
	var bad []string
	mine := &autostart.Service{Name: g.own, Home: g.home}
	if _, err := os.Stat(mine.Path()); err == nil {
		bad = append(bad, fmt.Sprintf("the run's own entry was written in the account's home (%s, now deleted): pair, start or stop ran with the real HOME", mine.Path()))
	}
	mine.Remove()
	if home, err := os.UserHomeDir(); err == nil && home != g.home {
		(&autostart.Service{Name: g.own, Home: home}).Remove()
	}
	if now := stamp(g.file); now != g.stamp {
		bad = append(bad, fmt.Sprintf("the person's entry %s changed: %s before, %s after", g.file, g.stamp, now))
	}
	after := loadedEntries()
	var added, gone []string
	for _, l := range after {
		if !slices.Contains(g.loaded, l) {
			added = append(added, l)
		}
	}
	for _, l := range g.loaded {
		if !slices.Contains(after, l) {
			gone = append(gone, l)
		}
	}
	if len(added) > 0 {
		bad = append(bad, "loaded during the run: "+strings.Join(added, " "))
	}
	if len(gone) > 0 {
		bad = append(bad, "unloaded during the run: "+strings.Join(gone, " "))
	}
	return bad
}

// stamp is a file's time and size, or "none".
func stamp(path string) string {
	fi, err := os.Stat(path)
	if err != nil {
		return "none"
	}
	return fmt.Sprintf("%d %d", fi.ModTime().UnixNano(), fi.Size())
}

// loadedEntries are the herdranywhere entries the service manager has loaded, other runs' test entries aside.
func loadedEntries() []string {
	var out []byte
	col := 0
	switch runtime.GOOS {
	case "darwin":
		out, _ = exec.Command("launchctl", "list").Output()
		col = 2
	case "linux":
		out, _ = exec.Command("systemctl", "--user", "list-units", "--all", "--no-legend", "--plain").Output()
	}
	var names []string
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) <= col {
			continue
		}
		n := strings.TrimSuffix(f[col], ".service")
		if (strings.Contains(n, "herdranywhere") || n == "herdr-anywhere") && !strings.HasPrefix(n, "io.example.herdranywhere.test.") {
			names = append(names, n)
		}
	}
	slices.Sort(names)
	return slices.Compact(names)
}

// FakeServiceManager puts launchctl and systemctl stand-ins (fakeservice) first on PATH and the person's home in a folder
// of the test's, so nothing here reaches the service manager or home of whoever runs it. It returns the account's
// home, a func that empties what the stand-ins have loaded, and one that lists it.
func FakeServiceManager(t *testing.T) (string, func(), func() []string) {
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
