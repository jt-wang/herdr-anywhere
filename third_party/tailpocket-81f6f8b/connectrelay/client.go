package connectrelay

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// HostConfig is the computer's side of one relay.
type HostConfig struct {
	URL   string             // the relay, "wss://relay.example.com" ("ws://" only for tests and loopback)
	Token string             // the relay's token
	Key   ed25519.PrivateKey // the computer's relay key; its public half names the room
	// HTTPClient dials the relay; nil means http.DefaultClient. Tests set one that trusts their server.
	HTTPClient *http.Client
	// Logf gets one line per registration change; nil discards them. It never gets tokens or keys.
	Logf func(format string, args ...any)
}

// ErrClosed is returned by Accept after Close.
var ErrClosed = errors.New("connectrelay: closed")

// Refusals that registering again will not fix. After one of these the listener stops and Status
// keeps the error: the relay's token, the computer's relay key or the relay's --public-host must
// change first.
var (
	ErrBadToken     = errors.New("connectrelay: bad-token: the relay refused the token")
	ErrRelayRefused = errors.New("connectrelay: relay-refused: the relay refused the registration (wrong relay key, or its --public-host is not the name in this URL)")
	ErrRoomTaken    = errors.New("connectrelay: room-taken: another live computer holds this room")
)

func final(err error) bool {
	return errors.Is(err, ErrBadToken) || errors.Is(err, ErrRelayRefused) || errors.Is(err, ErrRoomTaken)
}

// HostListener keeps the computer registered in its room and hands over each phone that joins as a
// net.Conn of raw bytes. It registers again, with backoff, whenever the relay goes away.
type HostListener struct {
	cfg    HostConfig
	room   string
	ctx    context.Context
	cancel context.CancelFunc
	conns  chan net.Conn

	mu  sync.Mutex
	up  bool
	err error
}

// Listen starts registering in the background and returns at once.
func Listen(cfg HostConfig) (*HostListener, error) {
	if len(cfg.Key) != ed25519.PrivateKeySize {
		return nil, errors.New("connectrelay: no relay key")
	}
	if _, err := wsURL(cfg.URL, ""); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	l := &HostListener{cfg: cfg, room: RoomFor(cfg.Key.Public().(ed25519.PublicKey)), ctx: ctx, cancel: cancel,
		conns: make(chan net.Conn)}
	go l.run()
	return l, nil
}

// Room is the room phones join.
func (l *HostListener) Room() string { return l.room }

// Status reports whether the computer holds its room now, and the last error when it does not.
func (l *HostListener) Status() (up bool, err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.up, l.err
}

func (l *HostListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.conns:
		return c, nil
	case <-l.ctx.Done():
		return nil, ErrClosed
	}
}

func (l *HostListener) Close() error { l.cancel(); return nil }

func (l *HostListener) Addr() net.Addr { return relayAddr(l.cfg.URL) }

func (l *HostListener) logf(format string, args ...any) {
	if l.cfg.Logf != nil {
		l.cfg.Logf(format, args...)
	}
}

func (l *HostListener) set(up bool, err error) {
	l.mu.Lock()
	l.up, l.err = up, err
	l.mu.Unlock()
}

func (l *HostListener) run() {
	wait := time.Second
	for l.ctx.Err() == nil {
		start := time.Now()
		err := l.register()
		l.set(false, err)
		if l.ctx.Err() != nil {
			return
		}
		l.logf("connectrelay: registration ended: %v", err)
		if final(err) {
			return
		}
		if time.Since(start) > time.Minute {
			wait = time.Second // it was up a while: this is a new outage
		}
		select {
		case <-time.After(wait):
		case <-l.ctx.Done():
			return
		}
		wait = min(wait*2, 30*time.Second)
	}
}

func (l *HostListener) auth() http.Header {
	return http.Header{"Authorization": {"Bearer " + l.cfg.Token}}
}

// register holds the room until the registration connection ends.
func (l *HostListener) register() error {
	u, _ := wsURL(l.cfg.URL, PathHost+"?room="+l.room)
	dctx, cancel := context.WithTimeout(l.ctx, 15*time.Second)
	c, resp, err := websocket.Dial(dctx, u, dialOptions(l.auth(), l.cfg.HTTPClient))
	cancel()
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusUnauthorized {
			return ErrBadToken
		}
		if resp != nil {
			return fmt.Errorf("connectrelay: %s answered %d", NormalizeHost(l.cfg.URL), resp.StatusCode)
		}
		return fmt.Errorf("connectrelay: %w", err)
	}
	defer c.CloseNow()
	rctx, cancel := context.WithTimeout(l.ctx, 15*time.Second)
	defer cancel()
	var ch Challenge
	if err := readJSON(rctx, c, &ch); err != nil || ch.Type != TypeChallenge {
		return fmt.Errorf("connectrelay: no challenge: %v", err)
	}
	pub := l.cfg.Key.Public().(ed25519.PublicKey)
	host := NormalizeHost(l.cfg.URL)
	reg := Register{Type: TypeRegister, Pub: base64.RawURLEncoding.EncodeToString(pub), Host: host,
		Sig: base64.RawURLEncoding.EncodeToString(SignChallenge(l.cfg.Key, host, l.room, ch.Nonce))}
	if err := writeJSON(rctx, c, reg); err != nil {
		return err
	}
	var ready Ready
	if err := readJSON(rctx, c, &ready); err != nil || ready.Type != TypeReady {
		switch websocket.CloseStatus(err) {
		case CloseBadRegister:
			return ErrRelayRefused
		case CloseRoomTaken:
			return ErrRoomTaken
		}
		return fmt.Errorf("connectrelay: registration refused: %v", err)
	}
	cancel()
	l.set(true, nil)
	l.logf("connectrelay: registered at %s", host)
	for {
		var in Incoming
		if err := readJSON(l.ctx, c, &in); err != nil {
			return err
		}
		if in.Type != TypeIncoming || !ValidID(in.ID) {
			continue
		}
		go l.accept(in.ID)
	}
}

