package pi

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	"herdr-anywhere/companion/internal/chat"
)

func profileDir(t *testing.T, home, name string) string {
	t.Helper()
	dir := filepath.Join(home, ".omp", "profiles", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dir, "agent")
}

func TestOMPDefaultSetupReadsProfileBlobImages(t *testing.T) {
	for _, storage := range []string{"agent", "xdg", "xdg-with-legacy-data"} {
		t.Run(storage, func(t *testing.T) {
			if storage != "agent" && runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
				t.Skip("OMP uses XDG on Linux and macOS")
			}
			home := ompEnv(t)
			agent := profileDir(t, home, "blue")
			data := agent
			if storage != "agent" {
				xdg := filepath.Join(home, "data")
				t.Setenv("XDG_DATA_HOME", xdg)
				data = filepath.Join(xdg, "omp", "profiles", "blue")
				if err := os.MkdirAll(data, 0o755); err != nil {
					t.Fatal(err)
				}
				if storage == "xdg-with-legacy-data" {
					data = agent
				}
			}
			s := OMPSetup("/sample/hc")
			if s.State() != chat.Absent {
				t.Fatal(s.State())
			}
			if err := s.Install(false); err != nil || s.State() != chat.Installed {
				t.Fatalf("install: %v %s", err, s.State())
			}
			for _, dir := range []string{s.Dir, agent} {
				if b, err := os.ReadFile(filepath.Join(dir, "extensions", "herdr-anywhere-omp.ts")); err != nil || string(b) != content(ompCurrent, s.HC) {
					t.Fatalf("extension %s: %v", dir, err)
				}
			}
			for _, sub := range []string{"sessions", "blobs"} {
				if err := os.MkdirAll(filepath.Join(data, sub), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			fixture := "testdata/omp/19.0.0/externalized"
			journal, err := os.ReadFile(filepath.Join(fixture, "images.jsonl"))
			if err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(data, "sessions", "blue.jsonl")
			if err := os.WriteFile(file, journal, 0o600); err != nil {
				t.Fatal(err)
			}
			blobs, _ := filepath.Glob(filepath.Join(fixture, "blobs", "*"))
			if len(blobs) != 1 {
				t.Fatal(blobs)
			}
			want, err := os.ReadFile(blobs[0])
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(data, "blobs", filepath.Base(blobs[0])), want, 0o600); err != nil {
				t.Fatal(err)
			}
			r := &Reader{Setup: s}
			p := chat.Pane{Pid: 17, Labels: map[string]string{chat.LabelPid: "17", chat.LabelSession: file}}
			path, _, err := r.FindPane(p)
			if err != nil || path != file {
				t.Fatalf("blue session: %q %v", path, err)
			}
			for _, id := range []string{"u1:1", "read1#0"} {
				media, got, err := r.Image(path, id)
				if err != nil || media != "image/png" || !bytes.Equal(got, want) {
					t.Fatalf("blue image %s: %s %d %v", id, media, len(got), err)
				}
			}
			if err := s.Remove(false); err != nil || s.State() != chat.Absent {
				t.Fatalf("remove: %v %s", err, s.State())
			}
			for _, dir := range []string{s.Dir, agent} {
				if _, err := os.Stat(filepath.Join(dir, "extensions", "herdr-anywhere-omp.ts")); !os.IsNotExist(err) {
					t.Fatalf("extension remains: %s %v", dir, err)
				}
			}
			if _, _, err := r.FindPane(p); miss(err) != chat.SetupNeeded {
				t.Fatalf("removed setup accepted retained blue labels: %v", err)
			}
		})
	}
}

