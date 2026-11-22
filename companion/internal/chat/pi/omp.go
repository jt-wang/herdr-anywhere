package pi

import (
	_ "embed"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"herdr-anywhere/companion/internal/chat"
)

var profileName = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
var reservedProfile = regexp.MustCompile(`(?i)^(CON|PRN|AUX|NUL|COM[0-9]|LPT[0-9])(\..*)?$`)

//go:embed herdr-anywhere-omp.ts
var ompCurrent string

//go:embed herdr-anywhere-omp-v1.txt
var ompPrevious string

// OMPSetup follows OMP 19's utils/dirs.ts: profiles ignore the agent-dir override;
// XDG data is used only after its omp (or omp/profiles/name) path exists.
func OMPSetup(hc string) *Setup {
	home, _ := os.UserHomeDir()
	config := os.Getenv("PI_CONFIG_DIR")
	if config == "" {
		config = ".omp"
	}
	// OMP 19 dirs.ts getBaseConfigRoot joins home even for absolute config values.
	root := filepath.Join(home, config)
	profile, ok := os.LookupEnv("OMP_PROFILE")
	if !ok {
		profile = os.Getenv("PI_PROFILE")
	}
	profile = strings.TrimSpace(profile)
	profile = validOMPProfile(profile)
	override := os.Getenv("PI_CODING_AGENT_DIR")
	if profile != "" {
		override = ""
	}
	xdg := os.Getenv("XDG_DATA_HOME")
	custom := os.Getenv("PI_CODING_AGENT_SESSION_DIR")
	makeTarget := func(name string) *Setup {
		return ompTarget(root, name, hc, override, xdg, custom)
	}
	s := makeTarget(profile)
	s.targets = func() ([]*Setup, error) {
		names := []string{profile}
		if profile != "" {
			names = append(names, "")
		}
		entries, err := os.ReadDir(filepath.Join(root, "profiles"))
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		for _, entry := range entries {
			name := entry.Name()
			if name == "" || validOMPProfile(name) != name {
				continue
			}
			st, err := os.Stat(filepath.Join(root, "profiles", name))
			if err != nil {
				if os.IsNotExist(err) {
					continue
				}
				return nil, err
			}
			if st.IsDir() {
				names = append(names, name)
			}
		}
		seen := map[string]bool{}
		var targets []*Setup
		for _, name := range names {
			target := makeTarget(name)
			if !seen[target.Dir] {
				seen[target.Dir] = true
				target.HC, target.templates = s.HC, s.templates
				targets = append(targets, target)
			}
		}
		return targets, nil
	}
	return s
}

func validOMPProfile(profile string) string {
	if profile == "default" || !profileName.MatchString(profile) || strings.HasSuffix(profile, ".") || reservedProfile.MatchString(profile) {
		return ""
	}
	return profile
}

func ompTarget(root, profile, hc, override, xdg, custom string) *Setup {
	if profile != "" {
		root = filepath.Join(root, "profiles", profile)
	}
	dir := filepath.Join(root, "agent")
	defaultDir := dir
	if profile == "" && override != "" {
		if abs, err := filepath.Abs(override); err == nil {
			dir = abs
		}
	}
	sessions := filepath.Join(dir, "sessions")
	if dir == defaultDir && (runtime.GOOS == "linux" || runtime.GOOS == "darwin") {
		if xdg != "" {
			base := filepath.Join(xdg, "omp")
			if profile != "" {
				base = filepath.Join(base, "profiles", profile)
			}
			if _, err := os.Stat(base); err == nil {
				sessions = filepath.Join(base, "sessions")
			}
		}
	}
	roots := []string{sessions}
	blobs := filepath.Join(filepath.Dir(sessions), "blobs")
	if custom != "" {
		if abs, err := filepath.Abs(custom); err == nil {
			roots = append(roots, abs)
		}
	}
	previous := strings.Replace(ompPrevious, "after /restart.", "after /reload or a restart.", 1)
	return &Setup{Dir: dir, HC: hc, ID: "omp", Title: "Oh My Pi", SessionRoots: roots, BlobDir: blobs, templates: map[int]string{version: ompCurrent, 1: ompPrevious, 0: previous}}
}

func (s *Setup) allState() chat.SetupState {
	targets, err := s.targets()
	if err != nil {
		return chat.Changed
	}
	found, installed, missing, old := false, false, false, false
	for _, target := range targets {
		switch target.State() {
		case chat.Changed:
			return chat.Changed
		case chat.Installed:
			found, installed = true, true
		case chat.Old:
			found, installed, old = true, true, true
		case chat.Absent:
			found, missing = true, true
		case chat.NoAgent:
			missing = true
			if st, err := os.Stat(filepath.Dir(target.Dir)); err == nil && st.IsDir() {
				found = true
			}
		}
	}
	if !found {
		return chat.NoAgent
	}
	if !installed {
		return chat.Absent
	}
	if old || missing {
		return chat.Old
	}
	return chat.Installed
}

func (s *Setup) missingTargets() bool {
	if s.targets == nil {
		return false
	}
	targets, err := s.targets()
	if err != nil {
		return false
	}
	for _, target := range targets {
		if state := target.State(); state == chat.Absent || state == chat.NoAgent {
			return true
		}
	}
	return false
}

func (s *Setup) applyTargets(install, force bool) error {
	targets, err := s.targets()
	if err != nil {
		return err
	}
	for _, target := range targets {
		if target.State() == chat.Changed && !force {
			return chat.ErrChanged
		}
	}
	if install && s.allState() == chat.NoAgent {
		return chat.ErrNoAgent
	}
	for _, target := range targets {
		if install {
			if err := os.MkdirAll(target.Dir, 0o755); err != nil {
				return err
			}
			err = target.Install(force)
		} else {
			err = target.Remove(force)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Setup) roots(blobs bool) []string {
	targets := []*Setup{s}
	if s.targets != nil {
		var err error
		targets, err = s.targets()
		if err != nil {
			return nil
		}
	}
	var roots []string
	seen := map[string]bool{}
	for _, target := range targets {
		dirs := append([]string{}, target.SessionRoots...)
		if blobs {
			dirs = []string{target.BlobDir}
		}
		if s.targets != nil {
			name := "sessions"
			if blobs {
				name = "blobs"
			}
			dirs = append(dirs, filepath.Join(target.Dir, name))
		}
		for _, dir := range dirs {
			if dir != "" && !seen[dir] {
				seen[dir] = true
				roots = append(roots, dir)
			}
		}
	}
	return roots
}
