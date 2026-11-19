package connectrelay

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// Config sets up a Server. Zero limits take the defaults.
type Config struct {
	Token            string // bearer token for /v1/host and /v1/accept
	Open             bool   // anyone may register a room; Token is not checked
	MaxRooms         int    // default 100
	MaxPhonesPerRoom int    // default 16
	MaxPhonesPerAddr int    // phones per room from one source address (AddrKey), default 4
	PhoneRate        int    // /v1/phone per source address in any 60 s, default 30
	HostRate         int    // /v1/host per source address in any 60 s, default 30
	TrustProxy       []netip.Prefix
	PublicHost       string        // required: the relay's host name as hosts dial it (NormalizeHost); never taken from a request
	HostGoneWait     time.Duration // after an accept connection ends, how long its phone waits to see the registration end too (then 4404, else 1001); default 1 s
	MaxConns         int           // TCP connections at once, in Listener; default 4096
	MaxConnsPerAddr  int           // TCP connections at once per source address (AddrKey), not for TrustProxy; default 64
	AcceptTimeout    time.Duration // default 10 s
	RegisterTimeout  time.Duration // default 10 s
	PingInterval     time.Duration // default 30 s
	StaleCheck       time.Duration // how long a registered host has to answer a ping when its room is claimed again, default 3 s
	Logger           *log.Logger   // default: discard
	Now              func() time.Time
}

const (
	DefaultMaxRooms         = 100
	DefaultMaxPhonesPerRoom = 16
	DefaultMaxPhonesPerAddr = 4
	DefaultPhoneRate        = 30
	DefaultHostRate         = 30
	DefaultAcceptTimeout    = 10 * time.Second
	DefaultPingInterval     = 30 * time.Second
	DefaultStaleCheck       = 3 * time.Second
	DefaultMaxConns         = 4096
	DefaultMaxConnsPerAddr  = 64
	DefaultHostGoneWait     = time.Second
)

// ReasonPeerGone is the close reason when the other end of a pair went
// away. The relay always closes with 1001 and this reason: close codes on a
// relay connection are not encrypted, so the relay never passes one on.
const ReasonPeerGone = "peer-gone"

// refusedLinesPerMinute bounds the refused lines written for one code.
const refusedLinesPerMinute = 10

// ErrNoToken: a relay with no token must be started open on purpose.
var ErrNoToken = errors.New("relay: no token and not open")

// ErrNoPublicHost: the relay must know its own name to check what a
// The host signed; the Host header is written by whoever dials, so it
// cannot stand in.
var ErrNoPublicHost = errors.New("relay: no public host name")

// Server is the relay. It is an http.Handler.
type Server struct {
	cfg       Config
	tokenH    [32]byte
	phoneRate *rateLimiter
	hostRate  *rateLimiter
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup

	logMu   sync.Mutex
	refused map[refusedKey]*logWindow

	connMu  sync.Mutex
	conns   int
	perAddr map[netip.Addr]int

	mu      sync.Mutex
	closed  bool
	rooms   map[string]*room
	pending int // host connections between upgrade and ready

	afterUpgrade func(path string) // tests only
}

type refusedKey struct {
	code int
	why  string
}

type logWindow struct {
	start   time.Time
	lines   int
	dropped int
}

type room struct {
	name   string
	host   *websocket.Conn
	phones int                // waiting plus paired, plus reserved during upgrade
	byAddr map[netip.Addr]int // the same, per AddrKey
	wait   map[string]*waiting
	gone   chan struct{} // closed when the host leaves
}

// waiting is one phone connection, from upgrade until it ends.
type waiting struct {
	claimed chan *websocket.Conn // exactly one send once taken: the accept connection, or nil if its upgrade failed
	taken   bool                 // an accept claimed it; guarded by Server.mu
}

