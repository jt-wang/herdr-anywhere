package chat

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Answer is what the phone gets for one terminal. Chat false: no chat button. Unknown: there is a reader for this
// agent, but nothing sure to show, and why.
type Answer struct {
	Chat    bool   `json:"chat"`
	Unknown string `json:"unknown,omitempty"`
	// Setup is the agent whose Setup the phone can offer, when Unknown is SetupNeeded or SetupNotLoaded.
	Setup string `json:"setup,omitempty"`
	Turns []Turn `json:"turns,omitempty"`
}

// Service answers for a terminal of the local herdr-core: one `hc get`, the reader of the agent it reports, and a
// check that the record agrees with the screen.
type Service struct {
	// HC is the hc binary; its environment (HC_SESSION, XDG_*) is this process's, as for `hc connect`.
	HC string
	// Readers by the app name the agent reports in OSC 7501 (herdr-core's status.app).
	Readers map[string]Reader
	Log     *log.Logger
	// Ctx is how long the Service follows `hc events` once a chat needs it (serve's own lifetime).
	Ctx context.Context

	followFn   func(context.Context) // tests replace Follow
	followOnce sync.Once

	get    func(addr string) ([]byte, error) // tests replace `hc get`
	logged sync.Map

	mu       sync.Mutex
	sessions map[string]seen // by terminal address: the session id an agent last reported (Follow)
}

