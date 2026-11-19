package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/netip"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/jt-wang/tailpocket/connecthost"
	"github.com/jt-wang/tailpocket/connectinvite"

	"herdr-anywhere/companion/internal/lan"
	"herdr-anywhere/companion/internal/store"
)

// secureLANPort is where serve --lan takes end-to-end encrypted connections from phones on the same network
// (TailPocket's lan route). 7681 stays the plain HTTP port, for loopback, --listen, and while it lasts the plain LAN
// port of phones paired before 0.3.
const secureLANPort = 7682

// plainCheck is how often serve looks whether a phone still needs the plain LAN port.
var plainCheck = 5 * time.Second

// plainLAN is the plain HTTP port on the LAN that phones paired over it before 0.3 still use: open only while one of
// them is paired, and never past lan.PlainUntil. It takes no new pairings.
type plainLAN struct {
	st    *store.Store
	addrs []string

	mu     sync.Mutex
	until  time.Time // zero: closed
	srv    *http.Server
	closed chan struct{}
}

// plainPhones counts the paired devices without a Tailcat key: they paired over plain HTTP and can reach this
// computer no other way.
func plainPhones(st *store.Store) (int, error) {
	ds, err := st.Devices()
	if err != nil {
		return 0, err
	}
	n := 0
	for _, d := range ds {
		if d.TailcatKey == "" {
			n++
		}
	}
	return n, nil
}

// openPlainLAN decides whether this serve keeps the plain LAN port, and where: serve's port on each LAN address, as
// before 0.3 (addrs are serve's own --listen addresses, left out).
func openPlainLAN(st *store.Store, withLAN bool, listen []string, lanAddrs []netip.Addr, addrs []string, now time.Time) (*plainLAN, error) {
	n, err := plainPhones(st)
	if err != nil {
		return nil, err
	}
	until, err := lan.PlainUntil(st.Dir(), n, now)
	if err != nil {
		return nil, err
	}
	p := &plainLAN{st: st}
	if !withLAN || n == 0 || !now.Before(until) {
		return p, nil
	}
	all, err := listenAddrs(listen, true, lanAddrs)
	if err != nil {
		return nil, err
	}
	for _, a := range all {
		if !slices.Contains(addrs, a) {
			p.addrs = append(p.addrs, a)
		}
	}
	if len(p.addrs) > 0 {
		p.until = until
	}
	return p, nil
}

// Until is when the plain LAN port closes, or zero when it is closed (/api/info's plainUntil).
func (p *plainLAN) Until() time.Time {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.until
}

// serve listens on the plain LAN addresses with handler, minus pairing, and closes them for good at the deadline or
// once no phone needs them.
func (p *plainLAN) serve(ctx context.Context, handler http.Handler, stdout io.Writer, logger *log.Logger) error {
	if p.Until().IsZero() {
		return nil
	}
	var lns []net.Listener
	for _, a := range p.addrs {
		ln, err := net.Listen("tcp", a)
		if err != nil {
			for _, l := range lns {
				l.Close()
			}
			return err
		}
		lns = append(lns, ln)
	}
	p.srv = &http.Server{Handler: plainOnly(p.st, handler), ReadHeaderTimeout: 10 * time.Second, ErrorLog: logger}
	p.closed = make(chan struct{})
	n, _ := plainPhones(p.st)
	for _, ln := range lns {
		fmt.Fprintf(stdout, "listening on http://%s, NOT encrypted, for %d phone(s) paired over the local network before 0.3, until %s\n",
			ln.Addr(), n, p.until.Local().Format("Mon 15:04"))
		go func() { _ = p.srv.Serve(ln) }()
	}
	logger.Printf("phones paired over the local network before 0.3 must pair again by %s: run herdr-anywhere pair and scan the code with Herdr Anywhere 0.3 or later",
		p.until.Local().Format("Mon Jan 2 15:04"))
	go p.watch(ctx, p.closed, logger)
	return nil
}

func (p *plainLAN) watch(ctx context.Context, stopped <-chan struct{}, logger *log.Logger) {
	deadline := time.NewTimer(time.Until(p.Until()))
	defer deadline.Stop()
	tick := time.NewTicker(plainCheck)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-stopped:
			return
		case <-deadline.C:
			p.shut(logger, "its 3 days are over")
			return
		case <-tick.C:
			if n, err := plainPhones(p.st); err == nil && n == 0 {
				p.shut(logger, "no phone paired before 0.3 needs it")
				return
			}
		}
	}
}

// shut closes the plain LAN port and records that it stays closed.
func (p *plainLAN) shut(logger *log.Logger, why string) {
	p.mu.Lock()
	if p.until.IsZero() {
		p.mu.Unlock()
		return
	}
	p.until = time.Time{}
	srv := p.srv
	p.mu.Unlock()
	if err := lan.ClosePlain(p.st.Dir(), time.Now()); err != nil {
		logger.Printf("could not record that the plain LAN port closed: %v", err)
	}
	_ = srv.Close() // also the WebSockets of phones still on it
	logger.Printf("closed the unencrypted LAN port: %s", why)
}

// close stops the plain LAN port for this serve only; a later serve opens it again until its deadline.
func (p *plainLAN) close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.srv != nil && p.closed != nil {
		close(p.closed)
		p.closed = nil
		_ = p.srv.Close()
	}
}

// plainOnly is the plain LAN port's handler: no pairing (from 0.3 a phone pairs only end-to-end
// encrypted), and no device that has a Tailcat key. Such a device reaches this computer through
// TailPocket and must never be served in plain HTTP, even with its own token: whoever on the network
// blocks the encrypted routes must not push it down here (PROTOCOL.md, "Phones paired before v0.3").
func plainOnly(st *store.Store, h http.Handler) http.Handler {
	refuse := func(w http.ResponseWriter, msg string) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"` + msg + `"}` + "\n"))
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/pair" || strings.HasPrefix(r.URL.Path, "/api/pair/") {
			refuse(w, "pairing over the unencrypted local network has ended; run herdr-anywhere pair and scan its code with the app")
			return
		}
		for _, token := range []string{strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), r.URL.Query().Get("token")} {
			if token == "" {
				continue
			}
			if d, err := st.Authenticate(token); err == nil && d.TailcatKey != "" {
				refuse(w, "this phone is paired end-to-end encrypted and is not served on the unencrypted local network")
				return
			}
		}
		h.ServeHTTP(w, r)
	})
}

// relayOption is --relay with the token from --relay-token-file.
func relayOption(url, tokenFile string, withTailcat bool) ([]connecthost.Relay, error) {
	if url == "" {
		if tokenFile != "" {
			return nil, errors.New("--relay-token-file needs --relay")
		}
		return nil, nil
	}
	if !withTailcat {
		return nil, errors.New("--relay needs --tailcat")
	}
	if !connectinvite.ValidRelayURL(url) {
		return nil, fmt.Errorf("--relay %q: want wss://host (ws:// only to a loopback address)", url)
	}
	if tokenFile == "" {
		return nil, errors.New("--relay needs --relay-token-file")
	}
	b, err := os.ReadFile(tokenFile)
	if err != nil {
		return nil, fmt.Errorf("--relay-token-file: %w", err)
	}
	token := strings.TrimSpace(string(b))
	if token == "" {
		return nil, errors.New("--relay-token-file is empty")
	}
	return []connecthost.Relay{{URL: url, Token: token}}, nil
}