// NewServer checks cfg and returns a relay.
func NewServer(cfg Config) (*Server, error) {
	if cfg.Token == "" && !cfg.Open {
		return nil, ErrNoToken
	}
	cfg.PublicHost = NormalizeHost(cfg.PublicHost)
	if cfg.PublicHost == "" {
		return nil, ErrNoPublicHost
	}
	def := func(v *int, d int) {
		if *v <= 0 {
			*v = d
		}
	}
	defD := func(v *time.Duration, d time.Duration) {
		if *v <= 0 {
			*v = d
		}
	}
	def(&cfg.MaxRooms, DefaultMaxRooms)
	def(&cfg.MaxPhonesPerRoom, DefaultMaxPhonesPerRoom)
	def(&cfg.MaxPhonesPerAddr, DefaultMaxPhonesPerAddr)
	def(&cfg.PhoneRate, DefaultPhoneRate)
	def(&cfg.HostRate, DefaultHostRate)
	defD(&cfg.AcceptTimeout, DefaultAcceptTimeout)
	defD(&cfg.RegisterTimeout, 10*time.Second)
	defD(&cfg.PingInterval, DefaultPingInterval)
	defD(&cfg.StaleCheck, DefaultStaleCheck)
	def(&cfg.MaxConns, DefaultMaxConns)
	def(&cfg.MaxConnsPerAddr, DefaultMaxConnsPerAddr)
	defD(&cfg.HostGoneWait, DefaultHostGoneWait)
	if cfg.Logger == nil {
		cfg.Logger = log.New(io.Discard, "", 0)
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Server{
		cfg:       cfg,
		tokenH:    sha256.Sum256([]byte(cfg.Token)),
		phoneRate: newRateLimiter(cfg.PhoneRate, time.Minute, cfg.Now),
		hostRate:  newRateLimiter(cfg.HostRate, time.Minute, cfg.Now),
		ctx:       ctx,
		cancel:    cancel,
		refused:   map[refusedKey]*logWindow{},
		perAddr:   map[netip.Addr]int{},
		rooms:     map[string]*room{},
	}, nil
}

// Close drops every connection and waits for the handlers to finish.
func (s *Server) Close() {
	s.cancel()
	s.mu.Lock()
	s.closed = true
	for _, r := range s.rooms {
		r.host.CloseNow()
	}
	s.mu.Unlock()
	s.wg.Wait()
}

// enter counts a handler for Close to wait on; false once closed.
func (s *Server) enter() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	s.wg.Add(1)
	return true
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != PathHealth {
		if !s.enter() {
			http.Error(w, "relay stopping", http.StatusServiceUnavailable)
			return
		}
		defer s.wg.Done()
	}
	// net/http does not close a hijacked connection when a handler panics,
	// so the connection would keep its Listener slot.
	own := &owned{}
	r = r.WithContext(context.WithValue(r.Context(), ownedKey{}, own))
	defer func() {
		if p := recover(); p != nil {
			own.closeAll()
			panic(p)
		}
	}()
	switch r.URL.Path {
	case PathHealth:
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		io.WriteString(w, "ok\n")
	case PathHost:
		s.serveHost(w, r)
	case PathPhone:
		s.servePhone(w, r)
	case PathAccept:
		s.serveAccept(w, r)
	default:
		http.NotFound(w, r)
	}
}

// event writes one log line. Only the first 6 characters of a room go in.
// code 0 and bytes < 0 leave those fields out; code -1 (no close frame) is
// written as 1006, the code WebSocket uses for that.
func (s *Server) event(name, roomName string, code int, bytes int64) {
	s.eventWhy(name, roomName, code, bytes, "")
}

// eventWhy is event with a why= field when why is not empty.
func (s *Server) eventWhy(name, roomName string, code int, bytes int64, why string) {
	short := "-"
	if ValidRoom(roomName) {
		short = roomName[:6]
	}
	line := s.cfg.Now().UTC().Format(time.RFC3339) + " " + name + " room=" + short
	if code == -1 {
		code = 1006
	}
	if code != 0 {
		line += fmt.Sprintf(" code=%d", code)
	}
	if bytes >= 0 {
		line += fmt.Sprintf(" bytes=%d", bytes)
	}
	if why != "" {
		line += " why=" + why
	}
	s.cfg.Logger.Print(line)
}

