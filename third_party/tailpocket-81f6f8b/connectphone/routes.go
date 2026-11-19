package connectphone

// Routes (v0.3). A Session reaches its host over Tailcat, tailpocket-relay or lan, whichever answers,
// trying the one that worked last first. relay and lan run connectsecure's Noise session to the
// host key the Link pins; an opaque relay hands the app the relay's bytes and leaves encryption to it.
//
// The phone's Noise key is derived from its Tailcat key, so a Link needs no second secret and a link
// paired before v0.3 already has one: Session.Sync registers it with the host over Tailcat.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/flynn/noise"
	"github.com/tailscale/tailcat"
	"golang.org/x/crypto/curve25519"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/types/key"

	"github.com/jt-wang/tailpocket/connectinvite"
	"github.com/jt-wang/tailpocket/connectrelay"
	"github.com/jt-wang/tailpocket/connectsecure"
)

// Version is the library version the phone sends in its encrypted hello.
const Version = "0.3.0"

// ErrRevoked: the host said, encrypted, that this phone is no longer paired. Pair again.
var ErrRevoked = errors.New("connectphone: the host no longer has this phone paired")

// perRoute bounds one route's attempt while others are left to try; lanDial bounds each lan address.
const (
	perRoute = 10 * time.Second
	lanDial  = 3 * time.Second
)

// noiseKeys is the phone's Noise static key pair, derived from its Tailcat key with a label so the two
// protocols never share a key.
func noiseKeys(k key.NodePrivate) noise.DHKey {
	raw := k.Raw32()
	priv := sha256.Sum256(append([]byte("tailpocket2 noise key\n"), raw[:]...))
	pub, _ := curve25519.X25519(priv[:], curve25519.Basepoint)
	return noise.DHKey{Private: priv[:], Public: pub}
}

func noisePublic(k key.NodePrivate) string {
	return base64.RawURLEncoding.EncodeToString(noiseKeys(k).Public)
}

// noiseProof proves to the host with Noise key hostPK that this phone holds its Noise key.
func noiseProof(k key.NodePrivate, hostPK string) (string, error) {
	server, ok := decodeKey(hostPK)
	if !ok {
		return "", errors.New("connectphone: invalid host key")
	}
	p, err := connectsecure.KeyProof(noiseKeys(k).Private, server, connectinvite.ProofNoise,
		connectinvite.NoiseClaim(k.Public().String(), noisePublic(k)))
	return base64.RawURLEncoding.EncodeToString(p), err
}

// nodekeyProof signs a pairing handshake with the phone's nodekey (ProofPair).
func nodekeyProof(k key.NodePrivate, server [32]byte) func([]byte) ([]byte, error) {
	return func(binding []byte) ([]byte, error) {
		raw := k.Raw32()
		return connectsecure.KeyProof(raw[:], server, connectinvite.ProofPair, binding)
	}
}

// setRoutes takes the routes a host sent: its Tailcat address, and the others for Via.
func (l *Link) setRoutes(via []connectinvite.Route) {
	l.Via = nil
	for _, r := range via {
		if r.T == connectinvite.RouteTailcat {
			if len(r.A) == 1 && connectinvite.ValidAddress(r.A[0]) {
				l.Address, l.Port = r.A[0], r.P
			}
			continue
		}
		l.Via = append(l.Via, r)
	}
}

// Session reaches the host of one Link. It is safe for concurrent use.
type Session struct {
	opts    Options
	private key.NodePrivate
	client  *tailcat.Client // nil while the Link has no Tailcat address

	secMu sync.Mutex // one encrypted session is set up at a time

	mu      sync.Mutex
	link    Link
	last    string // the route that worked last (routeKey)
	sec     *connectsecure.Session
	secKey  string
	revoked bool
}

// Connect prepares a session; nothing goes over the network until the first Dial.
func Connect(link Link, opts Options) (*Session, error) {
	if err := link.validate(); err != nil {
		return nil, err
	}
	private, _ := parsePrivate(link.Key)
	s := &Session{link: link, opts: opts, private: private}
	s.setClientLocked()
	return s, nil
}

func (s *Session) setClientLocked() {
	if s.client == nil && s.link.Address != "" {
		s.client = &tailcat.Client{Server: tailcat.Addr(s.link.Address), Key: s.private,
			Logf: func(string, ...any) {}, DERPMapURL: s.opts.DERPMapURL}
	}
}

// Link is the session's Link with the routes the host sent since Connect; store it again when
// Options.OnLink reports a change.
func (s *Session) Link() Link {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.link
}

