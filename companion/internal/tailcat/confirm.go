package tailcat

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"sync"
	"time"

	"github.com/jt-wang/tailpocket/connecthost"
)

// A v2 window (TailPocket v0.3.0-pre.4 and later) lets a phone in only once this computer's user has seen its name
// and the four emoji it shows, and said yes: serve holds the question, and `pair` (y/N) or the Add phone dialog
// answers it over the control socket (control.go). A no, the 55 s running out (connecthost.ConfirmTimeout) or a
// phone that goes away ends the window: the code is spent.

// Window states, as Status reports them.
const (
	StateWaiting = "waiting" // open; no phone is asking now
	StateAsking  = "asking"  // a phone is asking: Request says which, Answer replies
	StatePaired  = "paired"  // a phone paired; Device is its name
	StateEnded   = "ended"   // closed without a phone: expired, refused, or the phone left
)

// keepEnded is how long a closed window's outcome stays readable for the pair that showed it.
const keepEnded = time.Minute

// Request is a phone asking to pair, as the computer's user is shown it.
type Request struct {
	Device string   `json:"device"`
	Emoji  []string `json:"emoji"`
	Route  string   `json:"route"`
}

// Status is a window as pair follows it.
type Status struct {
	State   string   `json:"state"`
	Request *Request `json:"request,omitempty"`
	Device  string   `json:"device,omitempty"`
}

// ErrNoWindow: no open or recent window has this id.
var ErrNoWindow = errors.New("no such pairing window")

// ErrNotAsking: no phone is waiting for an answer on this window.
var ErrNotAsking = errors.New("no phone is waiting for an answer on this pairing window")

type window struct {
	mu     sync.Mutex
	status Status
	answer chan bool // the asking phone's; nil when none asks
}

func newWindowID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// confirm is the window's connecthost PairOptions.Confirm: it shows the request and waits for Answer.
func (w *window) confirm(ctx context.Context, r connecthost.PairRequest) bool {
	answer := make(chan bool, 1)
	w.mu.Lock()
	if w.status.State != StateWaiting {
		w.mu.Unlock()
		return false
	}
	w.status = Status{State: StateAsking, Request: &Request{Device: r.Device, Emoji: r.Emoji, Route: r.Route}}
	w.answer = answer
	w.mu.Unlock()
	ok := false
	select {
	case ok = <-answer:
	case <-ctx.Done():
	}
	w.mu.Lock()
	if w.status.State == StateAsking {
		w.status = Status{State: StateWaiting}
	}
	w.answer = nil
	w.mu.Unlock()
	return ok
}

func (w *window) end(device string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if device != "" {
		w.status = Status{State: StatePaired, Device: device}
	} else {
		w.status = Status{State: StateEnded}
	}
	w.answer = nil
}

type windows struct {
	mu sync.Mutex
	m  map[string]*window
}

func (ws *windows) add() (string, *window) {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	if ws.m == nil {
		ws.m = map[string]*window{}
	}
	id := newWindowID()
	w := &window{status: Status{State: StateWaiting}}
	ws.m[id] = w
	return id, w
}

func (ws *windows) get(id string) *window {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	return ws.m[id]
}

func (ws *windows) forget(id string) {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	delete(ws.m, id)
}

// Status is window id's state.
func (s *Service) Status(id string) (Status, error) {
	w := s.windows.get(id)
	if w == nil {
		return Status{}, ErrNoWindow
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.status, nil
}

// Answer lets the phone asking on window id pair (approve) or ends the window.
func (s *Service) Answer(id string, approve bool) error {
	w := s.windows.get(id)
	if w == nil {
		return ErrNoWindow
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.status.State != StateAsking || w.answer == nil {
		return ErrNotAsking
	}
	w.answer <- approve
	w.answer = nil
	w.status = Status{State: StateWaiting} // until the window says paired or ended
	return nil
}