// refusedEvent logs a refusal, at most refusedLinesPerMinute lines per code
// per minute. The first refusal of the next minute first writes how many
// were left out.
func (s *Server) refusedEvent(roomName string, code int) { s.refusedWhy(roomName, code, "") }

// refusedWhy is refusedEvent with a reason; each code and reason has its
// own lines per minute.
func (s *Server) refusedWhy(roomName string, code int, why string) {
	now := s.cfg.Now()
	k := refusedKey{code, why}
	s.logMu.Lock()
	w := s.refused[k]
	if w == nil || now.Sub(w.start) >= time.Minute {
		if w != nil && w.dropped > 0 {
			line := fmt.Sprintf("%s refused-dropped code=%d count=%d", now.UTC().Format(time.RFC3339), code, w.dropped)
			if why != "" {
				line += " why=" + why
			}
			s.cfg.Logger.Print(line)
		}
		w = &logWindow{start: now}
		s.refused[k] = w
	}
	if w.lines >= refusedLinesPerMinute {
		w.dropped++
		s.logMu.Unlock()
		return
	}
	w.lines++
	s.logMu.Unlock()
	s.eventWhy("refused", roomName, code, -1, why)
}

func (s *Server) refuse(w http.ResponseWriter, roomName string, status int, msg string) {
	s.refusedEvent(roomName, status)
	http.Error(w, msg, status)
}

func (s *Server) authorized(r *http.Request) bool {
	if s.cfg.Open {
		return true
	}
	h := r.Header.Get("Authorization")
	const p = "Bearer "
	if len(h) < len(p) || !strings.EqualFold(h[:len(p)], p) {
		return false
	}
	// Hash both sides so the compare takes the same time whatever the length.
	got := sha256.Sum256([]byte(h[len(p):]))
	return subtle.ConstantTimeCompare(got[:], s.tokenH[:]) == 1
}

type ownedKey struct{}

// owned is the WebSocket connections one request's handler holds.
type owned struct {
	mu    sync.Mutex
	conns []*websocket.Conn
}

// hold records that r's handler now owns c, for ServeHTTP to close if it panics.
func hold(r *http.Request, c *websocket.Conn) {
	if o, ok := r.Context().Value(ownedKey{}).(*owned); ok {
		o.mu.Lock()
		o.conns = append(o.conns, c)
		o.mu.Unlock()
	}
}

func (o *owned) closeAll() {
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, c := range o.conns {
		c.CloseNow()
	}
}

func (s *Server) upgrade(w http.ResponseWriter, r *http.Request) (*websocket.Conn, error) {
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		// The token is a header a browser cannot set, so the origin check
		// protects nothing here.
		InsecureSkipVerify: true,
		CompressionMode:    websocket.CompressionDisabled,
	})
	if err != nil {
		return nil, err
	}
	// The pair copier counts message bytes itself (MaxMessage); this only
	// stops the library from reading far past that.
	c.SetReadLimit(MaxMessage + 1)
	hold(r, c)
	if s.afterUpgrade != nil {
		s.afterUpgrade(r.URL.Path)
	}
	return c, nil
}

func randomID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b[:])
}

func writeJSON(ctx context.Context, c *websocket.Conn, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return c.Write(ctx, websocket.MessageText, b)
}

// keepAlive pings c every interval until ctx ends; a missed pong closes c.
func (s *Server) keepAlive(ctx context.Context, c *websocket.Conn) {
	t := time.NewTicker(s.cfg.PingInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			pctx, cancel := context.WithTimeout(ctx, s.cfg.PingInterval)
			err := c.Ping(pctx)
			cancel()
			if err != nil {
				if ctx.Err() == nil {
					c.CloseNow()
				}
				return
			}
		}
	}
}

