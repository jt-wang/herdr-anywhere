package pi

import (
	_ "embed"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"herdr-anywhere/companion/internal/chat"
)

// FileName is the one file Herdr Anywhere puts in Pi's extensions folder.
const FileName = "herdr-anywhere.ts"

// The file this Companion writes, and the ones earlier versions wrote (by version), so a file of ours left by an
// older Companion is updated, not taken for one someone changed. A new version adds an entry; none is removed.
//
//go:embed herdr-anywhere.ts
var current string

//go:embed herdr-anywhere-v1.txt
var previous string

const version = 2

var templates = map[int]string{version: current, 1: previous}

// Setup is Pi's chat.Setup. Pi loads every .ts file in its extensions folder at start and at /reload (measured
// with Pi 1.4.0: a file put there while Pi ran was not used until /reload, not even after a turn).
type Setup struct {
	Dir          string // Pi's own folder: PI_CODING_AGENT_DIR, else ~/.pi/agent (AgentDir)
	HC           string // the hc the file calls
	ID           string
	Title        string
	SessionRoots []string
	BlobDir      string
	templates    map[int]string
	targets      func() ([]*Setup, error)
	confirmMu    sync.Mutex
}

var _ chat.Setup = (*Setup)(nil)

// AgentDir is Pi's folder, as Pi itself decides it (its config.ts: PI_CODING_AGENT_DIR, else ~/.pi/agent).
func AgentDir() string {
	if d := os.Getenv("PI_CODING_AGENT_DIR"); d != "" {
		return d
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".pi", "agent")
}

func (s *Setup) Agent() string {
	if s.ID != "" {
		return s.ID
	}
	return "pi"
}
func (s *Setup) Name() string {
	if s.Title != "" {
		return s.Title
	}
	return "Pi"
}
func (s *Setup) Reload() string {
	if s.Agent() == "omp" {
		return "/restart"
	}
	return "/reload"
}
func (s *Setup) Version() int { return version }
func (s *Setup) File() string {
	if s.targets != nil {
		ts, _ := s.targets()
		for _, target := range ts {
			if target.State() == chat.Changed {
				return target.File()
			}
		}
	}
	name := FileName
	if s.Agent() == "omp" {
		name = "herdr-anywhere-omp.ts"
	}
	return filepath.Join(s.Dir, "extensions", name)
}

func content(tmpl, hc string) string {
	q, _ := json.Marshal(hc)
	return strings.Replace(tmpl, `"{{HC}}"`, string(q), 1)
}

func (s *Setup) variants() map[int]string {
	if s.templates != nil {
		return s.templates
	}
	return templates
}

// The line's end may be CRLF: a Windows checkout can embed the file so (git's autocrlf).
var hcLine = regexp.MustCompile(`(?m)^const HC = ("(?:[^"\\]|\\.)*");\r?$`)

func (s *Setup) State() chat.SetupState {
	if s.targets != nil {
		return s.allState()
	}
	if st, err := os.Stat(s.Dir); err != nil || !st.IsDir() {
		return chat.NoAgent
	}
	b, err := os.ReadFile(s.File())
	if err != nil {
		return chat.Absent
	}
	got := string(b)
	if got == content(s.variants()[s.Version()], s.HC) {
		return chat.Installed
	}
	if m := hcLine.FindStringSubmatch(got); m != nil {
		var hc string
		if json.Unmarshal([]byte(m[1]), &hc) == nil {
			for _, t := range s.variants() {
				if got == content(t, hc) {
					return chat.Old
				}
			}
		}
	}
	return chat.Changed
}

// Install writes the file. A changed file is left as it is unless force.
func (s *Setup) Install(force bool) error {
	if s.targets != nil {
		return s.applyTargets(true, force)
	}
	switch s.State() {
	case chat.NoAgent:
		return chat.ErrNoAgent
	case chat.Installed:
		return nil
	case chat.Changed:
		if !force {
			return chat.ErrChanged
		}
	}
	dir := filepath.Dir(s.File())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	// Written beside it under a name Pi doesn't load (not .ts), then moved in place in one step.
	tmp := filepath.Join(dir, "."+filepath.Base(s.File())+".tmp")
	if err := os.WriteFile(tmp, []byte(content(s.variants()[s.Version()], s.HC)), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.File()); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// Remove deletes the file, and the extensions folder when nothing else is left in it. A changed file is left as it is
// unless force.
func (s *Setup) Remove(force bool) error {
	if s.targets != nil {
		return s.applyTargets(false, force)
	}
	switch s.State() {
	case chat.NoAgent, chat.Absent:
		return nil
	case chat.Changed:
		if !force {
			return chat.ErrChanged
		}
	}
	if err := os.Remove(s.File()); err != nil && !os.IsNotExist(err) {
		return err
	}
	os.Remove(filepath.Dir(s.File())) // only goes when empty
	return nil
}
