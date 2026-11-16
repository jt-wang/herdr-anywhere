// Package autostart keeps serve running after the computer restarts: the computer's own service manager starts it
// when the person logs in, and again after a crash. launchd (a LaunchAgent) on macOS, systemd's user instance on
// Linux, and on Windows the Run key, which starts it at login only.
//
// As the programs people already run this way do it: Syncthing documents a LaunchAgent in ~/Library/LaunchAgents and
// a systemd user unit (docs.syncthing.net/users/autostart.html); tailscaled's install-system-daemon writes a launchd
// plist with RunAtLoad and loads it with launchctl (tailscale cmd/tailscaled/install_darwin.go); Ollama's Linux
// installer writes a unit with Restart=always and PATH set (ollama scripts/install.sh). Here it is per user, not
// system-wide, because serve runs herdr-core as the person.
package autostart

import (
	"bytes"
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
)

// Entry is the program the manager runs and how.
type Entry struct {
	Exe  string
	Args []string
	// Env is set for it: PATH, so serve finds herdr-core where the shell that installed it did.
	Env map[string]string
	// Log takes its output where the manager has no log of its own (launchd).
	Log string
}

// ErrNoManager: nothing on this computer starts it now; the entry starts it at the next login.
var ErrNoManager = errors.New("no service manager to start it now")

// ErrNotHere: this computer has no way to start serve at login.
var ErrNotHere = errors.New("starting at login isn't supported on this computer")

// Service is the entry for one name, kept under Home.
type Service struct {
	// Name is the launchd label, the systemd unit's name without .service, or the Run key's value name.
	Name string
	Home string
	run  func(name string, args ...string) ([]byte, error)
	// refuse, when set, is why this entry is not made, started or removed.
	refuse error
}

// NameEnv names another entry, so a test's serve never takes the place of the person's.
const NameEnv = "HERDR_ANYWHERE_AUTOSTART_NAME"

// DefaultName is the entry's name when NameEnv is not set: the person's own.
const DefaultName = defaultName

// Default is this user's entry. Under a HOME that is not the account's own (a test's, mostly) and with no NameEnv it is
// refused: the file would go in that HOME, where the service manager never looks at login, while the name is the
// person's in their own domain (launchd's gui/<uid>, systemd's user instance), so loading it would first unload the
// person's Companion and then put this one in its place.
func Default() *Service {
	home, _ := os.UserHomeDir()
	name := os.Getenv(NameEnv)
	s := &Service{Name: name, Home: home}
	if name == "" {
		s.Name = defaultName
		if account := accountHome(); runtime.GOOS != "windows" && account != "" && home != "" && !sameDir(home, account) {
			s.refuse = fmt.Errorf("HOME is %s, not this account's home (%s), so it is not set to start at login under this account's own name; %s gives it a name of its own", home, account, NameEnv)
		}
	}
	return s
}

// accountHome is the home the user database gives this account, whatever HOME says.
func accountHome() string {
	u, err := user.Current()
	if err != nil {
		return ""
	}
	return u.HomeDir
}

func sameDir(a, b string) bool {
	if ra, err := filepath.EvalSymlinks(a); err == nil {
		a = ra
	}
	if rb, err := filepath.EvalSymlinks(b); err == nil {
		b = rb
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

func (s *Service) command(name string, args ...string) ([]byte, error) {
	if s.run != nil {
		return s.run(name, args...)
	}
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("%s %s: %v: %s", name, strings.Join(args, " "), err, bytes.TrimSpace(out))
	}
	return out, nil
}

// writeIfChanged writes b at path unless it already holds exactly b, and says whether it wrote.
func writeIfChanged(path string, b []byte) (bool, error) {
	if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, b) {
		return false, nil
	}
	if err := os.MkdirAll(dirOf(path), 0o755); err != nil {
		return false, err
	}
	tmp := path + ".new"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return false, err
	}
	return true, os.Rename(tmp, path)
}