// serveHost: a the host registers a room and keeps this connection for
// incoming notices.
func (s *Server) serveHost(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("room")
	if r.Method != http.MethodGet {
		s.refuse(w, name, http.StatusMethodNotAllowed, "GET only")
		return
	}
	// Before the token check, so it also slows token guessing.
	if !s.hostRate.allow(ClientIP(r, s.cfg.TrustProxy)) {
		s.refuse(w, name, http.StatusTooManyRequests, "too many requests")
		return
	}
	if !s.authorized(r) {
		s.refuse(w, name, http.StatusUnauthorized, "bad or missing token")
		return
	}
	if !ValidRoom(name) {
		s.refuse(w, name, http.StatusBadRequest, "bad room")
		return
	}
	s.mu.Lock()
	if len(s.rooms)+s.pending >= s.cfg.MaxRooms {
		s.mu.Unlock()
		s.refuse(w, name, http.StatusServiceUnavailable, "relay full")
		return
	}
	s.pending++
	s.mu.Unlock()
	reserved := true
	defer func() {
		if reserved {
			s.mu.Lock()
			s.pending--
			s.mu.Unlock()
		}
	}()

	c, err := s.upgrade(w, r)
	if err != nil {
		return
	}
	defer c.CloseNow()

	ctx, cancel := context.WithCancel(s.ctx)
	defer cancel()
	nonce := randomID()
	rctx, rcancel := context.WithTimeout(ctx, s.cfg.RegisterTimeout)
	defer rcancel()
	if err := writeJSON(rctx, c, Challenge{Type: TypeChallenge, Nonce: nonce}); err != nil {
		return
	}
	typ, b, err := c.Read(rctx)
	if err != nil {
		if rctx.Err() != nil && ctx.Err() == nil {
			s.refusedEvent(name, int(websocket.StatusPolicyViolation))
			c.Close(websocket.StatusPolicyViolation, "no register")
		}
		return
	}
	rcancel()
	var reg Register
	ok := typ == websocket.MessageText && json.Unmarshal(b, &reg) == nil && reg.Type == TypeRegister
	var pub, sig []byte
	if ok {
		pub, err = base64.RawURLEncoding.DecodeString(reg.Pub)
		ok = err == nil && len(pub) == ed25519.PublicKeySize
	}
	if ok {
		sig, err = base64.RawURLEncoding.DecodeString(reg.Sig)
		ok = err == nil
	}
	relayHost := s.cfg.PublicHost // never r.Host: the dialler writes that
	if ok && NormalizeHost(reg.Host) != relayHost {
		// Signed for another relay (or for none): it may be passing on a
		// signature it got as that relay.
		s.refusedWhy(name, CloseBadRegister, "wrong-host")
		c.Close(CloseBadRegister, "wrong host")
		return
	}
	if !ok || RoomFor(pub) != name || !VerifyChallenge(pub, relayHost, name, nonce, sig) {
		s.refusedEvent(name, CloseBadRegister)
		c.Close(CloseBadRegister, "bad register")
		return
	}

	rm := &room{
		name:   name,
		host:   c,
		byAddr: map[netip.Addr]int{},
		wait:   map[string]*waiting{},
		gone:   make(chan struct{}),
	}
	if !s.claimRoom(ctx, rm) {
		s.refusedEvent(name, CloseRoomTaken)
		c.Close(CloseRoomTaken, "room taken")
		return
	}
	reserved = false // claimRoom moved the reservation into the room

	if err := writeJSON(ctx, c, Ready{Type: TypeReady}); err != nil {
		s.dropRoom(rm)
		return
	}
	s.event("host-up", name, 0, -1)
	go s.keepAlive(ctx, c)

	// Nothing is expected from the host; reading keeps pongs and the close
	// handshake flowing, and tells us when it goes.
	for {
		if _, _, err = c.Read(ctx); err != nil {
			break
		}
	}
	code := websocket.CloseStatus(err)
	s.dropRoom(rm)
	s.event("host-down", name, int(code), -1)
}

