package connecthost

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"

	"github.com/tailscale/tailcat"
	"golang.org/x/crypto/curve25519"
	"tailscale.com/tailcfg"
	"tailscale.com/types/key"
)

// identity is the main node's key, pre-shared key and relay region; together they fix its address.
// noise and relay are the host's keys for the end-to-end encrypted routes (v0.3): its Noise static
// X25519 key, which phones pin, and the Ed25519 key whose public half names its room on relays.
// A state from before v0.3 has neither; New makes them and Start saves them.
type identity struct {
	key    key.NodePrivate
	psk    tailcat.PresharedKey
	region *tailcfg.DERPRegion // nil until the first start picks the nearest region
	noise  [32]byte
	relay  ed25519.PrivateKey
}

type identityDoc struct {
	V      int                 `json:"v"`
	Key    string              `json:"key"`
	PSK    string              `json:"psk"`
	Region *tailcfg.DERPRegion `json:"region,omitempty"`
	Noise  string              `json:"noise,omitempty"` // X25519 private key, base64url
	Relay  string              `json:"relay,omitempty"` // Ed25519 seed, base64url
}

func newIdentity() identity {
	id := identity{key: key.NewNode(), psk: tailcat.NewPresharedKey()}
	id.addSecure()
	return id
}

// addSecure makes the v0.3 keys a state lacks and reports whether it made any.
func (id *identity) addSecure() bool {
	changed := false
	if id.noise == ([32]byte{}) {
		if _, err := rand.Read(id.noise[:]); err != nil {
			panic(err)
		}
		changed = true
	}
	if id.relay == nil {
		_, id.relay, _ = ed25519.GenerateKey(rand.Reader)
		changed = true
	}
	return changed
}

// noisePublic is the key phones pin.
func (id identity) noisePublic() [32]byte {
	var pub [32]byte
	p, _ := curve25519.X25519(id.noise[:], curve25519.Basepoint)
	copy(pub[:], p)
	return pub
}

// relayPublic names the host's room on relays.
func (id identity) relayPublic() ed25519.PublicKey { return id.relay.Public().(ed25519.PublicKey) }

func parseIdentity(text string) (identity, error) {
	bad := ErrInvalidIdentity
	var doc identityDoc
	if json.Unmarshal([]byte(text), &doc) != nil || doc.V != 1 {
		return identity{}, bad
	}
	var id identity
	if id.key.UnmarshalText([]byte(doc.Key)) != nil || id.key.IsZero() {
		return identity{}, bad
	}
	if id.psk.UnmarshalText([]byte(doc.PSK)) != nil || id.psk.IsZero() {
		return identity{}, bad
	}
	if doc.Region != nil {
		if doc.Region.RegionID == 0 || len(doc.Region.Nodes) == 0 {
			return identity{}, bad
		}
		id.region = doc.Region
	}
	if doc.Noise != "" {
		b, err := base64.RawURLEncoding.DecodeString(doc.Noise)
		if err != nil || len(b) != 32 {
			return identity{}, bad
		}
		copy(id.noise[:], b)
	}
	if doc.Relay != "" {
		b, err := base64.RawURLEncoding.DecodeString(doc.Relay)
		if err != nil || len(b) != ed25519.SeedSize {
			return identity{}, bad
		}
		id.relay = ed25519.NewKeyFromSeed(b)
	}
	return id, nil
}

func (id identity) String() string {
	k, _ := id.key.MarshalText()
	p, _ := id.psk.MarshalText()
	doc := identityDoc{V: 1, Key: string(k), PSK: string(p), Region: id.region}
	if id.noise != ([32]byte{}) {
		doc.Noise = base64.RawURLEncoding.EncodeToString(id.noise[:])
	}
	if id.relay != nil {
		doc.Relay = base64.RawURLEncoding.EncodeToString(id.relay.Seed())
	}
	body, _ := json.Marshal(doc)
	return string(body)
}
