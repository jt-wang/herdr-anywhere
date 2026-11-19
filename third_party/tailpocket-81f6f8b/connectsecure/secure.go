// Package connectsecure is the encrypted layer between the phone and the host: a Noise
// handshake over any byte pipe, an encrypted record layer, and streams multiplexed
// over it with a JSON control stream. It knows nothing about the device list or
// serve; the caller decides who a peer is through ServerConfig.Admit.
package connectsecure

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/flynn/noise"
)

// Identity is the host's static X25519 key pair.
type Identity struct{ Private, Public [32]byte }

// NewIdentity makes a fresh static key pair from crypto/rand.
func NewIdentity() (Identity, error) {
	k, err := noise.DH25519.GenerateKeypair(rand.Reader)
	if err != nil {
		return Identity{}, err
	}
	var id Identity
	copy(id.Private[:], k.Private)
	copy(id.Public[:], k.Public)
	return id, nil
}

// PairHello is what a pairing phone brings: the window it answers (CID) and the
// Noise psk derived from the one-time code. The phone's name goes in its hello
// (ClientConfig.Device), not in the first message, which has no forward secrecy.
type PairHello struct {
	CID string
	PSK [32]byte
}

// Timing holds the timers. A zero field takes its default.
type Timing struct {
	First     time.Duration // Accept: preamble and first handshake message in; default 5 s
	Handshake time.Duration // Accept: raw conn to Admit done; Dial: the whole Dial; default 10 s
	Ping      time.Duration // how often a ping goes out; default 30 s
	Idle      time.Duration // close when nothing came in for this long; default 60 s
	Write     time.Duration // one write to the conn; default 60 s
	// Stall (the phone only): when nothing came in for this long after a write, ping; when nothing
	// came in for as long again, end the session with ErrStalled. Default 3 s. It finds a path that
	// went silent (Wi-Fi gone without a reset) within 2×Stall instead of Idle or TCP's timeout.
	Stall time.Duration
}

const (
	defaultFirst     = 5 * time.Second
	defaultHandshake = 10 * time.Second
	defaultPing      = 30 * time.Second
	defaultIdle      = 60 * time.Second
	defaultWrite     = 60 * time.Second
	defaultStall     = 3 * time.Second
	closeGrace       = time.Second // Session.Close waits this long for its bye to go out
	maxDeviceName    = 64          // bytes of the phone's name the host keeps
)

func (t Timing) withDefaults() Timing {
	def := func(d *time.Duration, v time.Duration) {
		if *d <= 0 {
			*d = v
		}
	}
	def(&t.First, defaultFirst)
	def(&t.Handshake, defaultHandshake)
	def(&t.Ping, defaultPing)
	def(&t.Idle, defaultIdle)
	def(&t.Write, defaultWrite)
	def(&t.Stall, defaultStall)
	return t
}

// ClientConfig is the phone's side of a handshake.
type ClientConfig struct {
	Static  noise.DHKey // the phone's X25519 key pair
	Server  [32]byte    // the the host public key the phone expects
	Pair    *PairHello  // nil: connect as a paired phone
	Device  string      // the phone's name; sent in the hello when pairing
	Version string      // the phone library's version, sent in hello
	Phone   string      // the phone's id on the host (TailPocket: its "nodekey:…"), sent in hello
	// Prove, when set, proves Phone in the hello: it gets the handshake hash and returns what the host
	// checks (TailPocket: KeyProof with the nodekey's private key).
	Prove  func(binding []byte) ([]byte, error)
	Timing Timing

	proto int // 0 means Proto; tests set another value
}

// Peer is the phone as the host knows it once the handshake and the phone's
// hello are through. It carries no psk.
type Peer struct {
	Static [32]byte // the phone's Noise public key
	Pair   bool     // pair mode
	CID    string   // pair mode only
	Device string   // pair mode only; from the hello, cut to 64 bytes, no control characters
	Phone  string   // the id the phone says it has, from the hello; the host checks it
	// Binding is this session's handshake hash; Proof is what the phone's Prove returned for it.
	Binding, Proof []byte
}

