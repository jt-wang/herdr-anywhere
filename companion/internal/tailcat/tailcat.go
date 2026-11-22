// Package tailcat lets phones reach the companion through Tailcat Connect
// (github.com/jt-wang/tailpocket): serve keeps a connecthost
// Host, whose node only paired phones can reach, and serves the same HTTP
// handler on its listener. A phone pairs by scanning a QR invite that
// `herdr-anywhere pair` asks the running serve for (control.go); the pairing
// reply carries the phone's device token, so /hc checks it as on the LAN.
package tailcat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jt-wang/tailpocket/connecthost"

	"herdr-anywhere/companion/internal/store"
)

// Prefix started every Herdr invite before TailPocket v0.3, so the app's scanner takes ours and no other app's
// (Tailcat's own default is "tcc1:", connectinvite/invite.go:23). Prefix2 starts a v2 invite, the only kind serve
// shows now: every route, the computer's end-to-end key, and the emoji check (confirm.go). App versions before 0.3
// do not read it, and 0.3.0 (35) cannot pair with it: phone and computer are upgraded together.
const (
	Prefix  = "herdr1:"
	Prefix2 = "herdr2:"
)

// stateName holds the host's identity (its private keys) and the paired
// phones' Tailcat keys, next to state.json in the 0700 state dir.
const stateName = "tailcat.json"

// Window is one open pairing window as pair shows it.
type Window struct {
	Invite string `json:"invite"` // a v2 invite (Prefix2)
	// ID names the window for Status and Answer.
	ID      string    `json:"id,omitempty"`
	Expires time.Time `json:"expires"`
}

// Options are serve's routes, each end-to-end encrypted by TailPocket (Noise).
type Options struct {
	// Routes, when set, are exactly the routes the host runs (connecthost.Config.Routes): without "tailcat" it runs
	// no Tailcat node and pairs only through the other routes. Nil: Tailcat, the local network unless NoLAN or
	// TailcatOnly, and Relays. Routes takes no NoLAN or TailcatOnly.
	Routes []string
	// Relays are tailpocket-relay servers to register with.
	Relays []connecthost.Relay
	// Listen are local network addresses ("192.168.1.5:7682") phones reach directly. Empty: TailPocket's own
	// choice, every private address of this computer, unless NoLAN.
	Listen []string
	// NoLAN: no local network route at all.
	NoLAN bool
	// TailcatOnly: Tailcat alone, as before v0.3: no local network, no relay.
	TailcatOnly bool
}

// Service is serve's Tailcat side.
type Service struct {
	host  *connecthost.Host
	store *store.Store
	name  string
	log   *log.Logger

	windows windows

	mu   sync.Mutex
	last connecthost.State // as last saved
}

// Open restores the host from the state dir, or makes a new identity. Phones
// whose device was revoked while serve was not running are left out.
func Open(st *store.Store, name string, logger *log.Logger, opts Options) (*Service, error) {
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}
	s := &Service{store: st, name: name, log: logger}
	var saved connecthost.State
	b, err := os.ReadFile(s.statePath())
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return nil, err
	default:
		if err := json.Unmarshal(b, &saved); err != nil {
			return nil, fmt.Errorf("%s: %w", s.statePath(), err)
		}
	}
	keep, err := deviceKeys(st)
	if err != nil {
		return nil, err
	}
	var allowed []string
	for _, k := range saved.Allowed {
		if keep[k] {
			allowed = append(allowed, k)
		}
	}
	// Keys are the phones' end-to-end keys: without them, a phone that has its key would be turned away as unpaired
	// on the relay and the local network after a restart.
	// Before New: without Tailcat, New forgets the phones it holds no end-to-end key for and saves that (save).
	s.last = connecthost.State{Allowed: allowed, Keys: saved.Keys}
	h, err := connecthost.New(connecthost.Config{Name: name, Identity: saved.Identity, Allowed: allowed, Keys: saved.Keys,
		Save: s.save, Routes: opts.Routes, Relays: opts.Relays, Listen: opts.Listen, NoLAN: opts.NoLAN, TailcatOnly: opts.TailcatOnly})
	if err != nil {
		if errors.Is(err, connecthost.ErrInvalidRoutes) {
			return nil, err
		}
		return nil, fmt.Errorf("%s: %w", s.statePath(), err)
	}
	s.host = h
	return s, nil
}

