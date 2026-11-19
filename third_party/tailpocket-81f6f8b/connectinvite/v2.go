package connectinvite

import (
	"encoding/base64"
	"encoding/json"
	"net"
	"net/url"
	"strings"
	"time"
)

// Version2 invites list every way to reach the host (Via) and carry what the end-to-end encrypted
// routes need: the host's Noise public key (PK) and the one-time pairing code (PSK). A phone pairs
// over whichever route it reaches first; every route redeems the same window. Docs: PROTOCOL.md, v2.
const Version2 = 2

// Route types.
const (
	RouteTailcat = "tailcat" // Tailcat: A is the main node's address, P its port; in invites only Inv
	RouteRelay   = "relay"   // tailpocket-relay: U is the relay's wss:// URL, R the host's room
	RouteLAN     = "lan"     // direct TCP: A lists "host:port" addresses
)

// ControlPort is the virtual port on a host's main Tailcat node where paired phones register their
// Noise key and read the routes (POST /v2/sync).
const ControlPort = 48933

// DefaultPrefix2 is the QR prefix for v2 invites. DecodeV2 reads v1 invites too, so an app may keep
// one prefix for both.
const DefaultPrefix2 = "tailpocket2:"

// ModeOpaque marks a relay route whose bytes the app encrypts itself (for example its own pinned
// TLS): no Noise, and the host does not know which phone a connection comes from.
const ModeOpaque = "opaque"

// Route is one way to reach a host. Phones skip types they do not know.
type Route struct {
	T   string   `json:"t"`
	A   []string `json:"a,omitempty"`
	P   int      `json:"p,omitempty"`
	U   string   `json:"u,omitempty"`
	R   string   `json:"r,omitempty"`
	M   string   `json:"m,omitempty"`
	Inv *Invite  `json:"inv,omitempty"` // tailcat in an invite: the v1 pairing window
}

// V2 is a version 2 invite. A v1 invite reads as a V2 with only a tailcat route (DecodeV2).
type V2 struct {
	Version   int     `json:"v"`
	Name      string  `json:"name,omitempty"`
	ExpiresAt int64   `json:"exp"`
	PK        string  `json:"pk,omitempty"`  // host's Noise X25519 public key, base64url, no padding
	PSK       string  `json:"psk,omitempty"` // the pairing code; Noise psk and cid are derived from it
	Via       []Route `json:"via"`
}

// EncodeV2 is Encode for a V2 invite.
func EncodeV2(in V2, prefix string) (string, error) {
	if err := in.Validate(); err != nil {
		return "", err
	}
	return pack(in, prefix)
}

// DecodeV2 reads a v2 invite, or a v1 invite as a V2 with one tailcat route.
func DecodeV2(text, prefix string) (V2, error) {
	body, err := unpack(text, prefix)
	if err != nil {
		return V2{}, err
	}
	var head struct {
		V int `json:"v"`
	}
	if json.Unmarshal(body, &head) != nil {
		return V2{}, ErrInvalid
	}
	if head.V == Version {
		v1, err := ParseJSON(body)
		if err != nil {
			return V2{}, err
		}
		return Upgrade(v1), nil
	}
	var in V2
	if json.Unmarshal(body, &in) != nil {
		return V2{}, ErrInvalid
	}
	if err := in.Validate(); err != nil {
		return V2{}, err
	}
	return in, nil
}

// Upgrade is v1 as a V2.
func Upgrade(v1 Invite) V2 {
	return V2{Version: Version2, Name: v1.Name, ExpiresAt: v1.ExpiresAt, Via: []Route{{T: RouteTailcat, Inv: &v1}}}
}

// Tailcat is the v1 pairing window inside the invite, or nil.
func (i V2) Tailcat() *Invite {
	for _, r := range i.Via {
		if r.T == RouteTailcat && r.Inv != nil {
			return r.Inv
		}
	}
	return nil
}