// claimRoom gives rm its room. If a host already holds it, that host gets
// StaleCheck to answer a ping: a live one keeps the room (false), a silent
// one, such as a laptop that changed networks without closing, is dropped.
// The new host has already proven it holds the room's key.
func (s *Server) claimRoom(ctx context.Context, rm *room) bool {
	checked := false
	for {
		s.mu.Lock()
		old := s.rooms[rm.name]
		if old == nil {
			s.rooms[rm.name] = rm
			s.pending--
			s.mu.Unlock()
			return true
		}
		s.mu.Unlock()
		if checked {
			return false
		}
		checked = true
		pctx, cancel := context.WithTimeout(ctx, s.cfg.StaleCheck)
		err := old.host.Ping(pctx)
		cancel()
		if err == nil {
			return false
		}
		old.host.CloseNow()
		s.dropRoom(old)
	}
}

// dropRoom frees the room at once and closes every phone in it.
func (s *Server) dropRoom(rm *room) {
	s.mu.Lock()
	if s.rooms[rm.name] == rm {
		delete(s.rooms, rm.name)
	}
	select {
	case <-rm.gone:
	default:
		close(rm.gone)
	}
	s.mu.Unlock()
}

// servePhone: a phone joins a room and waits for the the host to accept.
func (s *Server) servePhone(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("room")
	if r.Method != http.MethodGet {
		s.refuse(w, name, http.StatusMethodNotAllowed, "GET only")
		return
	}
	src := ClientIP(r, s.cfg.TrustProxy)
	if !s.phoneRate.allow(src) {
		s.refuse(w, name, http.StatusTooManyRequests, "too many requests")
		return
	}
	if !ValidRoom(name) {
		s.refuse(w, name, http.StatusBadRequest, "bad room")
		return
	}
	key := AddrKey(src)
	s.mu.Lock()
	rm := s.rooms[name]
	if rm != nil {
		if rm.phones >= s.cfg.MaxPhonesPerRoom || rm.byAddr[key] >= s.cfg.MaxPhonesPerAddr {
			s.mu.Unlock()
			s.refuse(w, name, http.StatusServiceUnavailable, "room full")
			return
		}
		rm.phones++
		rm.byAddr[key]++
	}
	s.mu.Unlock()
	release := func() {
		if rm == nil {
			return
		}
		s.mu.Lock()
		rm.phones--
		if rm.byAddr[key]--; rm.byAddr[key] <= 0 {
			delete(rm.byAddr, key)
		}
		s.mu.Unlock()
	}

	c, err := s.upgrade(w, r)
	if err != nil {
		release()
		return
	}
	defer c.CloseNow()
	if rm == nil {
		s.refusedEvent(name, CloseNoHost)
		c.Close(CloseNoHost, "no companion in this room")
		return
	}

	id := randomID()
	wt := &waiting{claimed: make(chan *websocket.Conn, 1)}
	s.mu.Lock()
	select {
	case <-rm.gone:
		s.mu.Unlock()
		release()
		s.refusedEvent(name, CloseNoHost)
		c.Close(CloseNoHost, "no companion in this room")
		return
	default:
	}
	rm.wait[id] = wt
	s.mu.Unlock()
	var acc *websocket.Conn
	received := false // took the one value wt.claimed gets
	defer func() {
		s.mu.Lock()
		delete(rm.wait, id)
		taken := wt.taken
		s.mu.Unlock()
		release()
		if taken && !received {
			// An accept claimed this phone but we left first; close what it brings.
			go func() {
				if a := <-wt.claimed; a != nil {
					a.CloseNow()
				}
			}()
		}
	}()

	ctx, cancel := context.WithCancel(s.ctx)
	defer cancel()
	s.event("phone-in", name, 0, -1)
	// One deadline for telling the host and for its accept.
	deadline := time.Now().Add(s.cfg.AcceptTimeout)
	nctx, ncancel := context.WithDeadline(ctx, deadline)
	err = writeJSON(nctx, rm.host, Incoming{Type: TypeIncoming, ID: id})
	ncancel()
	if err != nil {
		s.event("phone-out", name, int(websocket.StatusInternalError), 0)
		c.Close(websocket.StatusInternalError, "companion unreachable")
		return
	}

	timer := time.NewTimer(time.Until(deadline))
	defer timer.Stop()
	select {
	case acc = <-wt.claimed:
		received = true
	case <-timer.C:
		s.mu.Lock()
		taken := wt.taken
		delete(rm.wait, id)
		s.mu.Unlock()
		if !taken {
			s.event("phone-out", name, CloseAcceptTimeout, 0)
			c.Close(CloseAcceptTimeout, "companion did not accept")
			return
		}
		// Claimed just now; its upgrade is under way.
		acc = <-wt.claimed
		received = true
	case <-rm.gone:
		s.event("phone-out", name, CloseNoHost, 0)
		c.Close(CloseNoHost, "companion left")
		return
	case <-ctx.Done():
		return
	}
	if acc != nil {
		hold(r, acc) // the accept's handler has handed it over
	}
	if acc == nil {
		s.event("phone-out", name, int(websocket.StatusGoingAway), 0)
		c.Close(websocket.StatusGoingAway, ReasonPeerGone)
		return
	}
	defer acc.CloseNow()
	s.event("phone-paired", name, 0, -1)

	code, n := s.splice(ctx, rm, c, acc)
	s.event("phone-out", name, int(code), n)
}

