package pi

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"herdr-anywhere/companion/internal/chat"
)

func ompEnv(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	for _, k := range []string{"OMP_PROFILE", "PI_PROFILE", "PI_CONFIG_DIR", "PI_CODING_AGENT_DIR", "PI_CODING_AGENT_SESSION_DIR", "XDG_DATA_HOME"} {
		t.Setenv(k, "")
	}
	return home
}

func TestOMPDirs(t *testing.T) {
	for _, scenario := range []string{"default", "agent", "config", "absolute-config", "tilde-config", "profile", "legacy-profile", "empty-profile", "invalid-profile", "xdg-absent", "xdg", "profile-xdg-absent", "profile-xdg", "override-xdg", "sessions"} {
		t.Run(scenario, func(t *testing.T) {
			h := ompEnv(t)
			dir := filepath.Join(h, ".omp", "agent")
			sessions := filepath.Join(dir, "sessions")
			xdg := filepath.Join(h, "data", "omp")
			switch scenario {
			case "agent", "override-xdg":
				dir = filepath.Join(h, "custom")
				sessions = filepath.Join(dir, "sessions")
				t.Setenv("PI_CODING_AGENT_DIR", dir)
			case "config":
				t.Setenv("PI_CONFIG_DIR", ".nest")
				dir = filepath.Join(h, ".nest", "agent")
				sessions = filepath.Join(dir, "sessions")
			case "absolute-config":
				config := filepath.Join(t.TempDir(), "config")
				t.Setenv("PI_CONFIG_DIR", config)
				dir = filepath.Join(h, config, "agent")
				sessions = filepath.Join(dir, "sessions")
			case "tilde-config":
				t.Setenv("PI_CONFIG_DIR", "~/nest")
				dir = filepath.Join(h, "~", "nest", "agent")
				sessions = filepath.Join(dir, "sessions")
			case "profile", "legacy-profile", "profile-xdg", "profile-xdg-absent":
				t.Setenv("OMP_PROFILE", "blue")
				if scenario == "legacy-profile" {
					os.Unsetenv("OMP_PROFILE")
					t.Setenv("PI_PROFILE", "blue")
				}
				t.Setenv("PI_CODING_AGENT_DIR", filepath.Join(h, "ignored"))
				dir = filepath.Join(h, ".omp", "profiles", "blue", "agent")
				sessions = filepath.Join(dir, "sessions")
				if strings.Contains(scenario, "xdg") {
					xdg = filepath.Join(xdg, "profiles", "blue")
				}
			case "empty-profile":
				t.Setenv("PI_PROFILE", "ignored")
			case "invalid-profile":
				t.Setenv("OMP_PROFILE", "../escape")
			case "sessions":
				t.Setenv("PI_CODING_AGENT_SESSION_DIR", filepath.Join(h, "elsewhere"))
			}
			if strings.Contains(scenario, "xdg") {
				t.Setenv("XDG_DATA_HOME", filepath.Join(h, "data"))
				if !strings.HasSuffix(scenario, "absent") {
					if err := os.MkdirAll(xdg, 0o755); err != nil {
						t.Fatal(err)
					}
					if scenario != "override-xdg" && (runtime.GOOS == "linux" || runtime.GOOS == "darwin") {
						sessions = filepath.Join(xdg, "sessions")
					}
				}
			}
			s := OMPSetup("/hc")
			if s.Dir != dir || s.SessionRoots[0] != sessions {
				t.Fatalf("%+v; want %s, %s", s, dir, sessions)
			}
			if scenario == "sessions" && s.SessionRoots[1] != filepath.Join(h, "elsewhere") {
				t.Fatal(s.SessionRoots)
			}
		})
	}
}

func TestOMPAndPiShareAgentDir(t *testing.T) {
	home := ompEnv(t)
	dir := filepath.Join(home, "shared")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PI_CODING_AGENT_DIR", dir)
	pi := &Setup{Dir: AgentDir(), HC: "/opt/hc"}
	omp := OMPSetup(pi.HC)
	if err := pi.Install(false); err != nil {
		t.Fatal(err)
	}
	if pi.State() != chat.Installed || omp.State() != chat.Absent {
		t.Fatalf("after Pi install: Pi %s; OMP %s", pi.State(), omp.State())
	}
	if err := omp.Install(false); err != nil {
		t.Fatal(err)
	}
	if pi.State() != chat.Installed || omp.State() != chat.Installed {
		t.Fatalf("after OMP install: Pi %s; OMP %s", pi.State(), omp.State())
	}
	if filepath.Base(omp.File()) != "herdr-anywhere-omp.ts" {
		t.Fatal(omp.File())
	}
	if err := pi.Remove(false); err != nil {
		t.Fatal(err)
	}
	if pi.State() != chat.Absent || omp.State() != chat.Installed {
		t.Fatalf("after Pi removal: Pi %s; OMP %s", pi.State(), omp.State())
	}
	if err := omp.Remove(false); err != nil {
		t.Fatal(err)
	}
}