// Admission is what Admit decided. With OK false Accept sends bye{Bye} and
// closes; it returns no Session and never starts reading streams.
type Admission struct {
	OK     bool
	Bye    string  // the bye code when OK is false, e.g. "unpaired"; "" means "busy"
	Device string  // with OK, the device this session belongs to
	Paired *Paired // pairing done: sent to the phone before the reader starts
	// Undo may be nil. If anything fails after Admit said OK and before Accept
	// returns the Session, Accept calls it once (pair mode: take the Redeem back).
	// Once paired has been written it is not called.
	Undo func()
}

// Paired is the paired message's content: the phone's id on this host, the host's
// name and the app's payload for this phone.
type Paired struct {
	Phone   string `json:"phone"`
	Name    string `json:"name"`
	Payload string `json:"payload,omitempty"`
}

// ServerConfig is the host's side of a handshake.
type ServerConfig struct {
	Identity Identity
	// PSK returns the psk of the open pairing window cid names; called when the
	// first message of a pairing has been read.
	PSK func(cid string) ([32]byte, bool)
	// Admit is called after the handshake and the phone's hello, before anything
	// else is read. An error counts as OK false with Bye "busy"; if the Admission
	// returned with the error has an Undo, Accept calls it first. nil turns
	// everyone away with bye busy.
	Admit   func(p Peer) (Admission, error)
	Version string // the host's version, sent in hello
	Timing  Timing
	// Limit caps handshakes in progress at once; nil means no cap. Wrap passes
	// NewLimiter(16).
	Limit *Limiter
}

// Limiter caps how many handshakes run at once. One more is closed at once.
type Limiter struct{ slots chan struct{} }

// NewLimiter allows n handshakes at once.
func NewLimiter(n int) *Limiter { return &Limiter{slots: make(chan struct{}, n)} }

func (l *Limiter) acquire() bool {
	select {
	case l.slots <- struct{}{}:
		return true
	default:
		return false
	}
}

func (l *Limiter) release() { <-l.slots }

// Proto is the only protocol version this package speaks. 2: ChaChaPoly, rekey frames and the
// pairing proof (v0.3.0-pre.2); 1 was v0.3.0-pre.1 (AESGCM), which no release shipped.
const Proto = 2

const prologue = "tailpocket2"

// Preamble modes, the fourth byte after "TP2".
const (
	modePair    = 'P'
	modeConnect = 'C'
)

var (
	// ErrHandshakeClosed: the other side closed the connection cleanly right after
	// the phone's first handshake message. The phone cannot tell why: the host
	// does this when the message does not decrypt (the phone holds another
	// computer's key) and when the pairing code is unknown, used or expired, but
	// so does a relay, the network, or a the host turning connections away. Say
	// the likelier thing first (the computer is not reachable on this route) and
	// offer what helps either way: check the host runs, then pair again.
	// A transport that knows why it closed (a relay close code, an HTTP refusal)
	// should return its own error, not io.EOF; that error comes through as itself.
	ErrHandshakeClosed = errors.New("secure: the computer closed the connection after the first handshake message")
	// ErrHandshake: the Noise handshake failed (bad message, wrong psk, cut short).
	ErrHandshake = errors.New("secure: handshake failed")
	// ErrHandshakeTimeout: the handshake took longer than Timing allows.
	ErrHandshakeTimeout = errors.New("secure: handshake took too long")
	// ErrTooManyHandshakes: Limit was full; the connection was closed at once.
	ErrTooManyHandshakes = errors.New("secure: too many handshakes at once")
	// ErrBadPreamble: the first four bytes were not "TP2P" or "TP2C".
	ErrBadPreamble = errors.New("secure: not a tailpocket2 connection")
	// ErrUnknownCode: no open pairing window for this cid.
	ErrUnknownCode = errors.New("secure: no pairing window for this code")
	// ErrNoHello: the peer's first encrypted message was not a hello we could decrypt.
	ErrNoHello = errors.New("secure: no hello after the handshake")
	// ErrNotAdmitted: Admit said no; the host sent bye and closed.
	ErrNotAdmitted = errors.New("secure: phone not admitted")
	// Bye codes as errors: a *ByeError matches the one for its code.
	ErrProtoMismatch = errors.New("secure: protocol version mismatch")
	ErrUnpaired      = errors.New("secure: this phone is not paired")
	ErrDenied        = errors.New("secure: the pairing code was refused")
	ErrBusy          = errors.New("secure: the computer is busy")
	ErrStopping      = errors.New("secure: the computer is stopping")
	// ErrConnLost: the byte pipe under the session broke. It wraps
	// io.ErrUnexpectedEOF, never io.EOF: a stream without a close frame is not finished.
	ErrConnLost = errors.New("secure: connection lost")
	// ErrBadRecord: a record did not decrypt; the whole connection is closed.
	ErrBadRecord = errors.New("secure: record failed to decrypt")
	// ErrFlowControl: the peer sent more than the stream's window.
	ErrFlowControl = errors.New("secure: peer overran a stream window")
	// ErrProtocol: a frame broke the multiplexing or control-stream rules.
	ErrProtocol = errors.New("secure: protocol violation")
	// ErrIdle: nothing came from the peer for Timing.Idle.
	ErrIdle = errors.New("secure: peer went quiet")
	// ErrStalled: the phone wrote and heard nothing back, not even a pong, for 2×Timing.Stall.
	ErrStalled = errors.New("secure: the path went silent")
	// ErrWriteStalled: one write to the conn did not finish within Timing.Write.
	ErrWriteStalled = errors.New("secure: peer stopped reading")
)

