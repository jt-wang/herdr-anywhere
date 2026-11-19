package connecthost

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/tailscale/tailcat"
	"tailscale.com/types/key"

	"github.com/jt-wang/tailpocket/connectinvite"
	"github.com/jt-wang/tailpocket/connectsecure"
)

const (
	DefaultTTL = 2 * time.Minute
	MaxTTL     = 15 * time.Minute
	// After a phone pairs, the window stays open this long so a phone whose reply got lost can retry.
	retryGrace = 10 * time.Second
	maxBody    = 4096
)

// Phone is a phone that joined through a pairing window.
type Phone struct {
	Key    string // "nodekey:…"
	Device string // the name the phone gave, cleaned
	Route  string // the route it pairs over: RouteTailcat, RouteRelay or RouteLAN
}

type PairOptions struct {
	// TTL bounds the window (default 2 minutes, at most 15).
	TTL time.Duration
	// Payload returns the app's own data for this phone (a token, settings, anything), delivered
	// with the reply inside the tunnel. It runs once per window. Nil sends no payload. An error
	// refuses the phone: this is where an app checks a pairing, for example the route it came over.
	Payload func(Phone) (string, error)
}

// Pairing is one open window. One phone can join it.
type Pairing struct {
	h       *Host
	inv     connectinvite.Invite
	payload func(Phone) (string, error)
	expires time.Time
	node    node
	server  *http.Server
	ln      *chanListener

	code string // the v2 pairing code; its cid names this window on the encrypted routes
	cid  string

	mu    sync.Mutex
	bound string
	noise *[32]byte // the Noise key the bound phone brought, if any
	reply *pairReply
	phone Phone

	joined   chan struct{}
	ended    chan struct{}
	joinOnce sync.Once
	endOnce  sync.Once
	timer    *time.Timer
}

type pairRequest struct {
	PhoneKey string `json:"phoneKey"`
	Device   string `json:"device"`
}

// pairReply is v1's reply; PK and Via are what v0.3 phones read for the encrypted routes, and older
// phones skip.
type pairReply struct {
	V       int                   `json:"v"`
	Address string                `json:"address"`
	Port    int                   `json:"port"`
	Name    string                `json:"name"`
	Payload string                `json:"payload,omitempty"`
	PK      string                `json:"pk,omitempty"`
	Via     []connectinvite.Route `json:"via,omitempty"`
}

// Pair opens a pairing window. Show p.QR(prefix) as a QR code, share it as a link, or call
// p.ServeLocal for same-Wi-Fi discovery; then p.Wait for the phone.
func (h *Host) Pair(ctx context.Context, opts PairOptions) (*Pairing, error) {
	ttl := opts.TTL
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	if ttl > MaxTTL {
		ttl = MaxTTL
	}
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return nil, ErrClosed
	}
	region := h.id.region
	h.mu.Unlock()

	client := key.NewNode()
	clientText, _ := client.MarshalText()
	expires := time.Now().Add(ttl)
	code := connectinvite.NewCode()
	p := &Pairing{h: h, payload: opts.Payload, expires: expires, ln: newChanListener(),
		joined: make(chan struct{}), ended: make(chan struct{}), code: code, cid: connectsecure.CIDFor(code)}
	h.op.Lock()
	err := h.flushLocked() // the v2 invite carries the host's Noise key: it must be saved first
	h.op.Unlock()
	if err != nil {
		return nil, err
	}
	var n node
	n, err = h.start(ctx, nodeSpec{key: key.NewNode(), psk: tailcat.NewPresharedKey(), region: region,
		allowed: []key.NodePublic{client.Public()}, port: h.pairingPort, derpMapURL: h.derpMapURL,
		onTCP: func(port uint16) func(netConn) {
			if port != h.pairingPort {
				return nil
			}
			return func(c netConn) { p.ln.offer(c) }
		}})
	if err != nil {
		return nil, err
	}
	p.node = n
	p.inv = connectinvite.Invite{Version: connectinvite.Version, Address: n.Address(), Port: int(h.pairingPort), Key: string(clientText),
		Proof: connectinvite.NewProof(), ExpiresAt: expires.Unix(), Name: h.name}
	if err := p.inv.Validate(); err != nil {
		_ = n.Close()
		return nil, err
	}
	p.server = &http.Server{Handler: p, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = p.server.Serve(p.ln) }()
	p.mu.Lock()
	p.timer = time.AfterFunc(time.Until(expires), p.Close)
	p.mu.Unlock()
	h.mu.Lock()
	h.pairings[p] = struct{}{}
	h.windows[p.cid] = p
	h.mu.Unlock()
	return p, nil
}

// InviteV2 is the window as a v2 invite: the Tailcat window, every relay and lan route, the host's
// Noise key and the pairing code. A phone may pair over any of them; they all redeem this window.
func (p *Pairing) InviteV2() connectinvite.V2 {
	v1 := p.inv
	via := []connectinvite.Route{{T: connectinvite.RouteTailcat, Inv: &v1}}
	h := p.h
	h.mu.Lock()
	pk := h.id.noisePublic()
	via = append(via, h.extraRoutesLocked()...)
	h.mu.Unlock()
	return connectinvite.V2{Version: connectinvite.Version2, Name: v1.Name, ExpiresAt: v1.ExpiresAt,
		PK: encodeKey(pk), PSK: p.code, Via: via}
}

