package autostart

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
)

var entry = Entry{
	Exe:  "/opt/a b/.local/bin/herdr-anywhere",
	Args: []string{"serve", "--tailcat", "--listen", "127.0.0.1:0", "--state", "/opt/a b/.config/herdr-anywhere"},
	Env:  map[string]string{"PATH": "/opt/homebrew/bin:/usr/bin:/bin"},
	Log:  "/opt/a b/.config/herdr-anywhere/serve.log",
}

// plistValue decodes the next plist value from d: a string, a bool, an []any or a map[string]any.
func plistValue(t *testing.T, d *xml.Decoder, start xml.StartElement) any {
	t.Helper()
	switch start.Name.Local {
	case "true", "false":
		d.Skip()
		return start.Name.Local == "true"
	case "string", "key", "integer":
		var s string
		if err := d.DecodeElement(&s, &start); err != nil {
			t.Fatal(err)
		}
		return s
	case "array", "dict":
		var items []any
		for {
			tok, err := d.Token()
			if err != nil {
				t.Fatal(err)
			}
			if _, end := tok.(xml.EndElement); end {
				break
			}
			if se, ok := tok.(xml.StartElement); ok {
				items = append(items, plistValue(t, d, se))
			}
		}
		if start.Name.Local == "array" {
			return items
		}
		m := map[string]any{}
		for i := 0; i+1 < len(items); i += 2 {
			m[items[i].(string)] = items[i+1]
		}
		return m
	}
	t.Fatalf("unknown plist element %s", start.Name.Local)
	return nil
}

// plistDoc reads the plist back as launchd would.
func plistDoc(t *testing.T, b []byte) map[string]any {
	t.Helper()
	d := xml.NewDecoder(strings.NewReader(string(b)))
	d.Strict = true
	for {
		tok, err := d.Token()
		if err != nil {
			t.Fatalf("no dict: %v\n%s", err, b)
		}
		if se, ok := tok.(xml.StartElement); ok && se.Name.Local == "dict" {
			return plistValue(t, d, se).(map[string]any)
		}
	}
}

func TestLaunchdPlistKeepsServeRunning(t *testing.T) {
	got := plistDoc(t, launchdPlist("io.example.test", entry))
	args := []any{}
	for _, a := range append([]string{entry.Exe}, entry.Args...) {
		args = append(args, a)
	}
	want := map[string]any{
		"Label":            "io.example.test",
		"ProgramArguments": args,
		"RunAtLoad":        true,
		// Started again when it crashes or a start fails (no network yet at login); left alone when it was stopped.
		"KeepAlive": map[string]any{"SuccessfulExit": false},
		// launchd would otherwise kill what serve started in its process group when serve ends.
		"AbandonProcessGroup":  true,
		"EnvironmentVariables": map[string]any{"PATH": "/opt/homebrew/bin:/usr/bin:/bin"},
		"StandardOutPath":      entry.Log,
		"StandardErrorPath":    entry.Log,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %#v\nwant %#v", got, want)
	}
}

func TestLaunchdPlistEscapes(t *testing.T) {
	e := entry
	e.Args = []string{"serve", "--state", "/tmp/a&b<c>"}
	b := launchdPlist("x", e)
	if !strings.Contains(string(b), "/tmp/a&amp;b&lt;c&gt;") {
		t.Fatalf("not escaped:\n%s", b)
	}
	if got := plistDoc(t, b)["ProgramArguments"].([]any); got[3] != "/tmp/a&b<c>" {
		t.Fatalf("read back as %q", got[3])
	}
}

