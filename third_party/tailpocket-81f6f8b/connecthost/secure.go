package connecthost

// The end-to-end encrypted routes (v0.3): tailpocket-relay and lan. A phone on them runs
// connectsecure's Noise handshake with the host's static key it pinned from the invite; the host
// knows it by its Noise public key, which State.Keys maps to the phone's "nodekey:…". Each stream of a
// session reaches the app as one Conn, through the same Listener as Tailcat. Opaque relays skip all of
// this and hand the relay's bytes to the app as they are.
//
// The control port serves phones on Tailcat: they register their Noise key there (proven by
// WireGuard, which is how phones paired before v0.3 get onto the new routes without a new QR) and
// read the routes.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"slices"
	"sync"
	"time"

	"tailscale.com/types/key"

	"github.com/jt-wang/tailpocket/connectinvite"
	"github.com/jt-wang/tailpocket/connectrelay"
	"github.com/jt-wang/tailpocket/connectsecure"
)

// Route names in Conn.Route and Event.Route.
const (
	RouteTailcat = connectinvite.RouteTailcat
	RouteRelay   = connectinvite.RouteRelay
	RouteLAN     = connectinvite.RouteLAN
)

// Version is the library version the host sends in its encrypted hello.
const Version = "0.3.0"

// maxSessionsPerPhone: a fifth session from one phone closes its oldest with bye busy.
const maxSessionsPerPhone = 4

// Event kinds.
const (
	EventPaired    = "paired"
	EventRevoked   = "revoked"
	EventConnOpen  = "conn-open"  // an encrypted session started
	EventConnClose = "conn-close" // it ended
	EventDenied    = "denied"     // a connection was turned away; Reason says why
)

// Event is one thing the host did, for Config.Audit.
type Event struct {
	Time   time.Time
	Kind   string
	Phone  string // "nodekey:…", "" when unknown
	Route  string
	Reason string
}

func (h *Host) record(e Event) {
	if h.audit == nil {
		return
	}
	if e.Time.IsZero() {
		e.Time = time.Now()
	}
	h.audit(e)
}

func (h *Host) authorized(phone, route string) bool {
	if h.authorize == nil {
		return true
	}
	if err := h.authorize(phone, route); err != nil {
		h.record(Event{Kind: EventDenied, Phone: phone, Route: route, Reason: "authorize"})
		return false
	}
	return true
}

type routeListener struct {
	route connectinvite.Route
	ln    net.Listener
	relay *connectrelay.HostListener // nil for lan
	// limit is this route's own handshake slots: a stranger filling one relay's (its /v1/phone takes no
	// token) leaves the others alone. perIP caps one source address's share on lan.
	limit *connectsecure.Limiter
	perIP *addrSlots
}

// Handshakes in progress at once: per route, and per source address on a lan route.
const (
	handshakesPerRoute = 16
	handshakesPerAddr  = 4
)

// addrSlots counts handshakes in progress per source address.
type addrSlots struct {
	mu sync.Mutex
	n  map[netip.Addr]int
}

func (a *addrSlots) acquire(c net.Conn) (release func(), ok bool) {
	ap, err := netip.ParseAddrPort(c.RemoteAddr().String())
	if err != nil {
		return nil, false
	}
	ip := ap.Addr().Unmap()
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.n[ip] >= handshakesPerAddr {
		return nil, false
	}
	a.n[ip]++
	var once sync.Once
	return func() {
		once.Do(func() {
			a.mu.Lock()
			if a.n[ip]--; a.n[ip] <= 0 {
				delete(a.n, ip)
			}
			a.mu.Unlock()
		})
	}, true
}

// Routes is how paired phones reach the host now; a v2 invite has these too.
func (h *Host) Routes() []connectinvite.Route {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.routesLocked()
}

// routesLocked: Tailcat while the main node runs, then the relay and lan routes. h.mu must be held.
func (h *Host) routesLocked() []connectinvite.Route {
	var out []connectinvite.Route
	if h.main != nil {
		out = append(out, connectinvite.Route{T: RouteTailcat, A: []string{h.main.Address()}, P: int(h.port)})
	}
	return append(out, h.extraRoutesLocked()...)
}