func (s *Service) statePath() string { return filepath.Join(s.store.Dir(), stateName) }

// HasIdentity says whether dir holds a host identity (one is saved once a phone first pairs).
func HasIdentity(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, stateName))
	return err == nil
}

// RetiredName is the file Retire moves the identity to at t, in the same dir.
func RetiredName(t time.Time) string {
	return stateName + ".retired-" + t.UTC().Format("20060102T150405Z")
}

// Retire moves dir's identity aside to name, unread and with its 0600 mode, so the next Open makes a new one: new
// Tailcat, Noise and relay keys. Only with no serve running: a running one saves the identity it holds again.
func Retire(dir, name string) error {
	if name != filepath.Base(name) || !strings.HasPrefix(name, stateName+".retired-") {
		return fmt.Errorf("not a retired identity's name: %q", name)
	}
	return os.Rename(filepath.Join(dir, stateName), filepath.Join(dir, name))
}

// save writes the host's state through a temp file (created 0600) and a
// rename, as the store does. connecthost refuses a change it cannot save.
func (s *Service) save(st connecthost.State) error {
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.store.Dir(), stateName+".*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(append(b, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), s.statePath()); err != nil {
		return err
	}
	s.mu.Lock()
	s.last = st
	s.mu.Unlock()
	return nil
}

// Start brings up the host's node when a phone is paired; with none it runs
// no node until the first pairing (connecthost/host.go:135-150).
func (s *Service) Start(ctx context.Context) error { return s.host.Start(ctx) }

// Listener yields connections from paired phones, and none from a phone revoked since (admit).
func (s *Service) Listener() net.Listener { return admit(s.host.Listener(), s.store, s.log) }

// Unsynced counts paired phones without an end-to-end key: they run an app from before 0.3, and reach this
// computer only through Tailcat until they open the new one.
func (s *Service) Unsynced() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, k := range s.last.Allowed {
		if s.last.Keys[k] == "" {
			n++
		}
	}
	return n
}

// Close stops the node, every pairing window and the listener.
func (s *Service) Close() error { return s.host.Close() }

// Pair opens a pairing window (Tailcat's default length) whose phone gets a
// device token from the single-use pairing code, once this computer's user
// confirmed it (Answer). Only its v2 invite leaves serve: a v1 one would let a
// phone pair over Tailcat without the emoji.
func (s *Service) Pair(ctx context.Context, code string) (Window, error) {
	iss := &issuer{store: s.store, code: code, name: s.name}
	id, w := s.windows.add()
	p, err := s.host.Pair(ctx, connecthost.PairOptions{Payload: iss.payload, Confirm: w.confirm})
	if err != nil {
		s.windows.forget(id)
		return Window{}, err
	}
	text, err := p.QR2(Prefix2)
	if err != nil {
		p.Close()
		s.windows.forget(id)
		return Window{}, err
	}
	go func() {
		phone, err := p.Wait(context.Background())
		if err != nil {
			iss.undo()
			w.end("")
		} else {
			w.end(phone.Device)
			s.log.Printf("paired %q over %s", phone.Device, phone.Route)
		}
		time.AfterFunc(keepEnded, func() { s.windows.forget(id) })
	}()
	return Window{Invite: text, ID: id, Expires: p.Expires()}, nil
}

// Watch keeps the host's allowlist in step with the device list, so
// `herdr-anywhere revoke` (another process) also cuts the phone off Tailcat.
//
// Revoking restarts the host's node (connecthost/host.go:259-260),
// which drops every phone's Tailcat
// connections, not only the revoked one's: the others' WebSockets close and
// their apps reconnect on the next dial, to the same address. Pairing a new
// phone admits it without a restart (host.go:247-251).
//
// `hold` (server.Server.Revoking) says a revoked phone's socket is still
// closing; the phone stays on Tailcat until then, so the restart does not
// drop the close that tells it to pair again.
func (s *Service) Watch(ctx context.Context, every time.Duration, hold func() bool) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := prune(ctx, s.host, s.store, s.log, hold); err != nil && ctx.Err() == nil {
				s.log.Printf("tailcat: %v", err)
			}
		}
	}
}