func TestOMPSetup(t *testing.T) {
	ompEnv(t)
	s := OMPSetup(filepath.Join(t.TempDir(), "hc with spaces"))
	if s.Reload() != "/restart" || (&Setup{}).Reload() != "/reload" {
		t.Fatalf("OMP reload %q; Pi reload %q", s.Reload(), (&Setup{}).Reload())
	}
	if s.Agent() != "omp" || s.Name() != "Oh My Pi" || s.State() != chat.NoAgent {
		t.Fatalf("%+v", s)
	}
	if err := s.Install(false); !errors.Is(err, chat.ErrNoAgent) {
		t.Fatal(err)
	}
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if s.State() != chat.Absent {
		t.Fatal(s.State())
	}
	if err := s.Install(false); err != nil {
		t.Fatal(err)
	}
	if s.State() != chat.Installed {
		t.Fatal(s.State())
	}
	b, _ := os.ReadFile(s.File())
	if !strings.Contains(string(b), "chat setup: omp 2") || !strings.Contains(string(b), "session_start") {
		t.Fatal(string(b))
	}
	if !strings.Contains(string(b), "after /restart.") {
		t.Fatal(string(b))
	}
	previous := content(strings.Replace(ompPrevious, "after /restart.", "after /reload or a restart.", 1), s.HC)
	if err := os.WriteFile(s.File(), []byte(previous), 0o644); err != nil {
		t.Fatal(err)
	}
	if s.State() != chat.Old {
		t.Fatalf("previous managed extension: %s", s.State())
	}
	if err := s.Install(false); err != nil {
		t.Fatal(err)
	}
	if s.State() != chat.Installed {
		t.Fatal(s.State())
	}
	s.HC += "-new"
	if s.State() != chat.Old {
		t.Fatal(s.State())
	}
	if err := s.Install(false); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.File(), []byte("my extension"), 0o644); err != nil {
		t.Fatal(err)
	}
	if s.State() != chat.Changed {
		t.Fatal(s.State())
	}
	if err := s.Install(false); !errors.Is(err, chat.ErrChanged) {
		t.Fatal(err)
	}
	if err := s.Remove(false); !errors.Is(err, chat.ErrChanged) {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(s.File()); string(b) != "my extension" {
		t.Fatal(string(b))
	}
	if err := s.Install(true); err != nil {
		t.Fatal(err)
	}
	if err := s.Remove(false); err != nil {
		t.Fatal(err)
	}
	if s.State() != chat.Absent {
		t.Fatal(s.State())
	}
}

func checkOMPTurns(t *testing.T, file string) {
	t.Helper()
	r := &Reader{}
	turns, err := r.Turns(file)
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 4 {
		t.Fatalf("turns: %+v", turns)
	}
	for i, reply := range []string{"Hello from the fake model.", "There are two files here.", "The check failed with code 3.", "It is a blue square."} {
		if turns[i].Reply != reply || turns[i].End != "done" {
			t.Fatalf("turn %d: %+v", i, turns[i])
		}
	}
	if turns[1].Steps[1].Title != "ls" || !turns[2].Steps[0].Failed || turns[3].Steps[0].Kind != "read" {
		t.Fatal(turns)
	}
	d, err := r.Detail(file, turns[2].Steps[0].ID)
	if err != nil || d.Run == nil || d.Run.Code != 3 || d.Run.Output != "checking\none test failed" {
		t.Fatalf("detail %+v, %v", d, err)
	}
	d, err = r.Detail(file, turns[3].Steps[0].ID)
	if err != nil || len(d.Images) != 1 {
		t.Fatalf("image %+v, %v", d, err)
	}
}

func TestOMPFixtures(t *testing.T) {
	checkOMPTurns(t, "testdata/omp/19.0.0/turns.jsonl")
	ts, err := (&Reader{}).Turns("testdata/omp/19.0.0/branch.jsonl")
	if err != nil || len(ts) != 2 {
		t.Fatalf("branch %+v, %v", ts, err)
	}
	if ts[1].Said != "HELLO alternate branch" || ts[1].Reply != "Hello from the fake model." {
		t.Fatal(ts)
	}
}

func TestOMPLabeledSession(t *testing.T) {
	ompEnv(t)
	s := OMPSetup("/hc")
	r := &Reader{Setup: s}
	p := chat.Pane{Pid: 17}
	_, _, err := r.FindPane(p)
	var miss *chat.Miss
	if !errors.As(err, &miss) || miss.Reason != chat.SetupNeeded {
		t.Fatal(err)
	}
	file := filepath.Join(s.SessionRoots[0], "project", "session.jsonl")
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := s.Install(false); err != nil {
		t.Fatal(err)
	}
	p.Labels = map[string]string{chat.LabelPid: "17", chat.LabelSession: file}
	_, _, err = r.FindPane(p)
	if !errors.As(err, &miss) || miss.Reason != chat.NoRecordYet {
		t.Fatal(err)
	}
	b, err := os.ReadFile("testdata/omp/19.0.0/turns.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, b, 0o644); err != nil {
		t.Fatal(err)
	}
	if got, _, err := r.FindPane(p); err != nil || got != file {
		t.Fatalf("%s %v", got, err)
	}
	p.Labels[chat.LabelPid] = "18"
	if _, _, err := r.FindPane(p); err == nil {
		t.Fatal("accepted stale process label")
	}
	p.Labels[chat.LabelPid] = "17"
	p.Labels[chat.LabelSession] = filepath.Join(t.TempDir(), "foreign.jsonl")
	if _, _, err := r.FindPane(p); !errors.As(err, &miss) || miss.Reason != chat.NoMatch {
		t.Fatal(err)
	}
	empty := filepath.Join(t.TempDir(), "empty.jsonl")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if ts, err := r.Turns(empty); err != nil || len(ts) != 0 {
		t.Fatalf("%+v %v", ts, err)
	}
	if err := os.WriteFile(empty, []byte("broken\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var format *chat.FormatError
	if _, err := r.Turns(empty); !errors.As(err, &format) {
		t.Fatal(err)
	}
}