func TestSystemdUnit(t *testing.T) {
	e := entry
	e.Args = append(slices.Clone(e.Args), `50%"$x\`)
	u := systemdUnit(e)
	for _, want := range []string{
		"[Service]\n",
		`ExecStart="/opt/a b/.local/bin/herdr-anywhere" "serve" "--tailcat"`,
		`"50%%\"$$x\\"` + "\n",
		`Environment="PATH=/opt/homebrew/bin:/usr/bin:/bin"`,
		"Restart=on-failure\n",
		// stopping the service must not end the agents' terminals serve may have started
		"KillMode=process\n",
		"WantedBy=default.target\n",
	} {
		if !strings.Contains(u, want) {
			t.Errorf("want %q in:\n%s", want, u)
		}
	}
}

func TestWindowsCommandLine(t *testing.T) {
	e := Entry{Exe: `C:\a b\herdr-anywhere.exe`, Args: []string{"serve", "--state", `C:\a b\state\`, `say "hi"`, ""}}
	got := windowsCommandLine(e)
	want := `"C:\a b\herdr-anywhere.exe" serve --state "C:\a b\state\\" "say \"hi\"" ""`
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

// fakeRun records the manager's commands and answers them.
type fakeRun struct {
	calls [][]string
	fail  map[string]error
}

func (f *fakeRun) run(name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, append([]string{name}, args...))
	return nil, f.fail[name+" "+strings.Join(args, " ")]
}

func (f *fakeRun) ran(prefix string) bool {
	for _, c := range f.calls {
		if strings.HasPrefix(strings.Join(c, " "), prefix) {
			return true
		}
	}
	return false
}

func TestInstallWritesOnceAndRemoveTakesItAway(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("the entry is a file on macOS and Linux; Windows's is in the registry (CI)")
	}
	home := t.TempDir()
	// GitHub's Linux runners set XDG_CONFIG_HOME to the runner's own ~/.config, which the unit would follow out of home.
	t.Setenv("XDG_CONFIG_HOME", "")
	f := &fakeRun{}
	s := &Service{Name: "io.example.test", Home: home, run: f.run}
	changed, err := s.Install(entry)
	if err != nil || !changed {
		t.Fatalf("install: %v %v", changed, err)
	}
	if _, err := os.Stat(s.Path()); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(s.Path(), home) {
		t.Fatalf("%s is outside the home it was given", s.Path())
	}
	if changed, err := s.Install(entry); err != nil || changed {
		t.Fatalf("installing the same again: %v %v", changed, err)
	}
	e := entry
	e.Exe = "/elsewhere/herdr-anywhere"
	if changed, err := s.Install(e); err != nil || !changed {
		t.Fatalf("a new path must replace the old: %v %v", changed, err)
	}
	b, _ := os.ReadFile(s.Path())
	if !strings.Contains(string(b), "/elsewhere/herdr-anywhere") {
		t.Fatalf("not replaced:\n%s", b)
	}
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	removed, err := s.Remove()
	if err != nil || !removed {
		t.Fatalf("remove: %v %v", removed, err)
	}
	if _, err := os.Stat(s.Path()); !os.IsNotExist(err) {
		t.Fatalf("still there: %v", err)
	}
	if removed, err := s.Remove(); err != nil || removed {
		t.Fatalf("removing nothing: %v %v", removed, err)
	}
	switch runtime.GOOS {
	case "darwin":
		uid := os.Getuid()
		if !f.ran("launchctl bootstrap gui/"+strconv.Itoa(uid)+" "+s.Path()) || !f.ran("launchctl bootout gui/"+strconv.Itoa(uid)+"/io.example.test") {
			t.Fatalf("launchctl calls: %v", f.calls)
		}
	case "linux":
		if !f.ran("systemctl --user enable io.example.test.service") || !f.ran("systemctl --user start io.example.test.service") ||
			!f.ran("systemctl --user disable --now io.example.test.service") {
			t.Fatalf("systemctl calls: %v", f.calls)
		}
	}
}

func TestDefaultNameCanBeChangedForTests(t *testing.T) {
	t.Setenv("HERDR_ANYWHERE_AUTOSTART_NAME", "io.example.other")
	if got := Default().Name; got != "io.example.other" {
		t.Fatal(got)
	}
	t.Setenv("HERDR_ANYWHERE_AUTOSTART_NAME", "")
	if got := Default().Name; got == "" || got == "io.example.other" {
		t.Fatal(got)
	}
}

func TestPathsStayInHome(t *testing.T) {
	s := &Service{Name: "n", Home: "/h"}
	switch runtime.GOOS {
	case "darwin":
		if s.Path() != filepath.Join("/h", "Library", "LaunchAgents", "n.plist") {
			t.Fatal(s.Path())
		}
	case "linux":
		t.Setenv("XDG_CONFIG_HOME", "")
		if s.Path() != filepath.Join("/h", ".config", "systemd", "user", "n.service") {
			t.Fatal(s.Path())
		}
		t.Setenv("XDG_CONFIG_HOME", "/x")
		if s.Path() != filepath.Join("/x", "systemd", "user", "n.service") {
			t.Fatal(s.Path())
		}
	}
}