func (h *Host) extraRoutesLocked() []connectinvite.Route {
	room := connectrelay.RoomFor(h.id.relayPublic())
	var out []connectinvite.Route
	for _, r := range h.relays {
		rt := connectinvite.Route{T: RouteRelay, U: r.URL, R: room}
		if r.Opaque {
			rt.M = connectinvite.ModeOpaque
		}
		out = append(out, rt)
	}
	if len(h.listen) > 0 {
		out = append(out, connectinvite.Route{T: RouteLAN, A: slices.Clone(h.listen)})
	}
	return out
}

// RelayStatus reports each relay: whether the host holds its room, and why not.
func (h *Host) RelayStatus() map[string]error {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := map[string]error{}
	for _, l := range h.routeLns {
		if l.relay != nil {
			up, err := l.relay.Status()
			if up {
				err = nil
			} else if err == nil {
				err = errors.New("connectrelay: registering")
			}
			out[l.route.U] = err
		}
	}
	return out
}

// startRoutes registers with the relays and opens the lan listeners, once. h.op must be held.
func (h *Host) startRoutes() error {
	h.mu.Lock()
	started := h.routeLns != nil || (len(h.relays) == 0 && len(h.listen) == 0)
	relayKey := h.id.relay
	h.mu.Unlock()
	if started {
		return nil
	}
	var lns []routeListener
	fail := func(err error) error {
		for _, l := range lns {
			_ = l.ln.Close()
		}
		return err
	}
	for _, a := range h.listen {
		ln, err := net.Listen("tcp", a)
		if err != nil {
			return fail(err)
		}
		lns = append(lns, routeListener{route: connectinvite.Route{T: RouteLAN, A: []string{a}}, ln: ln,
			limit: connectsecure.NewLimiter(handshakesPerRoute), perIP: &addrSlots{n: map[netip.Addr]int{}}})
	}
	for _, r := range h.relays {
		rl, err := connectrelay.Listen(connectrelay.HostConfig{URL: r.URL, Token: r.Token, Key: relayKey})
		if err != nil {
			return fail(err)
		}
		rt := connectinvite.Route{T: RouteRelay, U: r.URL, R: rl.Room()}
		if r.Opaque {
			rt.M = connectinvite.ModeOpaque
		}
		lns = append(lns, routeListener{route: rt, ln: rl, relay: rl, limit: connectsecure.NewLimiter(handshakesPerRoute)})
	}
	h.mu.Lock()
	h.routeLns = lns
	h.mu.Unlock()
	for _, l := range lns {
		go h.acceptRoute(l)
	}
	return nil
}

func (h *Host) acceptRoute(l routeListener) {
	for {
		c, err := l.ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) || errors.Is(err, connectrelay.ErrClosed) {
				return
			}
			time.Sleep(100 * time.Millisecond) // a failing listener must not spin
			continue
		}
		if l.route.M == connectinvite.ModeOpaque {
			h.offerOpaque(c, l.route.T)
			continue
		}
		release := func() {}
		if l.perIP != nil {
			r, ok := l.perIP.acquire(c)
			if !ok {
				_ = c.Close()
				continue
			}
			release = r
		}
		go h.serveSecure(c, l.route.T, l.limit, release)
	}
}

func (h *Host) offerOpaque(c net.Conn, route string) {
	pc := &Conn{Conn: c, Route: route, host: h}
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		_ = c.Close()
		return
	}
	h.conns[pc] = struct{}{}
	h.mu.Unlock()
	go h.listener.offer(pc)
}