func TestOMPSetupDiscoversNewProfiles(t *testing.T) {
	home := ompEnv(t)
	s := OMPSetup("/sample/hc")
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if s.State() != chat.Absent {
		t.Fatal(s.State())
	}
	if err := s.Install(false); err != nil || s.State() != chat.Installed {
		t.Fatalf("install: %v %s", err, s.State())
	}
	blue := profileDir(t, home, "blue")
	if s.State() != chat.Old {
		t.Fatalf("new profile without extension: %s", s.State())
	}
	if !slices.Contains((&Reader{Setup: s}).sessionDirs(), filepath.Join(blue, "sessions")) {
		t.Fatal("new profile's sessions not discovered")
	}
	if err := s.Install(false); err != nil || s.State() != chat.Installed {
		t.Fatalf("update: %v %s", err, s.State())
	}
	if err := s.Remove(false); err != nil || s.State() != chat.Absent {
		t.Fatalf("remove: %v %s", err, s.State())
	}
	if _, err := os.Stat(filepath.Join(blue, "extensions", "herdr-anywhere-omp.ts")); !os.IsNotExist(err) {
		t.Fatalf("blue extension remains: %v", err)
	}
}

func TestOMPMissingProfileOffersInstallWithoutBreakingLabeledPane(t *testing.T) {
	home := ompEnv(t)
	if err := os.MkdirAll(filepath.Join(home, ".omp", "agent"), 0o755); err != nil {
		t.Fatal(err)
	}
	s := OMPSetup("/sample/hc")
	if err := s.Install(false); err != nil {
		t.Fatal(err)
	}
	r := &Reader{Setup: s}
	p := chat.Pane{Pid: 17}
	if _, _, err := r.FindPane(p); miss(err) != chat.SetupNotLoaded {
		t.Fatalf("installed but unlabeled: %v", err)
	}
	blue := profileDir(t, home, "blue")
	if _, _, err := r.FindPane(p); miss(err) != chat.SetupNeeded {
		t.Fatalf("missing profile must offer Install: %v", err)
	}
	sessions := filepath.Join(s.Dir, "sessions")
	if err := os.MkdirAll(sessions, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(sessions, "sample.jsonl")
	if err := os.WriteFile(file, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	p.Labels = map[string]string{chat.LabelPid: "17", chat.LabelSession: file}
	if path, _, err := r.FindPane(p); err != nil || path != file {
		t.Fatalf("labeled default stopped: %s %v", path, err)
	}
	if err := s.Install(false); err != nil {
		t.Fatal(err)
	}
	p.Labels = nil
	if _, _, err := r.FindPane(p); miss(err) != chat.SetupNotLoaded {
		t.Fatalf("installed profile should restart: %v", err)
	}
	file = filepath.Join(blue, "extensions", "herdr-anywhere-omp.ts")
	if err := os.WriteFile(file, []byte(content(ompPrevious, s.HC)), 0o600); err != nil {
		t.Fatal(err)
	}
	if s.State() != chat.Old {
		t.Fatal(s.State())
	}
	if _, _, err := r.FindPane(p); miss(err) != chat.SetupNotLoaded {
		t.Fatalf("old copy is not missing: %v", err)
	}
}

func TestOMPSetupIgnoresInvalidProfileDirectories(t *testing.T) {
	home := ompEnv(t)
	blue := profileDir(t, home, "blue")
	invalid := []string{"UPPER", "has space", "_hidden", "default"}
	for _, name := range invalid {
		profileDir(t, home, name)
	}
	if err := os.WriteFile(filepath.Join(home, ".omp", "profiles", "not-a-directory"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	s := OMPSetup("/sample/hc")
	if err := s.Install(false); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(s.roots(false), filepath.Join(blue, "sessions")) {
		t.Fatal("valid profile ignored")
	}
	for _, name := range invalid {
		agent := filepath.Join(home, ".omp", "profiles", name, "agent")
		if _, err := os.Stat(agent); !os.IsNotExist(err) || slices.Contains(s.roots(false), filepath.Join(agent, "sessions")) {
			t.Fatalf("invalid profile included: %s %v", name, err)
		}
	}
	if slices.Contains(s.roots(false), filepath.Join(home, ".omp", "profiles", "not-a-directory", "agent", "sessions")) {
		t.Fatal("non-directory profile included")
	}
	for _, name := range []string{"../escape", "UPPER", "name.", "con", "con.txt", "com0", "lpt9.log", "default"} {
		if validOMPProfile(name) != "" {
			t.Fatalf("invalid profile accepted: %s", name)
		}
	}
}

func TestOMPProfileChangedFilesAreProtected(t *testing.T) {
	home := ompEnv(t)
	blue := profileDir(t, home, "blue")
	s := OMPSetup("/sample/hc")
	if err := s.Install(false); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(blue, "extensions", "herdr-anywhere-omp.ts")
	changed := []byte("// edited sample extension\n")
	if err := os.WriteFile(file, changed, 0o600); err != nil {
		t.Fatal(err)
	}
	if s.State() != chat.Changed || s.File() != file {
		t.Fatalf("changed profile: %s %s", s.State(), s.File())
	}
	for _, install := range []bool{true, false} {
		var err error
		if install {
			err = s.Install(false)
		} else {
			err = s.Remove(false)
		}
		if !errors.Is(err, chat.ErrChanged) {
			t.Fatalf("changed protection: %v", err)
		}
		if b, err := os.ReadFile(file); err != nil || !bytes.Equal(b, changed) {
			t.Fatalf("changed file modified: %v", err)
		}
		if _, err := os.Stat(filepath.Join(s.Dir, "extensions", "herdr-anywhere-omp.ts")); err != nil {
			t.Fatalf("preflight modified another target: %v", err)
		}
	}
	if err := s.Install(true); err != nil || s.State() != chat.Installed {
		t.Fatalf("force install: %v %s", err, s.State())
	}
	if err := os.WriteFile(file, []byte(content(ompPrevious, s.HC)), 0o600); err != nil {
		t.Fatal(err)
	}
	if s.State() != chat.Old {
		t.Fatal(s.State())
	}
	if err := s.Install(false); err != nil || s.State() != chat.Installed {
		t.Fatalf("update old profile: %v %s", err, s.State())
	}
	if err := os.WriteFile(file, changed, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.Remove(true); err != nil || s.State() != chat.Absent {
		t.Fatalf("force remove: %v %s", err, s.State())
	}
}

func TestOMPProfileTargetsRespectOverrideAndSelectedProfile(t *testing.T) {
	for _, selected := range []string{"", "green"} {
		t.Run(selected, func(t *testing.T) {
			home := ompEnv(t)
			blue := profileDir(t, home, "blue")
			override := filepath.Join(home, "custom")
			t.Setenv("PI_CODING_AGENT_DIR", override)
			t.Setenv("OMP_PROFILE", selected)
			s := OMPSetup("/sample/hc")
			if err := s.Install(false); err != nil {
				t.Fatal(err)
			}
			dirs := []string{blue, override}
			if selected != "" {
				dirs = []string{blue, filepath.Join(home, ".omp", "agent"), filepath.Join(home, ".omp", "profiles", selected, "agent")}
				if _, err := os.Stat(override); !os.IsNotExist(err) {
					t.Fatalf("profile should ignore override: %v", err)
				}
			}
			for _, dir := range dirs {
				if _, err := os.Stat(filepath.Join(dir, "extensions", "herdr-anywhere-omp.ts")); err != nil {
					t.Fatalf("target %s: %v", dir, err)
				}
			}
		})
	}
}

func TestOMPProfileSessionContainment(t *testing.T) {
	home := ompEnv(t)
	blue := profileDir(t, home, "blue")
	s := OMPSetup("/sample/hc")
	if err := s.Install(false); err != nil {
		t.Fatal(err)
	}
	r := &Reader{Setup: s}
	foreign := filepath.Join(t.TempDir(), "foreign.jsonl")
	if err := os.WriteFile(foreign, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	p := chat.Pane{Pid: 17, Labels: map[string]string{chat.LabelPid: "17", chat.LabelSession: foreign}}
	if _, _, err := r.FindPane(p); miss(err) != chat.NoMatch {
		t.Fatalf("foreign session accepted: %v", err)
	}
	sessions := filepath.Join(blue, "sessions")
	if err := os.MkdirAll(sessions, 0o755); err != nil {
		t.Fatal(err)
	}
	linked := filepath.Join(sessions, "linked.jsonl")
	if err := os.Symlink(foreign, linked); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	p.Labels[chat.LabelSession] = linked
	if _, _, err := r.FindPane(p); miss(err) != chat.NoMatch {
		t.Fatalf("session symlink escaped profile root: %v", err)
	}
}