func routeKey(r connectinvite.Route) string {
	switch r.T {
	case connectinvite.RouteTailcat:
		return r.T
	case connectinvite.RouteRelay:
		return r.T + " " + r.U
	}
	return r.T + " " + strings.Join(r.A, ",")
}

// attemptsLocked lists what Dial tries, in order.
func (s *Session) attemptsLocked() []connectinvite.Route {
	var all []connectinvite.Route
	if s.link.Address != "" {
		all = append(all, connectinvite.Route{T: connectinvite.RouteTailcat})
	}
	for _, r := range s.link.Via {
		switch {
		case r.T == connectinvite.RouteRelay && r.M == connectinvite.ModeOpaque:
		case (r.T == connectinvite.RouteRelay || r.T == connectinvite.RouteLAN) && s.link.HostKey != "":
		default:
			continue // a route this library does not know, or an encrypted one without the host's key
		}
		all = append(all, r)
	}
	for i, r := range all {
		if routeKey(r) == s.last && i > 0 {
			return append([]connectinvite.Route{r}, append(all[:i:i], all[i+1:]...)...)
		}
	}
	return all
}

// Dial opens one connection to the host's app, over the first route that answers.
func (s *Session) Dial(ctx context.Context) (net.Conn, error) {
	s.mu.Lock()
	revoked, list := s.revoked, s.attemptsLocked()
	s.mu.Unlock()
	if revoked {
		return nil, ErrRevoked
	}
	if len(list) == 0 {
		return nil, fmt.Errorf("%w: no route this library knows", ErrUnreachable)
	}
	var errs []error
	for i, r := range list {
		actx, cancel := ctx, context.CancelFunc(func() {})
		if i < len(list)-1 {
			actx, cancel = context.WithTimeout(ctx, perRoute)
		}
		c, err := s.dialRoute(actx, r)
		cancel()
		if err == nil {
			s.mu.Lock()
			s.last = routeKey(r)
			s.mu.Unlock()
			return c, nil
		}
		if errors.Is(err, ErrRevoked) || errors.Is(err, ErrUnauthorized) || errors.Is(err, connectsecure.ErrProtoMismatch) {
			return nil, err
		}
		errs = append(errs, fmt.Errorf("%s: %w", r.T, err))
		if ctx.Err() != nil {
			break
		}
	}
	all := errors.Join(errs...)
	if errors.Is(all, connectsecure.ErrHandshakeClosed) { // a route answered but closed: say so
		return nil, fmt.Errorf("%w: %w: %v", ErrUnreachable, connectsecure.ErrHandshakeClosed, all)
	}
	return nil, fmt.Errorf("%w: %v", ErrUnreachable, all)
}

func (s *Session) dialRoute(ctx context.Context, r connectinvite.Route) (net.Conn, error) {
	if r.T == connectinvite.RouteTailcat {
		s.mu.Lock()
		client, port := s.client, uint16(s.link.Port)
		s.mu.Unlock()
		c, err := client.DialTCPPort(ctx, port)
		if err != nil {
			// Tailcat returns a failed dial as a nil *gonet.TCPConn inside a non-nil net.Conn
			// (tailcat v0.6.0, tailcat.go:1895), whose Close panics.
			return nil, err
		}
		return c, nil
	}
	if r.T == connectinvite.RouteRelay && r.M == connectinvite.ModeOpaque {
		return connectrelay.DialPhone(ctx, r.U, r.R, s.opts.HTTPClient)
	}
	sess, err := s.secure(ctx, r)
	if err != nil {
		return nil, err
	}
	c, err := sess.Open()
	if err != nil && sess.Err() != nil {
		// The session ended between secure and Open (the host closed it): start a new one once.
		if sess, err = s.secure(ctx, r); err != nil {
			return nil, err
		}
		return sess.Open()
	}
	return c, err
}

// secure returns the encrypted session on route r, starting one if none is live.
func (s *Session) secure(ctx context.Context, r connectinvite.Route) (*connectsecure.Session, error) {
	s.secMu.Lock()
	defer s.secMu.Unlock()
	s.mu.Lock()
	cur, curKey, hostKey := s.sec, s.secKey, s.link.HostKey
	s.mu.Unlock()
	if cur != nil && curKey == routeKey(r) {
		select {
		case <-cur.Done():
		default:
			return cur, nil
		}
	}
	server, ok := decodeKey(hostKey)
	if !ok {
		return nil, errors.New("connectphone: no host key for this route")
	}
	raw, err := dialRaw(ctx, r, s.opts.HTTPClient)
	if err != nil {
		return nil, err
	}
	sess, err := dialSecure(ctx, raw, connectsecure.ClientConfig{Static: noiseKeys(s.private), Server: server,
		Phone: s.private.Public().String(), Version: Version})
	if err != nil {
		if errors.Is(err, connectsecure.ErrUnpaired) {
			s.mu.Lock()
			s.revoked = true
			s.mu.Unlock()
			return nil, ErrRevoked
		}
		if errors.Is(err, connectsecure.ErrDenied) {
			return nil, ErrUnauthorized
		}
		return nil, err
	}
	s.mu.Lock()
	old := s.sec
	s.sec, s.secKey = sess, routeKey(r)
	s.mu.Unlock()
	if old != nil {
		_ = old.Close()
	}
	go s.watch(sess)
	return sess, nil
}

