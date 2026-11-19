package connectsecure

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"sync/atomic"
	"time"

	"github.com/flynn/noise"
)

type firstPayload struct {
	Proto int    `json:"proto"`
	CID   string `json:"cid,omitempty"`
}

type helloMsg struct {
	Type    string `json:"type"`
	Proto   int    `json:"proto"`
	Version string `json:"version"`
	Device  string `json:"device,omitempty"`
	Phone   string `json:"phone,omitempty"`
	Proof   string `json:"proof,omitempty"` // base64url; ClientConfig.Prove
}

type byeMsg struct {
	Type   string `json:"type"`
	Code   string `json:"code"`
	Reason string `json:"reason,omitempty"`
}

type pairedMsg struct {
	Type string `json:"type"`
	Paired
}

func appendFrame(out, msg []byte) []byte {
	out = binary.BigEndian.AppendUint16(out, uint16(len(msg)))
	return append(out, msg...)
}

// limit closes conn when d runs out, whatever the conn does with deadlines.
type limit struct {
	t     *time.Timer
	fired atomic.Bool
}

func newLimit(d time.Duration, conn net.Conn) *limit {
	l := &limit{}
	l.t = time.AfterFunc(d, func() { l.fired.Store(true); conn.Close() })
	return l
}

// stop reports whether the limit had not run out yet.
func (l *limit) stop() bool { return l.t.Stop() && !l.fired.Load() }

// wrap turns an error into ErrHandshakeTimeout when the limit closed the conn.
func (l *limit) wrap(err error) error {
	if l.fired.Load() {
		return fmt.Errorf("%w (%v)", ErrHandshakeTimeout, err)
	}
	return err
}

// readHandshake reads one length-prefixed handshake message.
func readHandshake(conn net.Conn) ([]byte, error) {
	var hdr [2]byte
	if _, err := io.ReadFull(conn, hdr[:]); err != nil {
		return nil, err
	}
	msg := make([]byte, binary.BigEndian.Uint16(hdr[:]))
	if _, err := io.ReadFull(conn, msg); err != nil {
		return nil, err
	}
	return msg, nil
}

// readFirst reads exactly one record, which must be one complete control line,
// and leaves anything after that line for Control. Nothing else is read.
func (s *Session) readFirst() (map[string]any, error) {
	pt, err := s.readRecord()
	if err != nil {
		return nil, err
	}
	id, typ, payload, err := parseFrame(pt)
	if err != nil || id != 0 || typ != frameData {
		return nil, ErrNoHello
	}
	i := bytes.IndexByte(payload, '\n')
	if i < 0 || i > maxControlLine {
		return nil, ErrNoHello
	}
	m, err := parseLine(payload[:i])
	if err != nil {
		return nil, ErrNoHello
	}
	if rest := payload[i+1:]; len(rest) > 0 {
		if err := s.ctrl.feed(rest); err != nil {
			return nil, err
		}
	}
	return m, nil
}

func protoOf(m map[string]any) int {
	f, ok := m["proto"].(float64)
	if !ok {
		return 0
	}
	return int(f)
}

// Dial runs the phone's side over conn: preamble, handshake, hellos, all within
// Timing.Handshake. On error the conn is closed. When pairing, the paired message
// is the first thing Control().Recv returns.
func Dial(conn net.Conn, cfg ClientConfig) (*Session, error) {
	t := cfg.Timing.withDefaults()
	lim := newLimit(t.Handshake, conn)
	conn.SetDeadline(time.Now().Add(t.Handshake))
	s, err := dial(conn, cfg, t, lim)
	if err != nil {
		lim.stop()
		conn.Close()
		return nil, lim.wrap(err)
	}
	return s, nil
}