// A local address as herdr-core gives them (t7, t1b) or a terminal's name. One with "/" is on another machine.
var localAddress = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,63}$`)

type terminal struct {
	Address string            `json:"address"`
	Cwd     string            `json:"cwd"`
	Title   string            `json:"title"`
	Labels  map[string]string `json:"labels"`
	// Records are what the program reported on, by id: OpenCode 2 reports its session this way while it works.
	Records map[string]struct {
		App string `json:"app"`
	} `json:"records"`
	Status *struct {
		App   string `json:"app"`
		State string `json:"state"`
		Msg   string `json:"msg"` // what a blocked agent waits for, as it said (a permission prompt)
	} `json:"status"`
	Foreground *struct {
		Pid  int    `json:"pid"`
		Cwd  string `json:"cwd"`
		Name string `json:"name"`
	} `json:"foreground"`
}

func (s *Service) hcGet(addr string) ([]byte, error) {
	if s.get != nil {
		return s.get(addr)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, s.HC, "get", addr, "--json").Output()
}

// find runs the steps up to the session file. reader is nil when this terminal gets no chat button.
func (s *Service) find(addr string) (app string, r Reader, path, version string, state string, err error) {
	app, r, path, version, state, _, err = s.findMsg(addr)
	return
}

func (s *Service) findMsg(addr string) (app string, r Reader, path, version string, state, msg string, err error) {
	if !localAddress.MatchString(addr) {
		return "", nil, "", "", "", "", nil
	}
	out, err := s.hcGet(addr)
	if err != nil {
		return "", nil, "", "", "", "", nil
	}
	var t terminal
	if json.Unmarshal(out, &t) != nil {
		return "", nil, "", "", "", "", nil
	}
	if t.Status == nil {
		// A program that reports no status, known by its process name (OpenCode 1).
		app, r = s.named(t)
		if r == nil {
			return "", nil, "", "", "", "", nil
		}
		t.Status = &struct {
			App   string `json:"app"`
			State string `json:"state"`
			Msg   string `json:"msg"`
		}{App: app, State: unreported}
	}
	r = s.Readers[t.Status.App]
	if r == nil {
		return "", nil, "", "", "", "", nil
	}
	if t.Foreground == nil || t.Foreground.Pid <= 0 {
		return t.Status.App, r, "", "", t.Status.State, t.Status.Msg, &Miss{Reason: NoTerminal}
	}
	if pr, ok := r.(PaneReader); ok {
		if _, ok := r.(SessionReporter); ok {
			s.startFollow()
		}
		path, version, err = pr.FindPane(s.pane(t))
	} else {
		path, version, err = r.Find(t.Foreground.Pid)
	}
	return t.Status.App, r, path, version, t.Status.State, t.Status.Msg, err
}

// unreported is the state of a program that reports none (Named).
const unreported = "\x00unreported"

// named: the reader whose program runs in the foreground of a terminal that reports no status.
func (s *Service) named(t terminal) (string, Reader) {
	if t.Foreground == nil {
		return "", nil
	}
	name := strings.TrimSuffix(t.Foreground.Name, ".exe")
	for app, r := range s.Readers {
		if n, ok := r.(Named); ok && name != "" && n.ProgramName() == name {
			return app, r
		}
	}
	return "", nil
}

func (s *Service) Chat(addr string) Answer {
	app, r, path, version, state, msg, err := s.findMsg(addr)
	if r == nil {
		return Answer{}
	}
	var m *Miss
	if errors.As(err, &m) {
		a := Answer{Chat: true, Unknown: m.Reason}
		if m.Reason == SetupNeeded || m.Reason == SetupNotLoaded {
			a.Setup = app
		}
		return a
	}
	var fe *FormatError
	if errors.As(err, &fe) {
		s.once(app+" "+version+" "+fe.Field, "chat: %s %s: %v", app, version, err)
		return Answer{Chat: true, Unknown: Format}
	}
	if err != nil {
		return Answer{Chat: true, Unknown: NoMatch}
	}
	turns, err := r.Turns(path)
	if errors.As(err, &fe) {
		s.once(app+" "+version+" "+fe.Field, "chat: %s %s: %v", app, version, err)
		return Answer{Chat: true, Unknown: Format}
	}
	if err != nil {
		return Answer{Chat: true, Unknown: NoMatch}
	}
	open := len(turns) > 0 && turns[len(turns)-1].End == "open"
	// Behind: the screen and the record disagree on whether a turn runs. What the record has still goes with it, so
	// the phone shows the turns, marked as updating, and never an empty page while an agent works long.
	switch state {
	case unreported:
		// Nothing on the screen to check the record against: the record says whether the turn is open.
	case "working", "blocked":
		if !open {
			return Answer{Chat: true, Unknown: Behind, Turns: turns}
		}
	case "done", "idle", "error":
		if open {
			return Answer{Chat: true, Unknown: Behind, Turns: turns}
		}
	default:
		return Answer{Chat: true, Unknown: NoStatus}
	}
	// A permission prompt is only on the screen, not in the record (seen with Claude Code 2.1.319): the open turn asks it.
	if state == "blocked" && msg != "" && open && turns[len(turns)-1].Ask == nil {
		turns = append([]Turn(nil), turns...)
		turns[len(turns)-1].Ask = &Ask{Text: msg, Options: []string{}}
	}
	return Answer{Chat: true, Turns: turns}
}

// Step finds one step's details again, in the file this terminal's process has now.
func (s *Service) Step(addr, id string) (Detail, error) {
	_, r, path, _, _, err := s.find(addr)
	if r == nil || err != nil {
		return Detail{}, ErrNoStep
	}
	return r.Detail(path, id)
}

// Image finds an image of a turn or a step again, in the file this terminal's process has now.
func (s *Service) Image(addr, id string) (string, []byte, error) {
	_, r, path, _, _, err := s.find(addr)
	if r == nil || err != nil {
		return "", nil, ErrNoStep
	}
	return r.Image(path, id)
}

func (s *Service) once(key, f string, a ...any) {
	if _, seen := s.logged.LoadOrStore(key, true); !seen && s.Log != nil {
		s.Log.Printf(f, a...)
	}
}

// ClaudeDir is where Claude Code keeps its sessions, as Claude Code itself decides: CLAUDE_CONFIG_DIR, else ~/.claude.
func ClaudeDir() string {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return d
	}
	h, _ := os.UserHomeDir()
	return h + "/.claude"
}