// watch reads the host's control messages: new routes, and a bye.
func (s *Session) watch(sess *connectsecure.Session) {
	for {
		m, err := sess.Control().Recv()
		if err != nil {
			if errors.Is(err, connectsecure.ErrStalled) { // that path went silent: try the others first
				s.mu.Lock()
				if s.sec == sess && s.last == s.secKey {
					s.last = ""
				}
				s.mu.Unlock()
			}
			return
		}
		switch m["type"] {
		case "routes":
			via, ok := routesOf(m)
			if !ok {
				continue
			}
			s.mu.Lock()
			s.link.setRoutes(via)
			s.setClientLocked()
			link := s.link
			s.mu.Unlock()
			if s.opts.OnLink != nil {
				s.opts.OnLink(link)
			}
		case "bye":
			// A bye is the host's last word on this session: end it now rather than when the
			// transport's own close arrives, so the next Dial starts a new session.
			if m["code"] == "unpaired" {
				s.mu.Lock()
				s.revoked = true
				s.mu.Unlock()
			}
			_ = sess.Close()
			return
		}
	}
}

func routesOf(m map[string]any) ([]connectinvite.Route, bool) {
	b, _ := json.Marshal(m["via"])
	var via []connectinvite.Route
	if json.Unmarshal(b, &via) != nil || connectinvite.ValidateRoutes(via) != nil {
		return nil, false
	}
	return via, true
}

// dialRaw opens the byte pipe of an encrypted route.
func dialRaw(ctx context.Context, r connectinvite.Route, client *http.Client) (net.Conn, error) {
	if r.T == connectinvite.RouteRelay {
		return connectrelay.DialPhone(ctx, r.U, r.R, client)
	}
	d := net.Dialer{Timeout: lanDial}
	var last error
	for _, a := range r.A {
		c, err := d.DialContext(ctx, "tcp", a)
		if err == nil {
			return c, nil
		}
		last = err
	}
	return nil, last
}

// dialSecure is connectsecure.Dial that also gives up when ctx ends.
func dialSecure(ctx context.Context, raw net.Conn, cfg connectsecure.ClientConfig) (*connectsecure.Session, error) {
	stop := context.AfterFunc(ctx, func() { raw.Close() })
	defer stop()
	return connectsecure.Dial(raw, cfg)
}

func decodeKey(s string) ([32]byte, bool) {
	var k [32]byte
	if !connectinvite.ValidKey(s) {
		return k, false
	}
	b, _ := base64.RawURLEncoding.DecodeString(s)
	copy(k[:], b)
	return k, true
}

// Revoked reports whether the host said, encrypted, that this phone is no longer paired. Delete the
// link: no route will take it again.
func (s *Session) Revoked() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.revoked
}

// Forward starts a loopback listener whose every connection is a new Dial.
func (s *Session) Forward() (*Forward, error) {
	return startForward(func(ctx context.Context) (net.Conn, error) {
		// A dial the host never answers must still end, so the app's connection closes: 20 s, as in
		// v0.2, or perRoute for each route when there are more.
		s.mu.Lock()
		n := len(s.attemptsLocked())
		s.mu.Unlock()
		ctx, cancel := context.WithTimeout(ctx, max(20*time.Second, time.Duration(n)*perRoute))
		defer cancel()
		return s.Dial(ctx)
	})
}

// Close ends the session's Tailcat client and its encrypted session.
func (s *Session) Close() error {
	s.mu.Lock()
	client, sec := s.client, s.sec
	s.sec = nil
	s.mu.Unlock()
	if sec != nil {
		_ = sec.Close()
	}
	if client != nil {
		return client.Close()
	}
	return nil
}