// serveAccept: the the host takes the phone named by id.
func (s *Server) serveAccept(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	name, id := q.Get("room"), q.Get("id")
	if r.Method != http.MethodGet {
		s.refuse(w, name, http.StatusMethodNotAllowed, "GET only")
		return
	}
	if !s.authorized(r) {
		s.refuse(w, name, http.StatusUnauthorized, "bad or missing token")
		return
	}
	if !ValidRoom(name) || !ValidID(id) {
		s.refuse(w, name, http.StatusBadRequest, "bad room or id")
		return
	}
	s.mu.Lock()
	var wt *waiting
	if rm := s.rooms[name]; rm != nil {
		wt = rm.wait[id]
		if wt != nil {
			// Claimed: from here the phone handler, or its cleanup, takes
			// exactly one value from wt.claimed.
			delete(rm.wait, id)
			wt.taken = true
		}
	}
	s.mu.Unlock()
	if wt == nil {
		s.refuse(w, name, http.StatusNotFound, "no such phone")
		return
	}
	// Exactly one send on wt.claimed, also when the upgrade fails or this
	// handler panics: the phone handler is waiting for it.
	sent := false
	defer func() {
		if !sent {
			wt.claimed <- nil
		}
	}()
	c, err := s.upgrade(w, r)
	if err != nil {
		return
	}
	// The phone handler owns the connection from here.
	sent = true
	wt.claimed <- c
}

// pairEnd says how one direction of a pair stopped.
type pairEnd struct {
	conn    *websocket.Conn // the connection that failed or closed
	err     error
	tooBig  bool // conn sent a message over MaxMessage
	written bool // the error came from writing to conn
}