func dial(conn net.Conn, cfg ClientConfig, t Timing, lim *limit) (*Session, error) {
	proto := cfg.proto
	if proto == 0 {
		proto = Proto
	}
	pair := cfg.Pair != nil
	hs, err := noise.NewHandshakeState(handshakeConfig(pair, true, cfg.Static, cfg.Server[:], []byte(prologue), nil))
	if err != nil {
		return nil, err
	}
	first := firstPayload{Proto: proto}
	mode := byte(modeConnect)
	if pair {
		if err := hs.SetPresharedKey(cfg.Pair.PSK[:]); err != nil {
			return nil, err
		}
		first.CID = cfg.Pair.CID
		mode = modePair
	}
	p0, err := json.Marshal(first)
	if err != nil {
		return nil, err
	}
	m0, _, _, err := hs.WriteMessage(nil, p0)
	if err != nil {
		return nil, err
	}
	if _, err := conn.Write(appendFrame([]byte{'T', 'P', '2', mode}, m0)); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrHandshake, err)
	}

	m1, err := readHandshake(conn)
	if err == io.EOF && !lim.fired.Load() {
		return nil, ErrHandshakeClosed
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrHandshake, err)
	}
	_, send, recv, err := hs.ReadMessage(nil, m1)
	if err != nil || send == nil {
		return nil, fmt.Errorf("%w: %v", ErrHandshake, err)
	}

	s := newSession(conn, true, send, recv, cfg.Server, t)
	s.binding = hs.ChannelBinding()
	hello := helloMsg{Type: "hello", Proto: proto, Version: cfg.Version, Phone: cfg.Phone}
	if cfg.Prove != nil {
		p, err := cfg.Prove(s.binding)
		if err != nil {
			return nil, err
		}
		hello.Proof = base64.RawURLEncoding.EncodeToString(p)
	}
	if pair {
		hello.Device = cfg.Device
	}
	if err := s.ctrl.Send(hello); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrHandshake, err)
	}
	m, err := s.readFirst()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrNoHello, err)
	}
	switch m["type"] {
	case "hello":
		if protoOf(m) != Proto {
			return nil, ErrProtoMismatch
		}
	case "bye":
		code, _ := m["code"].(string)
		reason, _ := m["reason"].(string)
		return nil, &ByeError{Code: code, Reason: reason}
	default:
		return nil, ErrNoHello
	}
	if !lim.stop() {
		return nil, ErrHandshakeTimeout
	}
	conn.SetDeadline(time.Time{})
	s.start()
	return s, nil
}

// Accept runs the host's side over conn. It reads the preamble and the first
// message within Timing.First, then the phone's hello, then calls Admit, all within
// Timing.Handshake. Only when Admit says OK does it start reading streams; until
// then it reads nothing past the hello record. On error the conn is closed; an
// error after Admit said OK calls Admission.Undo once.
func Accept(conn net.Conn, cfg ServerConfig) (*Session, error) {
	if cfg.Limit != nil {
		if !cfg.Limit.acquire() {
			conn.Close()
			return nil, ErrTooManyHandshakes
		}
		defer cfg.Limit.release()
	}
	t := cfg.Timing.withDefaults()
	start := time.Now()
	first := newLimit(t.First, conn)
	whole := newLimit(t.Handshake, conn)
	conn.SetDeadline(start.Add(t.First))
	s, err := accept(conn, cfg, t, start, first, whole)
	if err != nil {
		first.stop()
		whole.stop()
		conn.Close()
		if first.fired.Load() {
			return nil, first.wrap(err)
		}
		return nil, whole.wrap(err)
	}
	return s, nil
}

