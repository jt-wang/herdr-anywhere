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
	"sync"
	"time"

	"github.com/jt-wang/tailpocket/connecthost"

	"herdr-anywhere/companion/internal/store"
)

// Prefix starts every Herdr invite, so the app's scanner takes ours and no
// other app's (Tailcat's own default is "tcc1:", connectinvite/invite.go:23).
const Prefix = "herdr1:"

// stateName holds the host's identity (its private keys) and the paired
// phones' Tailcat keys, next to state.json in the 0700 state dir.
const stateName = "tailcat.json"

// Window is one open pairing window as pair shows it.
type Window struct {
	Invite  string    `json:"invite"`
	Expires time.Time `json:"expires"`
}

// Service is serve's Tailcat side.
type Service struct {
	host  *connecthost.Host
	store *store.Store
	name  string
	log   *log.Logger
}

// Open restores the host from the state dir, or makes a new identity. Phones
// whose device was revoked while serve was not running are left out.
func Open(st *store.Store, name string, logger *log.Logger) (*Service, error) {
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
	h, err := connecthost.New(connecthost.Config{Name: name, Identity: saved.Identity, Allowed: allowed, Save: s.save})
	if err != nil {
		return nil, fmt.Errorf("%s: %w", s.statePath(), err)
	}
	s.host = h
	return s, nil
}

func (s *Service) statePath() string { return filepath.Join(s.store.Dir(), stateName) }

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
	return os.Rename(tmp.Name(), s.statePath())
}

// Start brings up the host's node when a phone is paired; with none it runs
// no node until the first pairing (connecthost/host.go:135-150).
func (s *Service) Start(ctx context.Context) error { return s.host.Start(ctx) }

// Listener yields connections from paired phones, and none from a phone revoked since (admit).
func (s *Service) Listener() net.Listener { return admit(s.host.Listener(), s.store, s.log) }

// Close stops the node, every pairing window and the listener.
func (s *Service) Close() error { return s.host.Close() }

// Pair opens a pairing window (Tailcat's default length) whose phone gets a
// device token from the single-use pairing code. The code is the same one
// the LAN link carries, so whichever path redeems it first pairs.
func (s *Service) Pair(ctx context.Context, code string) (Window, error) {
	iss := &issuer{store: s.store, code: code, name: s.name}
	p, err := s.host.Pair(ctx, connecthost.PairOptions{Payload: iss.payload})
	if err != nil {
		return Window{}, err
	}
	text, err := p.QR(Prefix)
	if err != nil {
		p.Close()
		return Window{}, err
	}
	go func() {
		phone, err := p.Wait(context.Background())
		if err != nil {
			iss.undo()
			return
		}
		s.log.Printf("paired %q through Tailcat", phone.Device)
	}()
	return Window{Invite: text, Expires: p.Expires()}, nil
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
