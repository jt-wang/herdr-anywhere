// Package connectrelay is the tailpocket-relay wire protocol and server. The relay sits
// between a phone and a the host that both dial out to it, and copies
// WebSocket messages between them. It never sees plaintext: everything it
// copies is already Noise-encrypted by the two ends.
package connectrelay

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"net"
	"strings"
)

// Paths the relay serves.
const (
	PathHost   = "/v1/host"   // the host registers a room
	PathPhone  = "/v1/phone"  // phone joins a room
	PathAccept = "/v1/accept" // the host takes one waiting phone
	PathHealth = "/healthz"
)

// WebSocket close codes the relay sends.
const (
	CloseBadRegister   = 4403 // room does not come from pub, or the signature is wrong
	CloseNoHost        = 4404 // no live the host in this room
	CloseAcceptTimeout = 4408 // the the host did not accept the phone in time
	CloseRoomTaken     = 4409 // this room already has a live the host
)

// MaxMessage is the largest single WebSocket message, in bytes, either way.
const MaxMessage = 64 << 10

// Message types on the the host's control connection.
const (
	TypeChallenge = "challenge"
	TypeRegister  = "register"
	TypeReady     = "ready"
	TypeIncoming  = "incoming"
)

// Challenge is the relay's first message to a registering the host.
type Challenge struct {
	Type  string `json:"type"`
	Nonce string `json:"nonce"` // 16 random bytes, base64url without padding
}

// Register is the the host's answer to Challenge.
type Register struct {
	Type string `json:"type"`
	Pub  string `json:"pub"`  // Ed25519 public key, base64url without padding
	Host string `json:"host"` // the relay's host name as the the host dialled it (NormalizeHost)
	Sig  string `json:"sig"`  // SignChallenge output, base64url without padding
}

// Ready says the room is the the host's now.
type Ready struct {
	Type string `json:"type"`
}

// Incoming tells the the host a phone is waiting; it opens
// PathAccept?room=…&id=ID within the accept timeout.
type Incoming struct {
	Type string `json:"type"`
	ID   string `json:"id"` // 16 random bytes, base64url without padding
}

const challengePrefix = "tailpocket-relay host v1\n"

// RoomFor is the room a the host's relay key owns:
// base64url(SHA-256(pub)[:16]), 22 characters.
func RoomFor(pub ed25519.PublicKey) string {
	sum := sha256.Sum256(pub)
	return base64.RawURLEncoding.EncodeToString(sum[:16])
}

// NormalizeHost is a relay host name in the form both ends sign: lower
// case, no scheme, path, port, trailing dot or IPv6 brackets. It takes a
// host, a host:port or a whole URL: "wss://Relay.Example.com:443/" gives
// "relay.example.com".
func NormalizeHost(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
		if j := strings.IndexAny(s, "/?#"); j >= 0 {
			s = s[:j]
		}
		if j := strings.LastIndex(s, "@"); j >= 0 {
			s = s[j+1:]
		}
	}
	if h, _, err := net.SplitHostPort(s); err == nil {
		s = h
	}
	s = strings.TrimSuffix(strings.TrimPrefix(s, "["), "]")
	s = strings.TrimSuffix(s, ".")
	return strings.ToLower(s)
}

// challengeText binds the signature to the relay (so another relay cannot
// pass it on), the room and this challenge.
func challengeText(host, room, nonce string) []byte {
	return []byte(challengePrefix + NormalizeHost(host) + "\n" + room + "\n" + nonce)
}

// SignChallenge signs "tailpocket-relay host v1\n" + host + "\n" + room + "\n" +
// nonce. host goes through NormalizeHost first; nonce is the string exactly
// as it came in the challenge.
func SignChallenge(priv ed25519.PrivateKey, host, room, nonce string) []byte {
	return ed25519.Sign(priv, challengeText(host, room, nonce))
}

// VerifyChallenge checks a SignChallenge signature.
func VerifyChallenge(pub ed25519.PublicKey, host, room, nonce string, sig []byte) bool {
	if len(pub) != ed25519.PublicKeySize {
		return false
	}
	return ed25519.Verify(pub, challengeText(host, room, nonce), sig)
}

// ValidRoom reports whether s is 16 bytes in strict base64url without padding.
func ValidRoom(s string) bool { return valid16(s) }

// ValidID reports whether s looks like an Incoming id.
func ValidID(s string) bool { return valid16(s) }

func valid16(s string) bool {
	if len(s) != 22 {
		return false
	}
	b, err := base64.RawURLEncoding.Strict().DecodeString(s)
	return err == nil && len(b) == 16
}