// ByeError is a bye the other side sent.
type ByeError struct{ Code, Reason string }

func (e *ByeError) Error() string {
	if e.Reason != "" {
		return fmt.Sprintf("secure: computer said bye (%s): %s", e.Code, e.Reason)
	}
	return fmt.Sprintf("secure: computer said bye (%s)", e.Code)
}

func (e *ByeError) Is(target error) bool {
	return byeErrs[e.Code] == target && target != nil
}

var byeErrs = map[string]error{
	"proto-mismatch": ErrProtoMismatch,
	"unpaired":       ErrUnpaired,
	"denied":         ErrDenied,
	"busy":           ErrBusy,
	"stopping":       ErrStopping,
}

// cleanName keeps at most maxDeviceName bytes of valid UTF-8, with control and
// format characters (newlines, escapes, bidi overrides) turned into spaces.
func cleanName(s string) string {
	var b strings.Builder
	for _, r := range strings.ToValidUTF8(s, "\uFFFD") {
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp) {
			r = ' '
		}
		if b.Len()+utf8.RuneLen(r) > maxDeviceName {
			break
		}
		b.WriteRune(r)
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// PSKFor is the Noise psk for a pairing code: SHA-256("tailpocket2 psk" || code).
func PSKFor(code string) [32]byte {
	return sha256.Sum256(append([]byte("tailpocket2 psk"), code...))
}

// CIDFor names a pairing window without giving the code away:
// base64url(SHA-256("tailpocket2 cid" || code)[:12]), 16 characters.
func CIDFor(code string) string {
	h := sha256.Sum256(append([]byte("tailpocket2 cid"), code...))
	return base64.RawURLEncoding.EncodeToString(h[:12])
}

// ChaChaPoly: constant time everywhere, wasm included (Go's software AES is table-based), and no
// per-key data limit a session can reach (AES-GCM's is about 360 GiB at TLS 1.3's margin).
var suite = noise.NewCipherSuite(noise.DH25519, noise.CipherChaChaPoly, noise.HashSHA256)

// handshakeConfig is the one place the Noise parameters are chosen:
// Noise_IK_25519_ChaChaPoly_SHA256 to connect, Noise_IKpsk2_25519_ChaChaPoly_SHA256 to
// pair. The psk is set on the state afterwards (SetPresharedKey), because
// The host learns which psk only from the first message.
// peer is the responder's public key on the initiator, nil on the responder.
// rng nil means crypto/rand.
func handshakeConfig(pair, initiator bool, static noise.DHKey, peer []byte, prologue []byte, rng io.Reader) noise.Config {
	c := noise.Config{
		CipherSuite:   suite,
		Random:        rng,
		Pattern:       noise.HandshakeIK,
		Initiator:     initiator,
		Prologue:      prologue,
		StaticKeypair: static,
		PeerStatic:    peer,
	}
	if pair {
		c.PresharedKeyPlacement = 2
	}
	return c
}

// protocolName is the Noise protocol name handshakeConfig builds.
func protocolName(pair bool) string {
	p := noise.HandshakeIK.Name
	if pair {
		p += "psk2"
	}
	return "Noise_" + p + "_" + string(suite.Name())
}