type allowlist interface {
	Allowed() []string
	Revoke(context.Context, string) error
}

// prune revokes allowed phones that no longer have a device. The allowlist
// is read before the devices: a pairing stores its device before Tailcat
// admits the phone, so a phone pairing meanwhile is never taken for revoked.
func prune(ctx context.Context, h allowlist, st *store.Store, logger *log.Logger, hold func() bool) error {
	allowed := h.Allowed()
	if len(allowed) == 0 || (hold != nil && hold()) {
		return nil
	}
	keep, err := deviceKeys(st)
	if err != nil {
		return err
	}
	for _, k := range allowed {
		if keep[k] {
			continue
		}
		if err := h.Revoke(ctx, k); err != nil {
			return err
		}
		if logger != nil {
			logger.Printf("tailcat: revoked phone %s", k)
		}
	}
	return nil
}

func deviceKeys(st *store.Store) (map[string]bool, error) {
	ds, err := st.Devices()
	if err != nil {
		return nil, err
	}
	keys := map[string]bool{}
	for _, d := range ds {
		if d.TailcatKey != "" {
			keys[d.TailcatKey] = true
		}
	}
	return keys, nil
}

// issuer is one window's Payload: it redeems the pairing code for the phone
// that joins and answers what POST /api/pair answers.
type issuer struct {
	store *store.Store
	code  string
	name  string

	mu     sync.Mutex
	phone  string
	reply  string
	device string
}

func (i *issuer) payload(ph connecthost.Phone) (string, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	// Tailcat calls Payload again when admitting the phone failed and it
	// retries (connecthost/pairing.go:225-240): same phone, same token.
	if i.reply != "" {
		if ph.Key != i.phone {
			return "", errors.New("pairing already used by another phone")
		}
		return i.reply, nil
	}
	d, token, err := i.store.RedeemTailcat(i.code, ph.Device, ph.Key)
	if err != nil {
		return "", err
	}
	b, err := json.Marshal(map[string]string{"token": token, "device": d.ID, "name": i.name})
	if err != nil {
		return "", err
	}
	i.phone, i.reply, i.device = ph.Key, string(b), d.ID
	return i.reply, nil
}

// undo forgets the device a window issued when its phone never joined.
func (i *issuer) undo() {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.device != "" {
		_ = i.store.Revoke(i.device)
		i.device = ""
	}
}

// admit passes on the connections of phones whose device is still paired. A revoke is a change to the device list,
// made by another process (`herdr-anywhere revoke`); Tailcat drops the phone only later, when Watch prunes it, and
// until its node stops the phone can still open connections. /hc refuses its token, but /api/info needs none, so
// without this a revoked phone still reached serve for that while (measured over real relays: up to 424 ms after
// revoke while the 4401 closes, and again in the moment between writing tailcat.json and stopping the node;
// TestLiveTailcat). A connection whose phone Tailcat does not name is passed on: Tailcat admits only allowed phones,
// and a phone it has just admitted is named only once connecthost indexes it.
func admit(ln net.Listener, st *store.Store, logger *log.Logger) net.Listener {
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}
	return &admitted{Listener: ln, store: st, log: logger}
}

type admitted struct {
	net.Listener
	store *store.Store
	log   *log.Logger
}

func (l *admitted) Accept() (net.Conn, error) {
	for {
		c, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		phone := connecthost.PhoneOf(c)
		if phone == "" {
			return c, nil
		}
		keys, err := deviceKeys(l.store)
		if err == nil && keys[phone] {
			return c, nil
		}
		c.Close()
		if err != nil {
			l.log.Printf("tailcat: refused a connection: reading the devices: %v", err)
		} else {
			l.log.Printf("tailcat: refused a connection from revoked phone %s", phone)
		}
	}
}