// Path reports how the session reaches the host: over Tailcat "direct", or "relay" with the DERP
// region code when it has one; over tailpocket-relay "relay" with the relay's host name; or "lan".
// For Tailcat it sends a disco ping and waits for the pong, which also nudges the client to look for a
// direct path (tailcat v0.6.0 Client.DiscoPing).
func (s *Session) Path(ctx context.Context) (route, relay string, err error) {
	s.mu.Lock()
	last, client := s.last, s.client
	s.mu.Unlock()
	switch {
	case strings.HasPrefix(last, connectinvite.RouteRelay+" "):
		return "relay", connectrelay.NormalizeHost(strings.TrimPrefix(last, connectinvite.RouteRelay+" ")), nil
	case strings.HasPrefix(last, connectinvite.RouteLAN+" "):
		return "lan", "", nil
	case client == nil:
		return "", "", ErrUnreachable
	}
	return routeOf(client.DiscoPing(ctx))
}

// routeOf reads a pong: it came over a direct path if it names an endpoint, otherwise through a relay.
func routeOf(res *ipnstate.PingResult, err error) (route, relay string, _ error) {
	if err != nil {
		return "", "", err
	}
	if res.Err != "" {
		return "", "", errors.New("connectphone: " + res.Err)
	}
	if res.Endpoint != "" {
		return "direct", "", nil
	}
	return "relay", relayName(res.DERPRegionCode), nil
}

// relayName drops the code tailcat.ParseAddr makes up: an address that carries its relay leaves the name
// out, and parsing fills in the relay's position, "1" (tailcat v0.6.0 tailcat.go:1089).
func relayName(code string) string {
	if strings.Trim(code, "0123456789") == "" {
		return ""
	}
	return code
}