func dirOf(path string) string {
	i := strings.LastIndexAny(path, `/\`)
	if i < 0 {
		return "."
	}
	return path[:i]
}

func xmlText(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '"':
			b.WriteString("&quot;")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// launchdPlist is the LaunchAgent: run at login (RunAtLoad), and again unless it ended by being stopped
// (KeepAlive SuccessfulExit false: serve exits 0 on SIGTERM, 1 on a failure). AbandonProcessGroup keeps launchd from
// killing what serve started in its process group, such as herdr-core, when serve ends.
func launchdPlist(label string, e Entry) []byte {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
`)
	str := func(k, v string) { fmt.Fprintf(&b, "\t<key>%s</key>\n\t<string>%s</string>\n", k, xmlText(v)) }
	str("Label", label)
	b.WriteString("\t<key>ProgramArguments</key>\n\t<array>\n")
	for _, a := range append([]string{e.Exe}, e.Args...) {
		fmt.Fprintf(&b, "\t\t<string>%s</string>\n", xmlText(a))
	}
	b.WriteString("\t</array>\n")
	b.WriteString("\t<key>RunAtLoad</key>\n\t<true/>\n")
	b.WriteString("\t<key>KeepAlive</key>\n\t<dict>\n\t\t<key>SuccessfulExit</key>\n\t\t<false/>\n\t</dict>\n")
	b.WriteString("\t<key>AbandonProcessGroup</key>\n\t<true/>\n")
	if len(e.Env) > 0 {
		b.WriteString("\t<key>EnvironmentVariables</key>\n\t<dict>\n")
		for _, k := range slices.Sorted(maps.Keys(e.Env)) {
			fmt.Fprintf(&b, "\t\t<key>%s</key>\n\t\t<string>%s</string>\n", xmlText(k), xmlText(e.Env[k]))
		}
		b.WriteString("\t</dict>\n")
	}
	if e.Log != "" {
		str("StandardOutPath", e.Log)
		str("StandardErrorPath", e.Log)
	}
	b.WriteString("</dict>\n</plist>\n")
	return []byte(b.String())
}

// systemdQuote is one ExecStart word: quoted, with systemd's specifiers (%) and variables ($) escaped.
func systemdQuote(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, `%`, `%%`, `$`, `$$`)
	return `"` + r.Replace(s) + `"`
}

// systemdUnit is the user unit: started at login (default.target), again after a failure. KillMode=process stops
// only serve, leaving the agents' terminals it may have started running.
func systemdUnit(e Entry) string {
	var words []string
	for _, a := range append([]string{e.Exe}, e.Args...) {
		words = append(words, systemdQuote(a))
	}
	var b strings.Builder
	b.WriteString("[Unit]\nDescription=Herdr Anywhere companion\nWants=network-online.target\nAfter=network-online.target\n\n[Service]\n")
	fmt.Fprintf(&b, "ExecStart=%s\n", strings.Join(words, " "))
	for _, k := range slices.Sorted(maps.Keys(e.Env)) {
		fmt.Fprintf(&b, "Environment=%s\n", systemdQuote(k+"="+e.Env[k]))
	}
	b.WriteString("Restart=on-failure\nRestartSec=5\nKillMode=process\n\n[Install]\nWantedBy=default.target\n")
	return b.String()
}

// windowsQuote quotes one argument the way CommandLineToArgvW reads it back.
func windowsQuote(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t\"") {
		return s
	}
	var b strings.Builder
	b.WriteByte('"')
	slashes := 0
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '\\':
			slashes++
		case '"':
			b.WriteString(strings.Repeat(`\`, 2*slashes+1))
			slashes = 0
			b.WriteByte('"')
			continue
		default:
			b.WriteString(strings.Repeat(`\`, slashes))
			slashes = 0
			b.WriteByte(c)
			continue
		}
	}
	b.WriteString(strings.Repeat(`\`, 2*slashes))
	b.WriteByte('"')
	return b.String()
}

// windowsCommandLine is the Run key's value: the program, always quoted, then its arguments.
func windowsCommandLine(e Entry) string {
	words := []string{`"` + e.Exe + `"`}
	for _, a := range e.Args {
		words = append(words, windowsQuote(a))
	}
	return strings.Join(words, " ")
}
