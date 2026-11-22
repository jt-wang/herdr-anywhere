package cursor

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"herdr-anywhere/companion/internal/chat"
)

const managed = "herdr-anywhere.cursor-hook"

var events = []string{"sessionStart", "beforeSubmitPrompt"}

type Setup struct{ Dir, Binary, HC string }

func supportsOS(goos string) bool { return goos == "linux" || goos == "darwin" }

var supported = func() bool { return supportsOS(runtime.GOOS) }

var hookCommand = regexp.MustCompile(`^('(?:[^']|'"'"')+') cursor-hook --hc ('(?:[^']|'"'"')+')$`)

func pristine(raw json.RawMessage) bool {
	var obj map[string]json.RawMessage
	var command string
	var timeout int
	if json.Unmarshal(raw, &obj) != nil || len(obj) != 3 || !owned(raw) || json.Unmarshal(obj["command"], &command) != nil || json.Unmarshal(obj["timeout"], &timeout) != nil || timeout != 2 {
		return false
	}
	m := hookCommand.FindStringSubmatch(command)
	if m == nil {
		return false
	}
	for _, arg := range m[1:] {
		if !filepath.IsAbs(strings.ReplaceAll(arg[1:len(arg)-1], "'\"'\"'", "'")) {
			return false
		}
	}
	return true
}

func (s *Setup) Agent() string  { return "cursor" }
func (s *Setup) Name() string   { return "Cursor" }
func (s *Setup) File() string   { return filepath.Join(s.root(), "hooks.json") }
func (s *Setup) Version() int   { return 1 }
func (s *Setup) Reload() string { return "" }
func (s *Setup) root() string {
	if s.Dir != "" {
		return s.Dir
	}
	// Cursor 2026.11.19 hooks-exec getDefaultPaths (Ve) ignores conversation config overrides.
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".cursor")
}
func quote(s string) string {
	if runtime.GOOS == "windows" {
		return `"` + s + `"`
	}
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}
func (s *Setup) entry() json.RawMessage {
	b, _ := json.Marshal(map[string]any{"command": quote(s.Binary) + " cursor-hook --hc " + quote(s.HC), managed: 1, "timeout": 2})
	return b
}

type document struct {
	top   map[string]json.RawMessage
	hooks map[string]json.RawMessage
	raw   []byte
	mode  os.FileMode
}

func (s *Setup) read() (document, error) {
	d := document{top: map[string]json.RawMessage{}, hooks: map[string]json.RawMessage{}, mode: 0600}
	b, e := os.ReadFile(s.File())
	if os.IsNotExist(e) {
		d.top["version"] = json.RawMessage("1")
		return d, nil
	}
	if e != nil {
		return d, e
	}
	d.raw = b
	st, e := os.Lstat(s.File())
	if e != nil {
		return d, e
	}
	if !st.Mode().IsRegular() {
		return d, fmt.Errorf("Cursor hooks.json is not a regular file; left untouched")
	}
	d.mode = st.Mode().Perm()
	if json.Unmarshal(b, &d.top) != nil || d.top == nil {
		return d, fmt.Errorf("Cursor hooks.json is malformed; left untouched")
	}
	var version int
	if json.Unmarshal(d.top["version"], &version) != nil || version != 1 {
		return d, fmt.Errorf("Cursor hooks.json has an unsupported version; left untouched")
	}
	if h, ok := d.top["hooks"]; ok {
		if json.Unmarshal(h, &d.hooks) != nil || d.hooks == nil {
			return d, fmt.Errorf("Cursor hooks.json has invalid hooks; left untouched")
		}
	}
	for event, raw := range d.hooks {
		var entries []json.RawMessage
		if json.Unmarshal(raw, &entries) != nil || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return d, fmt.Errorf("Cursor %s hooks are not an array; left untouched", event)
		}
		for _, entry := range entries {
			var obj map[string]json.RawMessage
			var command string
			if json.Unmarshal(entry, &obj) != nil || json.Unmarshal(obj["command"], &command) != nil || command == "" {
				return d, fmt.Errorf("Cursor %s hook is invalid; left untouched", event)
			}
			if marker, ok := obj[managed]; ok && string(marker) != "1" {
				return d, fmt.Errorf("Cursor managed hook has an unsupported version; left untouched")
			}
		}
	}
	return d, nil
}