func accept(conn net.Conn, cfg ServerConfig, t Timing, start time.Time, first, whole *limit) (*Session, error) {
	var pre [4]byte
	if _, err := io.ReadFull(conn, pre[:]); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrHandshake, err)
	}
	if string(pre[:3]) != "TP2" || (pre[3] != modePair && pre[3] != modeConnect) {
		return nil, ErrBadPreamble
	}
	pair := pre[3] == modePair

	static := noise.DHKey{Private: cfg.Identity.Private[:], Public: cfg.Identity.Public[:]}
	hs, err := noise.NewHandshakeState(handshakeConfig(pair, false, static, nil, []byte(prologue), nil))
	if err != nil {
		return nil, err
	}
	m0, err := readHandshake(conn)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrHandshake, err)
	}
	if !first.stop() {
		return nil, ErrHandshakeTimeout
	}
	conn.SetDeadline(start.Add(t.Handshake))
	p0, _, _, err := hs.ReadMessage(nil, m0)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrHandshake, err)
	}
	var fp firstPayload
	if err := json.Unmarshal(p0, &fp); err != nil {
		return nil, fmt.Errorf("%w: first message: %v", ErrHandshake, err)
	}
	if pair {
		if fp.CID == "" || cfg.PSK == nil {
			return nil, ErrUnknownCode
		}
		psk, ok := cfg.PSK(fp.CID)
		if !ok {
			return nil, ErrUnknownCode
		}
		if err := hs.SetPresharedKey(psk[:]); err != nil {
			return nil, err
		}
	}
	m1, recv, send, err := hs.WriteMessage(nil, nil)
	if err != nil || send == nil {
		return nil, fmt.Errorf("%w: %v", ErrHandshake, err)
	}
	if _, err := conn.Write(appendFrame(nil, m1)); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrHandshake, err)
	}

	var remote [32]byte
	copy(remote[:], hs.PeerStatic())
	s := newSession(conn, false, send, recv, remote, t)
	s.binding = hs.ChannelBinding()
	// Nothing encrypted goes out before the phone proves it holds the ephemeral
	// key, and only this one record is read before Admit.
	m, err := s.readFirst()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrNoHello, err)
	}
	if m["type"] != "hello" {
		return nil, ErrNoHello
	}
	if fp.Proto != Proto || protoOf(m) != Proto {
		s.ctrl.Send(byeMsg{Type: "bye", Code: "proto-mismatch", Reason: fmt.Sprintf("this computer speaks proto %d", Proto)})
		return nil, ErrProtoMismatch
	}

	phone, _ := m["phone"].(string)
	peer := Peer{Static: remote, Pair: pair, Phone: phone, Binding: s.binding}
	if p, ok := m["proof"].(string); ok {
		peer.Proof, _ = base64.RawURLEncoding.DecodeString(p)
	}
	if pair {
		peer.CID = fp.CID
		d, _ := m["device"].(string)
		peer.Device = cleanName(d)
	}
	// No Admit is the host's mistake, so busy: unpaired would make the phone
	// drop its pairing.
	adm := Admission{Bye: "busy"}
	if cfg.Admit != nil {
		a, err := cfg.Admit(peer)
		if err != nil {
			// Admit may have redeemed before it failed: take that back first.
			if a.Undo != nil {
				a.Undo()
			}
			a = Admission{Bye: "busy"}
		}
		adm = a
	}
	if !adm.OK {
		// An Admission with no code is the host's mistake as well: busy.
		// Only an explicit "unpaired" makes the phone drop its pairing.
		code := adm.Bye
		if code == "" {
			code = "busy"
		}
		s.ctrl.Send(byeMsg{Type: "bye", Code: code})
		return nil, fmt.Errorf("%w: %s", ErrNotAdmitted, code)
	}
	// From here a failure before the Session is returned takes the admission back
	// (Undo, once). The limit stops first: if it already ran out, that is a failure.
	undo := func() {
		if adm.Undo != nil {
			adm.Undo()
		}
	}
	if !whole.stop() {
		undo()
		return nil, ErrHandshakeTimeout
	}
	if err := s.ctrl.Send(helloMsg{Type: "hello", Proto: Proto, Version: cfg.Version}); err != nil {
		undo()
		return nil, fmt.Errorf("%w: %w", ErrHandshake, err)
	}
	if adm.Paired != nil {
		if err := s.ctrl.Send(pairedMsg{Type: "paired", Paired: *adm.Paired}); err != nil {
			undo()
			return nil, fmt.Errorf("%w: %w", ErrHandshake, err)
		}
	}
	// paired is out: the phone has its token, so no undo after this.
	conn.SetDeadline(time.Time{})
	s.device = adm.Device
	s.start()
	return s, nil
}