func (l *HostListener) accept(id string) {
	u, _ := wsURL(l.cfg.URL, PathAccept+"?room="+l.room+"&id="+id)
	dctx, cancel := context.WithTimeout(l.ctx, 10*time.Second)
	c, _, err := websocket.Dial(dctx, u, dialOptions(l.auth(), l.cfg.HTTPClient))
	cancel()
	if err != nil {
		return
	}
	nc := netConn(c, l.cfg.URL)
	select {
	case l.conns <- nc:
	case <-l.ctx.Done():
		nc.Close()
	}
}

// DialPhone joins room on the relay at relayURL and returns the raw byte stream to the computer.
// The relay does not authenticate anyone here: the bytes must be encrypted and authenticated end to
// end, by connectsecure or by the app's own TLS.
func DialPhone(ctx context.Context, relayURL, room string, client *http.Client) (net.Conn, error) {
	if !ValidRoom(room) {
		return nil, errors.New("connectrelay: invalid room")
	}
	u, err := wsURL(relayURL, PathPhone+"?room="+room)
	if err != nil {
		return nil, err
	}
	c, resp, err := websocket.Dial(ctx, u, dialOptions(nil, client))
	if err != nil {
		if resp != nil {
			return nil, fmt.Errorf("%w: %s answered %d", ErrUnreachable, NormalizeHost(relayURL), resp.StatusCode)
		}
		return nil, fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	return netConn(c, relayURL), nil
}

// ErrUnreachable wraps every failure DialPhone and a phone's relay connection report, close codes
// included: they are not encrypted, so anyone on the path can send one, and none of them means the
// phone was unpaired. Only the encrypted bye says that.
var ErrUnreachable = errors.New("connectrelay: unreachable")

// ValidURL reports whether s is a relay URL the clients accept: wss://, or ws:// to a loopback host.
func ValidURL(s string) bool { _, err := wsURL(s, ""); return err == nil }

func wsURL(base, path string) (string, error) {
	u, err := url.Parse(strings.TrimRight(base, "/"))
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return "", errors.New("connectrelay: invalid relay URL")
	}
	switch u.Scheme {
	case "wss":
	case "ws":
		if ip := net.ParseIP(u.Hostname()); u.Hostname() != "localhost" && (ip == nil || !ip.IsLoopback()) {
			return "", errors.New("connectrelay: ws:// only to a loopback address; use wss://")
		}
	default:
		return "", errors.New("connectrelay: relay URL must start with wss://")
	}
	return u.Scheme + "://" + u.Host + path, nil
}

// netConn is a WebSocket as a byte stream; it lives until either end closes it.
func netConn(c *websocket.Conn, relayURL string) net.Conn {
	c.SetReadLimit(MaxMessage)
	return &wsConn{Conn: websocket.NetConn(context.Background(), c, websocket.MessageBinary), addr: relayAddr(relayURL)}
}

type wsConn struct {
	net.Conn
	addr net.Addr
}

func (c *wsConn) RemoteAddr() net.Addr { return c.addr }

// Read reports a WebSocket close as ErrUnreachable, so a relay's close code never reads as
// anything about the phone. A clean close by the peer is still io.EOF.
func (c *wsConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	if err != nil && err != io.EOF && websocket.CloseStatus(err) != -1 {
		err = fmt.Errorf("%w: closed with %d", ErrUnreachable, websocket.CloseStatus(err))
	}
	return n, err
}

// Write sends at most MaxMessage bytes per WebSocket message, the most the relay passes on.
func (c *wsConn) Write(p []byte) (int, error) {
	n := 0
	for len(p) > 0 {
		k := min(len(p), MaxMessage)
		w, err := c.Conn.Write(p[:k])
		n += w
		if err != nil {
			return n, err
		}
		p = p[k:]
	}
	return n, nil
}

type relayAddr string

func (a relayAddr) Network() string { return "relay" }
func (a relayAddr) String() string  { return NormalizeHost(string(a)) }

func readJSON(ctx context.Context, c *websocket.Conn, v any) error {
	typ, b, err := c.Read(ctx)
	if err != nil {
		return err
	}
	if typ != websocket.MessageText {
		return errors.New("connectrelay: expected a text message")
	}
	return json.Unmarshal(b, v)
}