func owned(raw json.RawMessage) bool {
	var obj map[string]json.RawMessage
	return json.Unmarshal(raw, &obj) == nil && string(obj[managed]) == "1"
}

func (s *Setup) available() bool {
	if !supported() {
		return false
	}
	dir := s.root()
	if s.Dir == "" {
		dir = ConfigRoot()
	}
	if st, e := os.Stat(dir); e == nil && st.IsDir() {
		return true
	}
	st, e := os.Stat(s.root())
	return e == nil && st.IsDir()
}

func (s *Setup) State() chat.SetupState {
	if !s.available() {
		return chat.NoAgent
	}
	d, e := s.read()
	if e != nil {
		return chat.Changed
	}
	n, old := 0, false
	for _, event := range events {
		count := 0
		var entries []json.RawMessage
		_ = json.Unmarshal(d.hooks[event], &entries)
		for _, entry := range entries {
			if owned(entry) {
				if !pristine(entry) {
					return chat.Changed
				}
				count++
				n++
				var a, b any
				_ = json.Unmarshal(entry, &a)
				_ = json.Unmarshal(s.entry(), &b)
				aa, _ := json.Marshal(a)
				bb, _ := json.Marshal(b)
				if !bytes.Equal(aa, bb) {
					old = true
				}
			}
		}
		if count != 1 {
			old = true
		}
	}
	if n == 0 {
		return chat.Absent
	}
	if n == len(events) && !old {
		return chat.Installed
	}
	return chat.Old
}

func (s *Setup) Install(force bool) error { return s.change(true, force) }
func (s *Setup) Remove(force bool) error  { return s.change(false, force) }
func (s *Setup) change(install, force bool) error {
	if install {
		if !s.available() {
			return chat.ErrNoAgent
		}
		if !filepath.IsAbs(s.Binary) || !filepath.IsAbs(s.HC) {
			return fmt.Errorf("Cursor hook requires absolute Companion and hc paths")
		}
	}
	d, e := s.read()
	if e != nil {
		return e
	}
	for _, event := range events {
		var entries []json.RawMessage
		_ = json.Unmarshal(d.hooks[event], &entries)
		for _, entry := range entries {
			if owned(entry) && !pristine(entry) && !force {
				return chat.ErrChanged
			}
		}
	}
	changed := false
	for _, event := range events {
		var entries []json.RawMessage
		_ = json.Unmarshal(d.hooks[event], &entries)
		kept := []json.RawMessage{}
		for _, entry := range entries {
			if owned(entry) {
				changed = true
				continue
			}
			kept = append(kept, entry)
		}
		if install {
			kept = append(kept, s.entry())
			changed = true
		}
		if len(kept) == 0 {
			delete(d.hooks, event)
		} else {
			d.hooks[event], _ = json.Marshal(kept)
		}
	}
	if !changed {
		return nil
	}
	if install {
		if e := os.MkdirAll(s.root(), 0700); e != nil {
			return e
		}
	}
	if len(d.hooks) == 0 {
		delete(d.top, "hooks")
	} else {
		d.top["hooks"], _ = json.Marshal(d.hooks)
	}
	// A file made solely for this setup disappears again; user settings stay.
	if !install && len(d.top) == 1 && string(d.top["version"]) == "1" {
		return os.Remove(s.File())
	}
	b, e := json.MarshalIndent(d.top, "", "  ")
	if e != nil {
		return e
	}
	b = append(b, '\n')
	// Refuse a concurrent editor's changes rather than overwriting them.
	current, e := os.ReadFile(s.File())
	if e != nil && !os.IsNotExist(e) {
		return e
	}
	if !bytes.Equal(current, d.raw) {
		return fmt.Errorf("Cursor hooks.json changed while editing; left untouched")
	}
	f, e := os.CreateTemp(s.root(), ".herdr-cursor-*")
	if e != nil {
		return e
	}
	name := f.Name()
	defer os.Remove(name)
	if e = f.Chmod(d.mode); e == nil {
		_, e = f.Write(b)
	}
	closeErr := f.Close()
	if e != nil {
		return e
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(name, s.File())
}