// QR2 is InviteV2 as text for a QR code or link, e.g. with connectinvite.DefaultPrefix2.
func (p *Pairing) QR2(prefix string) (string, error) {
	return connectinvite.EncodeV2(p.InviteV2(), prefix)
}

// Invite is what the phone needs; holding it lets a phone pair until it expires.
func (p *Pairing) Invite() connectinvite.Invite { return p.inv }

// QR is the invite as text for a QR code or link, e.g. with connectinvite.DefaultPrefix.
func (p *Pairing) QR(prefix string) (string, error) { return connectinvite.Encode(p.inv, prefix) }

// Expires is when the window closes.
func (p *Pairing) Expires() time.Time { return p.expires }

// Wait returns the phone that joined, ErrExpired when the window closed first, or ctx.Err().
func (p *Pairing) Wait(ctx context.Context) (Phone, error) {
	select {
	case <-p.joined:
		p.mu.Lock()
		defer p.mu.Unlock()
		return p.phone, nil
	case <-p.ended:
		select {
		case <-p.joined:
			p.mu.Lock()
			defer p.mu.Unlock()
			return p.phone, nil
		default:
			return Phone{}, ErrExpired
		}
	case <-ctx.Done():
		return Phone{}, ctx.Err()
	}
}

// Done is closed when the window has closed (expired, Close, or after a phone joined).
func (p *Pairing) Done() <-chan struct{} { return p.ended }

// Close ends the window early. A phone that already joined stays paired.
func (p *Pairing) Close() {
	p.endOnce.Do(func() {
		p.mu.Lock()
		if p.timer != nil {
			p.timer.Stop()
		}
		p.mu.Unlock()
		if p.server != nil {
			_ = p.server.Close()
		}
		_ = p.ln.Close()
		if p.node != nil {
			_ = p.node.Close()
		}
		p.h.mu.Lock()
		delete(p.h.pairings, p)
		delete(p.h.windows, p.cid)
		p.h.mu.Unlock()
		close(p.ended)
	})
}

func (p *Pairing) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.URL.EscapedPath() != "/pair" || r.URL.RawQuery != "" {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+p.inv.Proof)) != 1 {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBody+1))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if len(body) > maxBody {
		w.WriteHeader(http.StatusRequestEntityTooLarge)
		return
	}
	var req pairRequest
	if json.Unmarshal(body, &req) != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	k, err := parsePhone(req.PhoneKey)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	// The pairing node admits the invite's one-time key, so phoneKey is only a claim here: no Noise
	// key is taken with it (pre.2 phones still send one; it is ignored). The phone registers its key
	// with Sync on the main node, where WireGuard proves the nodekey.
	reply, status := p.join(r.Context(), k, req.Device, RouteTailcat, nil)
	if status != http.StatusOK {
		w.WriteHeader(status)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(reply)
}

// join pairs phone k through this window, once: the same phone again (a lost reply) gets the same
// reply. It returns an HTTP status; the encrypted routes map it to a bye.
func (p *Pairing) join(ctx context.Context, k key.NodePublic, device, route string, noise *[32]byte) (*pairReply, int) {
	phone := Phone{Key: k.String(), Device: connectinvite.CleanName(device), Route: route}
	if phone.Device == "" {
		phone.Device = "Phone"
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.bound != "" && (p.bound != phone.Key || (noise != nil && p.noise != nil && *noise != *p.noise)) {
		return nil, http.StatusConflict
	}
	if p.reply == nil {
		if !time.Now().Before(p.expires) {
			return nil, http.StatusGone
		}
		payload := ""
		if p.payload != nil {
			var err error
			if payload, err = p.payload(phone); err != nil {
				return nil, http.StatusServiceUnavailable
			}
		}
		address, err := p.h.addPhone(ctx, k, noise)
		if err != nil || address == "" {
			return nil, http.StatusServiceUnavailable
		}
		p.bound, p.phone, p.noise = phone.Key, phone, noise
		h := p.h
		p.reply = &pairReply{V: 1, Address: address, Port: int(h.port), Name: h.name, Payload: payload}
		h.mu.Lock()
		// The encrypted routes only for a phone whose Noise key the host now holds: a phone that pairs
		// again keeps its old key (addPhone), and must not be told routes that would call it unpaired.
		if cur, ok := h.noise[k]; ok && noise != nil && cur == *noise {
			p.reply.PK, p.reply.Via = encodeKey(h.id.noisePublic()), h.routesLocked()
		}
		h.mu.Unlock()
		p.joinOnce.Do(func() { close(p.joined) })
		if time.Until(p.expires) > retryGrace {
			p.timer.Reset(retryGrace)
		}
		h.record(Event{Kind: EventPaired, Phone: phone.Key, Route: phone.Route})
	}
	return p.reply, http.StatusOK
}