// serveSecure runs one encrypted session: handshake, admission, then every stream to the app.
// limit is the route's handshake slots; release frees the source address's once the handshake is over.
func (h *Host) serveSecure(c net.Conn, route string, limit *connectsecure.Limiter, release func()) {
	h.mu.Lock()
	id := connectsecure.Identity{Private: h.id.noise, Public: h.id.noisePublic()}
	h.mu.Unlock()
	s, err := connectsecure.Accept(c, connectsecure.ServerConfig{Identity: id, PSK: h.pskFor,
		Admit:   func(p connectsecure.Peer) (connectsecure.Admission, error) { return h.admit(p, route) },
		Version: Version, Limit: limit})
	release()
	if err != nil {
		return // a refusal was audited in admit, before its bye went out
	}
	phone := s.Device()
	h.mu.Lock()
	k, _ := parsePhone(phone)
	if _, ok := h.noise[k]; !ok || h.closed {
		h.mu.Unlock()
		_ = s.Revoked() // revoked during the handshake
		return
	}
	h.sessions[s] = phone
	oldest := h.excessSessionLocked(phone)
	routes := h.routesLocked()
	h.mu.Unlock()
	if oldest != nil {
		_ = oldest.Bye("busy")
	}
	h.record(Event{Kind: EventConnOpen, Phone: phone, Route: route})
	defer func() {
		h.mu.Lock()
		delete(h.sessions, s)
		h.mu.Unlock()
		h.record(Event{Kind: EventConnClose, Phone: phone, Route: route})
	}()
	_ = s.Control().Send(routesMsg{Type: "routes", Via: routes})
	go drainControl(s)
	for {
		st, err := s.AcceptStream()
		if err != nil {
			return
		}
		pc := &Conn{Conn: st, Phone: phone, Route: route, host: h}
		h.mu.Lock()
		_, ok := h.noise[k]
		if ok {
			h.conns[pc] = struct{}{}
		}
		h.mu.Unlock()
		if !ok {
			_ = st.Close()
			return
		}
		go h.listener.offer(pc)
	}
}

type routesMsg struct {
	Type string                `json:"type"`
	Via  []connectinvite.Route `json:"via"`
}

// drainControl reads what the phone sends on the control stream, so its queue never fills. Nothing
// a phone sends there needs an answer yet.
func drainControl(s *connectsecure.Session) {
	for {
		if _, err := s.Control().Recv(); err != nil {
			return
		}
	}
}

// excessSessionLocked returns phone's oldest session once it has more than maxSessionsPerPhone.
func (h *Host) excessSessionLocked(phone string) *connectsecure.Session {
	var mine []*connectsecure.Session
	for s, p := range h.sessions {
		if p == phone {
			mine = append(mine, s)
		}
	}
	if len(mine) <= maxSessionsPerPhone {
		return nil
	}
	slices.SortFunc(mine, func(a, b *connectsecure.Session) int { return a.Started().Compare(b.Started()) })
	delete(h.sessions, mine[0])
	return mine[0]
}

// pskFor is connectsecure's PSK: the window cid names, while it is open.
func (h *Host) pskFor(cid string) ([32]byte, bool) {
	h.mu.Lock()
	p := h.windows[cid]
	h.mu.Unlock()
	if p == nil || !time.Now().Before(p.expires) {
		return [32]byte{}, false
	}
	return connectsecure.PSKFor(p.code), true
}

// admit decides an encrypted session after its handshake (connectsecure.ServerConfig.Admit).
func (h *Host) admit(peer connectsecure.Peer, route string) (connectsecure.Admission, error) {
	if peer.Pair {
		return h.admitPairing(peer, route)
	}
	h.mu.Lock()
	phone := ""
	for k, pub := range h.noise {
		if pub == peer.Static {
			phone = k.String()
			break
		}
	}
	h.mu.Unlock()
	if phone == "" || (peer.Phone != "" && peer.Phone != phone) {
		h.record(Event{Kind: EventDenied, Route: route, Reason: "unpaired"})
		return connectsecure.Admission{Bye: "unpaired"}, nil
	}
	if !h.authorized(phone, route) {
		return connectsecure.Admission{Bye: "denied"}, nil
	}
	return connectsecure.Admission{OK: true, Device: phone}, nil
}