// splice copies messages both ways until one side ends, then closes the
// other side with 1001 "peer-gone". It returns the ending side's close code
// and the bytes copied.
func (s *Server) splice(ctx context.Context, rm *room, phone, acc *websocket.Conn) (websocket.StatusCode, int64) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go s.keepAlive(ctx, phone)
	go s.keepAlive(ctx, acc)

	var mu sync.Mutex
	var total int64
	ends := make(chan pairEnd, 2)
	pump := func(src, dst *websocket.Conn) {
		var buf bytes.Buffer
		for {
			typ, rd, err := src.Reader(ctx)
			if err != nil {
				ends <- pairEnd{conn: src, err: err}
				return
			}
			// Read the whole message before sending any of it, so an
			// over-size one reaches the other side not even in part.
			buf.Reset()
			n, err := buf.ReadFrom(io.LimitReader(rd, MaxMessage+1))
			if err != nil {
				tooBig := errors.Is(err, websocket.ErrMessageTooBig)
				ends <- pairEnd{conn: src, err: err, tooBig: tooBig}
				return
			}
			if n > MaxMessage {
				ends <- pairEnd{conn: src, tooBig: true}
				return
			}
			if err := dst.Write(ctx, typ, buf.Bytes()); err != nil {
				ends <- pairEnd{conn: dst, err: err, written: true}
				return
			}
			mu.Lock()
			total += n
			mu.Unlock()
			if buf.Cap() > 64<<10 {
				buf = bytes.Buffer{} // do not keep a big buffer on an idle pair
			}
		}
	}
	go pump(phone, acc)
	go pump(acc, phone)
	bytesSoFar := func() int64 {
		mu.Lock()
		defer mu.Unlock()
		return total
	}

	var end pairEnd
	select {
	case end = <-ends:
	case <-rm.gone:
		// Close before cancel: cancelling a read drops the socket at once,
		// and the phone would see 1006 instead of 4404.
		acc.CloseNow()
		phone.Close(CloseNoHost, "companion left")
		cancel()
		<-ends
		<-ends
		return CloseNoHost, bytesSoFar()
	case <-ctx.Done():
		return websocket.StatusGoingAway, bytesSoFar()
	}

	if end.conn == acc && !end.tooBig {
		// A the host shutting down may close the accept a moment before
		// its registration; then the phone should learn it left (4404).
		wait := time.NewTimer(s.cfg.HostGoneWait)
		select {
		case <-rm.gone:
			wait.Stop()
			acc.CloseNow()
			phone.Close(CloseNoHost, "companion left")
			cancel()
			<-ends
			return CloseNoHost, bytesSoFar()
		case <-wait.C:
		case <-ctx.Done():
			wait.Stop()
		}
	}
	other := acc
	if end.conn == acc {
		other = phone
	}
	code := websocket.CloseStatus(end.err)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		if end.tooBig {
			code = websocket.StatusMessageTooBig
			end.conn.Close(websocket.StatusMessageTooBig, "message too big")
		} else {
			end.conn.CloseNow()
		}
	}()
	go func() {
		defer wg.Done()
		other.Close(websocket.StatusGoingAway, ReasonPeerGone)
	}()
	wg.Wait()
	cancel()
	<-ends
	return code, bytesSoFar()
}

// ClientIP is the address limits count against: the TCP peer, or, when the
// peer is in trust, the rightmost X-Forwarded-For hop that is not.
func ClientIP(r *http.Request, trust []netip.Prefix) netip.Addr {
	ap, err := netip.ParseAddrPort(r.RemoteAddr)
	var peer netip.Addr
	if err == nil {
		peer = ap.Addr().Unmap()
	} else if a, err := netip.ParseAddr(r.RemoteAddr); err == nil {
		peer = a.Unmap()
	} else {
		return netip.Addr{}
	}
	trusted := func(a netip.Addr) bool {
		for _, p := range trust {
			if p.Contains(a) {
				return true
			}
		}
		return false
	}
	if !trusted(peer) {
		return peer
	}
	hops := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
	client := peer
	for i := len(hops) - 1; i >= 0; i-- {
		h := strings.TrimSpace(hops[i])
		a, err := netip.ParseAddr(h)
		if err != nil {
			ap, err2 := netip.ParseAddrPort(h)
			if err2 != nil {
				break
			}
			a = ap.Addr()
		}
		a = a.Unmap()
		client = a
		if !trusted(a) {
			break
		}
	}
	return client
}
