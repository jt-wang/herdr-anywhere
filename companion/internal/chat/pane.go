package chat

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os/exec"
	"sort"
	"time"
)

// Pane is what herdr-core says about a terminal, for a reader that finds a session by more than its process.
type Pane struct {
	Pid   int
	Cwd   string // the program's folder
	Title string // the terminal's title, as the program set it
	// Sessions are the ids the program reported for its session in OSC 7501: now, or in its last turn while this
	// Companion was following (Follow).
	Sessions []string
	// Labels are the terminal's hc labels: Pi's file sets which conversation it writes there.
	Labels map[string]string
}

// PaneReader is a Reader that finds a session from the pane instead of the process's open files: OpenCode 2 keeps
// every session in one database and says which one only in its reports.
type PaneReader interface {
	Reader
	FindPane(Pane) (path, version string, err error)
}

// SessionReporter is a PaneReader whose agent reports its session to herdr-core only while it works (OpenCode 2):
// from its first chat the Service follows `hc events` to keep what it reported.
type SessionReporter interface {
	PaneReader
	ReportsSession()
}

// seen is a session id a terminal's program reported, and the process that was running then.
type seen struct {
	app, id string
	pid     int
}

// pane: the terminal as a PaneReader gets it, with the session remembered for the same process when herdr-core
// has none now (OpenCode 2 clears its report at the end of each turn).
func (s *Service) pane(t terminal) Pane {
	p := Pane{Pid: t.Foreground.Pid, Cwd: t.Foreground.Cwd, Title: t.Title, Labels: t.Labels}
	if p.Cwd == "" {
		p.Cwd = t.Cwd
	}
	for id, r := range t.Records {
		if r.App == t.Status.App {
			p.Sessions = append(p.Sessions, id)
		}
	}
	sort.Strings(p.Sessions)
	if len(p.Sessions) == 0 && t.Address != "" {
		s.mu.Lock()
		k, ok := s.sessions[t.Address]
		s.mu.Unlock()
		if ok && k.pid == p.Pid && k.app == t.Status.App {
			p.Sessions = []string{k.id}
		}
	}
	return p
}

// startFollow follows `hc events` from the first chat of a pane whose reader needs it, until Ctx ends.
func (s *Service) startFollow() {
	if s.Ctx == nil {
		return
	}
	s.followOnce.Do(func() {
		f := s.followFn
		if f == nil {
			f = s.Follow
		}
		go f(s.Ctx)
	})
}

// Follow keeps the session ids agents report, from `hc events`, until ctx ends; it starts the stream again when it
// breaks.
func (s *Service) Follow(ctx context.Context) {
	for ctx.Err() == nil {
		cmd := exec.CommandContext(ctx, s.HC, "events", "--json")
		out, err := cmd.StdoutPipe()
		if err == nil && cmd.Start() == nil {
			s.follow(out)
			cmd.Wait()
		}
		select {
		case <-ctx.Done():
		case <-time.After(5 * time.Second):
		}
	}
}

// maxEvent: an `hc events` line longer than this is passed over (a status event is a few hundred bytes).
const maxEvent = 4 << 20

// follow reads `hc events --json` lines: a status event whose records name a session is remembered with the
// process running in that terminal then. A line over maxEvent is passed over without holding it.
func (s *Service) follow(r io.Reader) {
	br := bufio.NewReaderSize(r, 64<<10)
	var line []byte
	long := false
	for {
		part, err := br.ReadSlice('\n')
		if len(line)+len(part) > maxEvent {
			line, long = line[:0], true
		} else if !long {
			line = append(line, part...)
		}
		if err == bufio.ErrBufferFull {
			continue
		}
		if len(bytes.TrimSpace(line)) > 0 {
			s.event(line)
		}
		line, long = line[:0], false
		if err != nil {
			return
		}
	}
}

// event remembers the session a status event names.
func (s *Service) event(b []byte) {
	var ev struct {
		Params struct {
			Terminal string `json:"terminal"`
			Type     string `json:"type"`
			Data     struct {
				Records map[string]struct {
					App string `json:"app"`
				} `json:"records"`
			} `json:"data"`
		} `json:"params"`
	}
	if json.Unmarshal(b, &ev) != nil || ev.Params.Type != "status" || len(ev.Params.Data.Records) != 1 {
		return
	}
	for id, rec := range ev.Params.Data.Records {
		s.mu.Lock()
		old, ok := s.sessions[ev.Params.Terminal]
		s.mu.Unlock()
		if ok && old.id == id && old.app == rec.App {
			continue
		}
		out, err := s.hcGet(ev.Params.Terminal)
		var t terminal
		if err != nil || json.Unmarshal(out, &t) != nil || t.Foreground == nil {
			continue
		}
		s.mu.Lock()
		if s.sessions == nil {
			s.sessions = map[string]seen{}
		}
		s.sessions[ev.Params.Terminal] = seen{app: rec.App, id: id, pid: t.Foreground.Pid}
		s.mu.Unlock()
	}
}