func (h *Host) admitPairing(peer connectsecure.Peer, route string) (connectsecure.Admission, error) {
	denied := connectsecure.Admission{Bye: "denied"}
	k, err := parsePhone(peer.Phone)
	h.mu.Lock()
	p := h.windows[peer.CID]
	known := err == nil && slices.Contains(h.allowed, k)
	pub, hasKey := h.noise[k]
	h.mu.Unlock()
	if p == nil || err != nil {
		h.record(Event{Kind: EventDenied, Route: route, Reason: "no-pairing-window"})
		return denied, nil
	}
	// The hello names the phone's nodekey; only its holder can sign this handshake with it.
	h.mu.Lock()
	own := h.id.noise
	h.mu.Unlock()
	if !connectsecure.CheckKeyProof(own[:], k.Raw32(), connectinvite.ProofPair, peer.Binding, peer.Proof) {
		h.record(Event{Kind: EventDenied, Phone: k.String(), Route: route, Reason: "nodekey-unproven"})
		return denied, nil
	}
	p.mu.Lock()
	rejoin := p.bound == k.String()
	p.mu.Unlock()
	// A phone already paired brings its nodekey only as a claim here; it keeps the Noise key it has,
	// and pairs over this route only with that key.
	if known && !rejoin && (!hasKey || pub != peer.Static) {
		h.record(Event{Kind: EventDenied, Phone: k.String(), Route: route, Reason: "paired-without-this-key"})
		return denied, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	static := peer.Static
	reply, status := p.join(ctx, k, peer.Device, route, &static)
	switch {
	case status == http.StatusServiceUnavailable:
		h.record(Event{Kind: EventDenied, Phone: k.String(), Route: route, Reason: "busy"})
		return connectsecure.Admission{Bye: "busy"}, nil
	case status != http.StatusOK:
		h.record(Event{Kind: EventDenied, Phone: k.String(), Route: route, Reason: "pairing-refused"})
		return denied, nil
	}
	h.mu.Lock()
	ok := h.noise[k] == peer.Static
	h.mu.Unlock()
	if !ok {
		return denied, nil
	}
	return connectsecure.Admission{OK: true, Device: k.String(),
		Paired: &connectsecure.Paired{Phone: k.String(), Name: reply.Name, Payload: reply.Payload}}, nil
}

// takeSessionsLocked unregisters the encrypted sessions of phones. h.mu must be held.
func (h *Host) takeSessionsLocked(phones map[string]bool) []*connectsecure.Session {
	var out []*connectsecure.Session
	for s, p := range h.sessions {
		if phones[p] {
			delete(h.sessions, s)
			out = append(out, s)
		}
	}
	return out
}

// revokeSessions tells each phone, encrypted, that it is no longer paired, then closes.
func revokeSessions(ss []*connectsecure.Session) {
	for _, s := range ss {
		_ = s.Revoked()
	}
}

// Control port (Tailcat main node, ControlPort).

type syncRequest struct {
	Noise string `json:"noise"`
	Proof string `json:"proof"` // KeyProof ProofNoise; without it only the host's key and routes come back
}

type syncReply struct {
	V   int                   `json:"v"`
	PK  string                `json:"pk"`
	Via []connectinvite.Route `json:"via"`
}

// serveControl answers POST /v2/sync from paired phones on Tailcat: it records the phone's Noise
// key (the connection proves the phone holds its nodekey) and returns the host's key and routes.
func (h *Host) serveControl() {
	srv := &http.Server{ReadHeaderTimeout: 5 * time.Second, Handler: http.HandlerFunc(h.handleControl),
		ConnContext: func(ctx context.Context, c net.Conn) context.Context {
			return context.WithValue(ctx, phoneCtxKey{}, PhoneOf(c))
		}}
	_ = srv.Serve(h.control)
}

type phoneCtxKey struct{}

func (h *Host) handleControl(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	phone, _ := r.Context().Value(phoneCtxKey{}).(string)
	k, err := parsePhone(phone)
	if err != nil {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	if r.URL.EscapedPath() != "/v2/sync" || r.URL.RawQuery != "" {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBody+1))
	if err != nil || len(body) > maxBody {
		w.WriteHeader(http.StatusRequestEntityTooLarge)
		return
	}
	var req syncRequest
	if json.Unmarshal(body, &req) != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if req.Noise != "" {
		nk, ok := parseNoiseKey(req.Noise)
		if !ok {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if !h.provesNoise(phone, req.Noise, nk, req.Proof) {
			h.record(Event{Kind: EventDenied, Phone: phone, Route: RouteTailcat, Reason: "noise-key-unproven"})
			w.WriteHeader(http.StatusForbidden)
			return
		}
		if err := h.setNoise(r.Context(), k, nk); errors.Is(err, errNoiseTaken) {
			w.WriteHeader(http.StatusConflict)
			return
		} else if err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
	}
	h.mu.Lock()
	reply := syncReply{V: 2, PK: encodeKey(h.id.noisePublic()), Via: h.routesLocked()}
	h.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(reply)
}

// setNoise records phone k's Noise key, saving first. A phone not (or no longer) paired is refused.
func (h *Host) setNoise(_ context.Context, k key.NodePublic, nk [32]byte) error {
	h.op.Lock()
	defer h.op.Unlock()
	h.mu.Lock()
	if !slices.Contains(h.allowed, k) {
		h.mu.Unlock()
		return errors.New("connecthost: not paired")
	}
	if cur, ok := h.noise[k]; ok && cur == nk {
		h.mu.Unlock()
		return nil
	}
	if h.noiseTakenLocked(k, nk) {
		h.mu.Unlock()
		return errNoiseTaken
	}
	next := make(map[key.NodePublic][32]byte, len(h.noise)+1)
	for a, b := range h.noise {
		next[a] = b
	}
	next[k] = nk
	st := h.stateLocked(h.id, h.allowed, next)
	h.mu.Unlock()
	if err := h.save(st); err != nil {
		return err
	}
	h.mu.Lock()
	h.noise, h.dirty = next, false
	var old []*connectsecure.Session // a session under another key is no longer this phone's
	for s, p := range h.sessions {
		if p == k.String() && s.RemoteStatic() != nk {
			delete(h.sessions, s)
			old = append(old, s)
		}
	}
	h.mu.Unlock()
	revokeSessions(old)
	return nil
}

// provesNoise checks that the phone reporting Noise key nk (text, as sent) holds it.
func (h *Host) provesNoise(phone, text string, nk [32]byte, proof string) bool {
	p, err := base64.RawURLEncoding.DecodeString(proof)
	if err != nil || len(p) == 0 {
		return false
	}
	h.mu.Lock()
	own := h.id.noise
	h.mu.Unlock()
	return connectsecure.CheckKeyProof(own[:], nk, connectinvite.ProofNoise, connectinvite.NoiseClaim(phone, text), p)
}

// errNoiseTaken: a phone reported a Noise key another phone holds. Only a handshake proves a phone
// holds its key; sync and Tailcat pairing take it on the phone's word, so a key may name one phone only.
var errNoiseTaken = errors.New("connecthost: another phone holds this Noise key")

// noiseTakenLocked reports whether a phone other than k holds nk. h.mu must be held.
func (h *Host) noiseTakenLocked(k key.NodePublic, nk [32]byte) bool {
	for p, pub := range h.noise {
		if p != k && pub == nk {
			return true
		}
	}
	return false
}

func encodeKey(k [32]byte) string { return base64.RawURLEncoding.EncodeToString(k[:]) }

func parseNoiseKey(s string) ([32]byte, bool) {
	var k [32]byte
	if !connectinvite.ValidKey(s) {
		return k, false
	}
	b, _ := base64.RawURLEncoding.DecodeString(s)
	copy(k[:], b)
	return k, k != [32]byte{}
}

func validListen(a string) bool {
	host, port, err := net.SplitHostPort(a)
	if err != nil || host == "" || port == "" || port == "0" {
		return false
	}
	ip := net.ParseIP(host)
	return ip == nil || !ip.IsUnspecified()
}