// Expired reports whether the invite can no longer be used at now.
func (i V2) Expired(now time.Time) bool { return now.Unix() >= i.ExpiresAt }

// Validate checks the fields. Routes of unknown types are kept, not checked.
func (i V2) Validate() error {
	if i.Version != Version2 || i.ExpiresAt <= 0 || !ValidName(i.Name) || len(i.Via) == 0 || len(i.Via) > 16 {
		return ErrInvalid
	}
	if (i.PK == "") != (i.PSK == "") || (i.PK != "" && (!ValidKey(i.PK) || !ValidCode(i.PSK))) {
		return ErrInvalid
	}
	for _, r := range i.Via {
		if err := r.validate(true); err != nil {
			return err
		}
		if (r.T == RouteLAN || (r.T == RouteRelay && r.M != ModeOpaque)) && i.PK == "" {
			return ErrInvalid // an encrypted relay route needs the host's key
		}
	}
	return nil
}

// ValidateRoutes checks routes a host sent a paired phone.
func ValidateRoutes(rs []Route) error {
	if len(rs) > 16 {
		return ErrInvalid
	}
	for _, r := range rs {
		if err := r.validate(false); err != nil {
			return err
		}
	}
	return nil
}

func (r Route) validate(inInvite bool) error {
	if r.M != "" && r.T != RouteRelay {
		return ErrInvalid // only a relay can be opaque: lan and Tailcat never carry the app's bytes bare
	}
	switch r.T {
	case RouteTailcat:
		if inInvite {
			if r.Inv == nil || r.Inv.Validate() != nil {
				return ErrInvalid
			}
			return nil
		}
		if len(r.A) != 1 || !ValidAddress(r.A[0]) || r.P < 1 || r.P > 65535 {
			return ErrInvalid
		}
	case RouteLAN:
		if len(r.A) == 0 || len(r.A) > 8 {
			return ErrInvalid
		}
		for _, a := range r.A {
			if h, p, err := net.SplitHostPort(a); err != nil || h == "" || p == "" || len(a) > 256 {
				return ErrInvalid
			}
		}
	case RouteRelay:
		if !ValidRelayURL(r.U) || !validB64(r.R, 16) || (r.M != "" && r.M != ModeOpaque) {
			return ErrInvalid
		}
	}
	return nil
}

// ValidKey reports a 32-byte key as base64url without padding.
func ValidKey(s string) bool { return validB64(s, 32) }

// ValidCode reports a pairing code: 16 bytes as base64url without padding.
func ValidCode(s string) bool { return validB64(s, 16) }

// NewCode returns a new pairing code.
func NewCode() string { return NewProof()[:22] }

func validB64(s string, n int) bool {
	b, err := base64.RawURLEncoding.DecodeString(s)
	return err == nil && len(b) == n && len(s) == base64.RawURLEncoding.EncodedLen(n)
}

// ValidRelayURL: wss://host[:port], or ws:// to a loopback address (tests).
func ValidRelayURL(s string) bool {
	u, err := url.Parse(strings.TrimRight(s, "/"))
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || len(s) > 256 {
		return false
	}
	switch u.Scheme {
	case "wss":
		return true
	case "ws":
		ip := net.ParseIP(u.Hostname())
		return u.Hostname() == "localhost" || (ip != nil && ip.IsLoopback())
	}
	return false
}

// Proof labels for connectsecure.KeyProof, keyed with the host's Noise key on the host side.
const (
	// ProofNoise: a phone holds the Noise key it reports (sync, Tailcat pairing). Signed with that
	// Noise key; the message is NoiseClaim.
	ProofNoise = "tailpocket2 noise"
	// ProofPair: a phone pairing over relay or lan holds the nodekey it names. Signed with the
	// nodekey; the message is the handshake hash.
	ProofPair = "tailpocket2 pair"
)

// NoiseClaim is what a ProofNoise proof covers: the phone's nodekey and its Noise public key.
func NoiseClaim(phone, noise string) []byte { return []byte(phone + "\n" + noise) }