// Sync registers the phone's Noise key with the host over Tailcat and takes the host's key and
// routes. A phone paired before v0.3 calls it once to use the encrypted routes without a new QR; it
// is also how a phone learns routes the host added. A host older than v0.3 does not answer.
func (s *Session) Sync(ctx context.Context) (Link, error) {
	s.mu.Lock()
	client := s.client
	s.mu.Unlock()
	if client == nil {
		return Link{}, errors.New("connectphone: this link has no Tailcat route")
	}
	httpClient := &http.Client{Transport: &http.Transport{DisableKeepAlives: true,
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			c, err := client.DialTCPPort(ctx, connectinvite.ControlPort)
			if err != nil {
				return nil, err
			}
			return c, nil
		}}}
	type syncReply struct {
		V   int                   `json:"v"`
		PK  string                `json:"pk"`
		Via []connectinvite.Route `json:"via"`
	}
	post := func(body map[string]string) (syncReply, error) {
		b, _ := json.Marshal(body)
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "http://control.invalid/v2/sync", bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		resp, err := httpClient.Do(req)
		if err != nil {
			return syncReply{}, fmt.Errorf("%w: %v", ErrUnreachable, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK { // never ErrRevoked: only the encrypted bye unpairs
			return syncReply{}, fmt.Errorf("connectphone: host answered %d", resp.StatusCode)
		}
		var r syncReply
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		if json.Unmarshal(data, &r) != nil || r.V != 2 || !connectinvite.ValidKey(r.PK) || connectinvite.ValidateRoutes(r.Via) != nil {
			return syncReply{}, errors.New("connectphone: invalid host reply")
		}
		return r, nil
	}
	// The proof is keyed with the host's Noise key: a link from before v0.3 asks for it first. Tailcat
	// already authenticates the host, so the key that comes back is the host's.
	s.mu.Lock()
	pk := s.link.HostKey
	s.mu.Unlock()
	if pk == "" {
		r, err := post(map[string]string{})
		if err != nil {
			return Link{}, err
		}
		pk = r.PK
	}
	proof, err := noiseProof(s.private, pk)
	if err != nil {
		return Link{}, err
	}
	r, err := post(map[string]string{"noise": noisePublic(s.private), "proof": proof})
	if err != nil {
		return Link{}, err
	}
	s.mu.Lock()
	s.link.HostKey = r.PK
	s.link.setRoutes(r.Via)
	link := s.link
	s.mu.Unlock()
	if s.opts.OnLink != nil {
		s.opts.OnLink(link)
	}
	return link, nil
}

// PairV2 pairs from a v2 invite (or a v1 invite read with DecodeV2) over the first route that works:
// the Tailcat window, or an encrypted relay or lan route. Every route redeems the same window.
func PairV2(ctx context.Context, inv connectinvite.V2, phoneKey string, opts Options) (Link, error) {
	if err := inv.Validate(); err != nil {
		return Link{}, err
	}
	private, err := parsePrivate(phoneKey)
	if err != nil {
		return Link{}, err
	}
	if inv.Expired(time.Now()) {
		return Link{}, ErrExpired
	}
	ctx, cancel := context.WithDeadline(ctx, time.Unix(inv.ExpiresAt, 0))
	defer cancel()
	var list []connectinvite.Route
	for _, r := range inv.Via {
		if r.T == connectinvite.RouteTailcat || r.T == connectinvite.RouteLAN || (r.T == connectinvite.RouteRelay && r.M != connectinvite.ModeOpaque) {
			list = append(list, r)
		}
	}
	if len(list) == 0 {
		return Link{}, fmt.Errorf("%w: no route to pair over", ErrUnreachable)
	}
	var errs []error
	for i, r := range list {
		actx, acancel := ctx, context.CancelFunc(func() {})
		if i < len(list)-1 {
			actx, acancel = context.WithTimeout(ctx, 30*time.Second)
		}
		var l Link
		if r.T == connectinvite.RouteTailcat {
			if l, err = Pair(actx, *r.Inv, phoneKey, opts); err == nil {
				l = syncAfterPair(ctx, l, opts)
			}
		} else {
			l, err = pairSecure(actx, inv, r, private, phoneKey, opts)
		}
		acancel()
		if err == nil {
			return l, nil
		}
		if errors.Is(err, ErrUnauthorized) || errors.Is(err, ErrTaken) {
			return Link{}, err
		}
		errs = append(errs, fmt.Errorf("%s: %w", r.T, err))
		if ctx.Err() != nil {
			break
		}
	}
	if !time.Now().Before(time.Unix(inv.ExpiresAt, 0)) {
		return Link{}, ErrExpired
	}
	return Link{}, fmt.Errorf("%w: %v", ErrUnreachable, errors.Join(errs...))
}

// syncAfterPair registers the Noise key of a phone that just paired over Tailcat, on the main node,
// and returns the link with the host's key and routes. If that fails the Tailcat link still works,
// and a later Sync adds the rest.
func syncAfterPair(ctx context.Context, l Link, opts Options) Link {
	s, err := Connect(l, opts)
	if err != nil {
		return l
	}
	defer s.Close()
	sctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if synced, err := s.Sync(sctx); err == nil {
		return synced
	}
	return l
}

// pairSecure pairs over one encrypted route: Noise IKpsk2 with the code's psk, then the host's paired
// message and its routes. It tries again while the route is up but the host is busy.
func pairSecure(ctx context.Context, inv connectinvite.V2, r connectinvite.Route, private key.NodePrivate, phoneKey string, opts Options) (Link, error) {
	server, _ := decodeKey(inv.PK)
	cfg := connectsecure.ClientConfig{Static: noiseKeys(private), Server: server, Version: Version,
		Pair:   &connectsecure.PairHello{CID: connectsecure.CIDFor(inv.PSK), PSK: connectsecure.PSKFor(inv.PSK)},
		Device: connectinvite.CleanName(opts.Device), Phone: private.Public().String(), Prove: nodekeyProof(private, server)}
	for {
		l, err := pairOnce(ctx, r, cfg, inv, phoneKey, opts)
		if err == nil || errors.Is(err, ErrUnauthorized) {
			return l, err
		}
		select {
		case <-ctx.Done():
			return Link{}, err
		case <-time.After(time.Second):
		}
	}
}

func pairOnce(ctx context.Context, r connectinvite.Route, cfg connectsecure.ClientConfig, inv connectinvite.V2, phoneKey string, opts Options) (Link, error) {
	raw, err := dialRaw(ctx, r, opts.HTTPClient)
	if err != nil {
		return Link{}, err
	}
	sess, err := dialSecure(ctx, raw, cfg)
	if err != nil {
		if errors.Is(err, connectsecure.ErrDenied) {
			return Link{}, ErrUnauthorized // wrong code, used, expired or another phone's
		}
		return Link{}, err
	}
	defer sess.Close()
	stop := context.AfterFunc(ctx, func() { sess.Close() })
	defer stop()
	l := Link{Key: phoneKey, HostKey: inv.PK, Name: inv.Name}
	gotPaired := false
	for {
		m, err := sess.Control().Recv()
		if err != nil {
			return Link{}, err
		}
		switch m["type"] {
		case "paired":
			if m["phone"] != cfg.Phone {
				return Link{}, errors.New("connectphone: the host paired another phone")
			}
			name, _ := m["name"].(string)
			payload, _ := m["payload"].(string)
			if connectinvite.ValidName(name) {
				l.Name = name
			}
			l.Payload, gotPaired = payload, true
		case "routes":
			via, ok := routesOf(m)
			if !gotPaired || !ok {
				return Link{}, errors.New("connectphone: invalid host reply")
			}
			l.setRoutes(via)
			if len(l.Via) == 0 && l.Address == "" {
				l.Via = []connectinvite.Route{r}
			}
			return l, l.validate()
		}
	}
}
