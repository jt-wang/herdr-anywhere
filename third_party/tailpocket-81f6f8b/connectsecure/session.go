package connectsecure

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/flynn/noise"
)

// Wire limits. A record is a 2-byte big-endian length and one Noise message, so at
// most 2 + 65535 bytes; its plaintext is one frame.
const (
	tagLen          = 16
	maxRecordPlain  = noise.MaxMsgLen - tagLen        // 65519
	frameHeaderLen  = 7                               // stream u32, type u8, len u16
	maxFramePayload = maxRecordPlain - frameHeaderLen // 65512

	frameOpen   = 0
	frameData   = 1
	frameClose  = 2
	frameWindow = 3
	frameRekey  = 4 // stream 0, no payload: the sender's next record uses its rekeyed key

	initialWindow  = 256 << 10
	windowUpdateAt = initialWindow / 2 // give credit back once half the window was read
	maxWindow      = 1 << 30           // a peer granting more than this is broken
	maxStreams     = 256               // open streams per session
	maxUnread      = 16 << 20          // bytes received and not yet read, all streams together
)

// Rekeying (Noise spec 11.3, CipherState.Rekey): a sender rekeys after rekeyRecords records or
// rekeyAfter with one key, whichever comes first; a receiver ends the session when one key brought
// more than rejectRecords. Variables so tests can lower them.
var (
	rekeyRecords  uint64 = 1 << 20
	rekeyAfter           = 10 * time.Minute
	rejectRecords uint64 = 1 << 22
)

// Session is one encrypted connection carrying streams and a control stream.
type Session struct {
	conn      net.Conn
	initiator bool // the phone
	remote    [32]byte
	timing    Timing

	wmu      sync.Mutex // one record at a time, nonces in order
	send     *noise.CipherState
	sent     uint64    // records sent with the current send key
	sendFrom time.Time // when the current send key began
	// rekeyRecords, rekeyAfter and rejectRecords as the session started
	rekeyN, rejectN uint64
	rekeyT          time.Duration
	wbuf            []byte
	wplain          []byte

	recv     *noise.CipherState // only the reader touches it
	received uint64             // records received with the current recv key
	rhdr     [2]byte
	rbuf     []byte
	pbuf     []byte
	unread   int // bytes in stream buffers; guarded by mu

	binding []byte // the handshake hash

	openMu sync.Mutex // ids go out in order: the host refuses an id below one it has seen

	mu         sync.Mutex
	streams    map[uint32]*stream
	nextID     uint32 // phone: next id to open
	lastPeerID uint32 // the host: highest id the phone opened
	accepts    []*stream
	acceptSig  chan struct{}
	closed     bool
	err        error

	ctrl       Control
	lastRecv   atomic.Int64
	writeStart atomic.Int64 // when the conn.Write now in progress began; 0 when none is
	pong       chan struct{}
	credits    []credit // window to give back for streams we closed; sent by keepalive
	creditSig  chan struct{}
	done       chan struct{}
	once       sync.Once
	closeOnce  sync.Once
	idle       *time.Timer
	wdog       *time.Timer // fires when one conn.Write has run for Timing.Write

	// The phone's stall check (Timing.Stall). lastWrite is the last record out, in UnixNano.
	lastWrite   atomic.Int64
	stallMu     sync.Mutex
	stall       *time.Timer // nil until start, and on the host
	stallArmed  bool
	stallFrom   int64 // the write the check waits to hear back after
	stallPinged bool
	device      string
	started     time.Time
}

func newSession(conn net.Conn, initiator bool, send, recv *noise.CipherState, remote [32]byte, t Timing) *Session {
	s := &Session{
		conn:      conn,
		initiator: initiator,
		remote:    remote,
		timing:    t,
		send:      send,
		sendFrom:  time.Now(),
		rekeyN:    rekeyRecords,
		rekeyT:    rekeyAfter,
		rejectN:   rejectRecords,
		recv:      recv,
		wbuf:      make([]byte, 0, 2+noise.MaxMsgLen),
		wplain:    make([]byte, maxRecordPlain),
		rbuf:      make([]byte, noise.MaxMsgLen),
		pbuf:      make([]byte, 0, noise.MaxMsgLen),
		streams:   map[uint32]*stream{},
		nextID:    1,
		acceptSig: make(chan struct{}, 1),
		pong:      make(chan struct{}, 1),
		creditSig: make(chan struct{}, 1),
		done:      make(chan struct{}),
		started:   time.Now(),
	}
	s.ctrl = Control{s: s, sig: make(chan struct{}, 1)}
	s.wdog = time.AfterFunc(time.Hour, s.checkWrite)
	s.wdog.Stop()
	s.lastRecv.Store(time.Now().UnixNano())
	return s
}

// noteWrite arms the stall check after a record went out, unless it is armed already.
func (s *Session) noteWrite() {
	now := time.Now().UnixNano()
	s.lastWrite.Store(now)
	s.stallMu.Lock()
	defer s.stallMu.Unlock()
	if s.stall == nil || s.stallArmed {
		return
	}
	s.stallArmed, s.stallFrom, s.stallPinged = true, now, false
	s.stall.Reset(s.timing.Stall)
}

// checkStall runs Timing.Stall after a write: heard back since, it waits for a later unanswered write
// or disarms; not heard, it pings once, and ends the session the next time.
func (s *Session) checkStall() {
	s.stallMu.Lock()
	heard := s.lastRecv.Load()
	if heard >= s.stallFrom {
		if lw := s.lastWrite.Load(); lw > heard {
			s.stallFrom, s.stallPinged = lw, false
			s.stall.Reset(max(s.timing.Stall-time.Since(time.Unix(0, lw)), time.Millisecond))
		} else {
			s.stallArmed = false
		}
		s.stallMu.Unlock()
		return
	}
	if !s.stallPinged {
		s.stallPinged = true
		s.stall.Reset(s.timing.Stall)
		s.stallMu.Unlock()
		go s.ctrl.Send(map[string]string{"type": "ping"}) // may block on the dead path; the timer does not wait
		return
	}
	s.stallMu.Unlock()
	s.fail(ErrStalled)
}

// start runs the reader, the pinger and the idle watchdog once the hellos are through.
func (s *Session) start() {
	s.lastRecv.Store(time.Now().UnixNano())
	s.mu.Lock()
	s.idle = time.AfterFunc(s.timing.Idle, s.checkIdle)
	s.mu.Unlock()
	if s.initiator {
		s.stallMu.Lock()
		s.stall = time.AfterFunc(time.Hour, s.checkStall)
		s.stall.Stop()
		s.stallMu.Unlock()
	}
	go s.readLoop()
	go s.keepalive()
}

// Control is the JSON control stream (stream 0).
func (s *Session) Control() *Control { return &s.ctrl }

// Done is closed when the session has ended; Err says why.
func (s *Session) Done() <-chan struct{} { return s.done }

// Err is why the session ended, or nil while it is open.
func (s *Session) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

// RemoteStatic is the other side's static X25519 public key.
func (s *Session) RemoteStatic() [32]byte { return s.remote }

// ChannelBinding is the Noise handshake hash: the same on both sides of this session and on no
// other. An app can bind its own credentials to it.
func (s *Session) ChannelBinding() []byte { return append([]byte(nil), s.binding...) }

// Device is the device Admit named for this session; "" on the phone.
func (s *Session) Device() string { return s.device }

// Close ends the session. It sends no close for streams the application left
// open, so the peer reads ErrConnLost on them; frames already written (data, and
// closes the application sent) went out first. On the host it then sends bye
// stopping. Then the conn closes; it waits at most closeGrace for the bye.
func (s *Session) Close() error { return s.closeWith("stopping") }

// Revoked closes the session the way Close does, but the host's bye says
// "unpaired": the phone is no longer paired and should not reconnect. Only the
// host sends it; on the phone it is Close.
func (s *Session) Revoked() error { return s.Bye("unpaired") }

// Bye closes the session the way Close does, with this bye code from the host ("busy" when the host
// turns away a phone's extra session). On the phone it is Close.
func (s *Session) Bye(code string) error { return s.closeWith(code) }

// Started is when the session's handshake finished.
func (s *Session) Started() time.Time { return s.started }

func (s *Session) closeWith(code string) error {
	s.closeOnce.Do(func() {
		if s.isClosed() {
			return
		}
		// Streams the application did not close get no close frame: the peer must
		// see ErrConnLost on them, not a clean EOF. Frames already written (data and
		// the closes the application sent) went out before this.
		bound := time.AfterFunc(closeGrace, func() { s.fail(net.ErrClosed) })
		defer bound.Stop()
		if !s.initiator {
			s.ctrl.Send(byeMsg{Type: "bye", Code: code})
		}
	})
	s.fail(net.ErrClosed)
	return nil
}

func (s *Session) fail(err error) {
	s.once.Do(func() {
		s.mu.Lock()
		s.closed = true
		s.err = err
		idle := s.idle
		s.mu.Unlock()
		if idle != nil {
			idle.Stop()
		}
		s.wdog.Stop()
		s.stallMu.Lock()
		if s.stall != nil {
			s.stall.Stop()
		}
		s.stallMu.Unlock()
		close(s.done)
		s.conn.Close()
	})
}

func (s *Session) isClosed() bool {
	select {
	case <-s.done:
		return true
	default:
		return false
	}
}

func signal(c chan struct{}) {
	select {
	case c <- struct{}{}:
	default:
	}
}

// writeFrame encrypts one frame as one record and writes it. When the send key is due, a rekey
// frame goes out first under the old key, then the key changes, all under wmu, so the peer sees the
// rekey frame right before the first record under the new key.
func (s *Session) writeFrame(id uint32, typ byte, payload []byte) error {
	if len(payload) > maxFramePayload {
		return errors.New("secure: frame too long")
	}
	s.wmu.Lock()
	defer s.wmu.Unlock()
	if s.isClosed() {
		return s.Err()
	}
	if s.sent >= s.rekeyN || time.Since(s.sendFrom) >= s.rekeyT {
		var rk [frameHeaderLen]byte
		rk[4] = frameRekey
		if err := s.writeRecord(rk[:]); err != nil {
			return err
		}
		s.send.Rekey()
		s.sent, s.sendFrom = 0, time.Now()
	}
	plain := s.wplain
	binary.BigEndian.PutUint32(plain[0:4], id)
	plain[4] = typ
	binary.BigEndian.PutUint16(plain[5:7], uint16(len(payload)))
	n := frameHeaderLen + copy(plain[frameHeaderLen:], payload)
	return s.writeRecord(plain[:n])
}

// writeRecord encrypts plain as one record and writes it. Caller holds wmu.
func (s *Session) writeRecord(plain []byte) error {
	out, err := s.send.Encrypt(s.wbuf[:2], nil, plain)
	if err != nil {
		s.fail(err)
		return err
	}
	s.sent++
	binary.BigEndian.PutUint16(out[0:2], uint16(len(out)-2))
	now := time.Now()
	s.conn.SetWriteDeadline(now.Add(s.timing.Write))
	s.writeStart.Store(now.UnixNano())
	s.wdog.Reset(s.timing.Write) // for conns whose write deadline does nothing
	s.noteWrite()
	_, err = s.conn.Write(out)
	s.wdog.Stop()
	s.writeStart.Store(0)
	if err != nil {
		s.fail(lost(err))
		return s.Err()
	}
	return nil
}

// lost is how a session ends when the byte pipe under it broke. It is never
// io.EOF: a stream that got no close frame must not look finished.
func lost(err error) error {
	if errors.Is(err, ErrConnLost) {
		return err
	}
	if errors.Is(err, os.ErrDeadlineExceeded) {
		return fmt.Errorf("%w (%v)", ErrWriteStalled, err)
	}
	return fmt.Errorf("%w: %w (%v)", ErrConnLost, io.ErrUnexpectedEOF, err)
}

// readRecord reads and decrypts one record. The result is valid until the next call.
func (s *Session) readRecord() ([]byte, error) {
	if _, err := io.ReadFull(s.conn, s.rhdr[:]); err != nil {
		return nil, err
	}
	n := int(binary.BigEndian.Uint16(s.rhdr[:]))
	if n < tagLen+frameHeaderLen {
		return nil, ErrBadRecord
	}
	if _, err := io.ReadFull(s.conn, s.rbuf[:n]); err != nil {
		return nil, err
	}
	pt, err := s.recv.Decrypt(s.pbuf[:0], nil, s.rbuf[:n])
	if err != nil {
		return nil, ErrBadRecord
	}
	if s.received++; s.received > s.rejectN {
		return nil, fmt.Errorf("%w: %d records under one key", ErrProtocol, s.received)
	}
	s.lastRecv.Store(time.Now().UnixNano())
	return pt, nil
}

func parseFrame(pt []byte) (id uint32, typ byte, payload []byte, err error) {
	if len(pt) < frameHeaderLen {
		return 0, 0, nil, ErrProtocol
	}
	id = binary.BigEndian.Uint32(pt[0:4])
	typ = pt[4]
	if int(binary.BigEndian.Uint16(pt[5:7])) != len(pt)-frameHeaderLen {
		return 0, 0, nil, ErrProtocol
	}
	return id, typ, pt[frameHeaderLen:], nil
}

func (s *Session) readLoop() {
	for {
		pt, err := s.readRecord()
		if err != nil {
			if !errors.Is(err, ErrBadRecord) && !errors.Is(err, ErrProtocol) {
				err = lost(err)
			}
			s.fail(err)
			return
		}
		if err := s.handleFrame(pt); err != nil {
			s.fail(err)
			return
		}
	}
}

// handleFrame never writes and never waits on a reader: one stream nobody reads
// must not stop the others.
func (s *Session) handleFrame(pt []byte) error {
	id, typ, payload, err := parseFrame(pt)
	if err != nil {
		return err
	}
	if id == 0 {
		switch {
		case typ == frameRekey && len(payload) == 0:
			// Only after this record decrypted: the next one is under the rekeyed key.
			s.recv.Rekey()
			s.received = 0
			return nil
		case typ != frameData:
			return ErrProtocol
		}
		return s.ctrl.feed(payload)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if typ == frameOpen {
		if s.initiator || id%2 == 0 || id <= s.lastPeerID || len(payload) != 0 || len(s.streams) >= 2*maxStreams {
			// 2x: the phone forgets a closed stream at a different moment than we do, so it may briefly have more room than we think.
			return ErrProtocol
		}
		st := s.newStream(id)
		s.lastPeerID = id
		s.accepts = append(s.accepts, st)
		signal(s.acceptSig)
		return nil
	}
	st := s.streams[id]
	if st == nil {
		if typ == frameWindow && s.used(id) {
			return nil // credit for a stream both sides already closed
		}
		return ErrProtocol
	}
	switch typ {
	case frameData:
		if st.remoteClosed {
			return ErrProtocol
		}
		if uint32(len(payload)) > st.recvAvail {
			return ErrFlowControl
		}
		st.recvAvail -= uint32(len(payload))
		if !st.localClosed {
			if s.unread += len(payload); s.unread > maxUnread {
				return ErrFlowControl // the app reads none of it: stop holding the peer's bytes
			}
			st.buf.Write(payload)
			signal(st.rsig)
			break
		}
		// We closed this stream and drop what comes; give the credit back anyway,
		// or the peer's Write waits for window forever.
		st.unacked += uint32(len(payload))
		if st.unacked >= windowUpdateAt {
			s.credits = append(s.credits, credit{st.id, st.unacked})
			st.recvAvail += st.unacked
			st.unacked = 0
			signal(s.creditSig)
		}
	case frameClose:
		if st.remoteClosed || len(payload) != 0 {
			return ErrProtocol
		}
		st.remoteClosed = true
		s.forgetIfDone(st)
		signal(st.rsig)
	case frameWindow:
		if len(payload) != 4 {
			return ErrProtocol
		}
		st.sendWin += uint64(binary.BigEndian.Uint32(payload))
		if st.sendWin > maxWindow {
			return ErrProtocol
		}
		signal(st.wsig)
	default:
		return ErrProtocol
	}
	return nil
}

// used reports whether id was ever opened on this session. Caller holds s.mu.
func (s *Session) used(id uint32) bool {
	if s.initiator {
		return id%2 == 1 && id < s.nextID
	}
	return id%2 == 1 && id <= s.lastPeerID
}

func (s *Session) forgetIfDone(st *stream) {
	if st.writeClosed && st.remoteClosed {
		delete(s.streams, st.id)
	}
}

func (s *Session) streamCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.streams)
}

type credit struct {
	id  uint32
	inc uint32
}

// checkIdle ends the session when nothing came in for Idle.
func (s *Session) checkIdle() {
	next := s.timing.Idle - time.Since(time.Unix(0, s.lastRecv.Load()))
	if next <= 0 {
		s.fail(ErrIdle)
		return
	}
	s.mu.Lock()
	if !s.closed {
		s.idle.Reset(next)
	}
	s.mu.Unlock()
}

// checkWrite ends the session when one conn.Write has been stuck for
// Timing.Write: the peer pings, so Idle never fires, but does not read.
func (s *Session) checkWrite() {
	ws := s.writeStart.Load()
	if ws != 0 && time.Since(time.Unix(0, ws)) >= s.timing.Write {
		s.fail(ErrWriteStalled)
	}
}

func (s *Session) sendCredits() {
	s.mu.Lock()
	cs := s.credits
	s.credits = nil
	s.mu.Unlock()
	for _, c := range cs {
		var b [4]byte
		binary.BigEndian.PutUint32(b[:], c.inc)
		if s.writeFrame(c.id, frameWindow, b[:]) != nil {
			return
		}
	}
}

func (s *Session) keepalive() {
	t := time.NewTicker(s.timing.Ping)
	defer t.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-t.C:
			s.ctrl.Send(map[string]string{"type": "ping"})
		case <-s.pong:
			s.ctrl.Send(map[string]string{"type": "pong"})
		case <-s.creditSig:
			s.sendCredits()
		}
	}
}

// Open starts a stream. Only the phone opens streams.
func (s *Session) Open() (net.Conn, error) {
	if !s.initiator {
		return nil, errors.New("secure: only the phone opens streams")
	}
	s.openMu.Lock()
	defer s.openMu.Unlock()
	s.mu.Lock()
	if s.closed {
		err := s.err
		s.mu.Unlock()
		return nil, err
	}
	if len(s.streams) >= maxStreams {
		s.mu.Unlock()
		return nil, errors.New("secure: too many open streams")
	}
	id := s.nextID
	s.nextID += 2
	st := s.newStream(id)
	s.mu.Unlock()
	if err := s.writeFrame(id, frameOpen, nil); err != nil {
		return nil, err
	}
	return st, nil
}

// AcceptStream waits for the phone to open a stream.
func (s *Session) AcceptStream() (net.Conn, error) {
	for {
		s.mu.Lock()
		if len(s.accepts) > 0 {
			st := s.accepts[0]
			s.accepts = s.accepts[1:]
			s.mu.Unlock()
			return st, nil
		}
		if s.closed {
			err := s.err
			s.mu.Unlock()
			return nil, err
		}
		s.mu.Unlock()
		select {
		case <-s.acceptSig:
		case <-s.done:
		}
	}
}

// newStream registers a stream. Caller holds s.mu.
func (s *Session) newStream(id uint32) *stream {
	st := &stream{
		s:         s,
		id:        id,
		recvAvail: initialWindow,
		sendWin:   initialWindow,
		rsig:      make(chan struct{}, 1),
		wsig:      make(chan struct{}, 1),
	}
	s.streams[id] = st
	return st
}

// stream is one multiplexed connection. Its state is guarded by s.mu.
type stream struct {
	s  *Session
	id uint32

	wmu sync.Mutex // one Write (or the close frame) at a time on this stream

	buf          bytes.Buffer
	recvAvail    uint32 // bytes the peer may still send
	unacked      uint32 // read by the app, not yet given back
	sendWin      uint64 // bytes we may still send
	remoteClosed bool
	writeClosed  bool // we sent close: no more Writes
	localClosed  bool // Close: no more Writes or Reads
	readDL       time.Time
	writeDL      time.Time
	rsig, wsig   chan struct{}
}

func (st *stream) wait(sig chan struct{}, dl time.Time) error {
	var timer <-chan time.Time
	if !dl.IsZero() {
		d := time.Until(dl)
		if d <= 0 {
			return os.ErrDeadlineExceeded
		}
		t := time.NewTimer(d)
		defer t.Stop()
		timer = t.C
	}
	select {
	case <-sig:
	case <-st.s.done:
	case <-timer:
		return os.ErrDeadlineExceeded
	}
	return nil
}

func (st *stream) Read(p []byte) (int, error) {
	s := st.s
	for {
		s.mu.Lock()
		if st.localClosed {
			s.mu.Unlock()
			return 0, net.ErrClosed
		}
		if st.buf.Len() > 0 {
			n, _ := st.buf.Read(p)
			s.unread -= n
			st.unacked += uint32(n)
			var grant uint32
			if st.unacked >= windowUpdateAt && !st.remoteClosed {
				grant = st.unacked
				st.unacked = 0
				st.recvAvail += grant
			}
			s.mu.Unlock()
			if grant > 0 {
				var b [4]byte
				binary.BigEndian.PutUint32(b[:], grant)
				s.writeFrame(st.id, frameWindow, b[:]) // a failure ends the session; Read sees it next time
			}
			return n, nil
		}
		if st.remoteClosed {
			s.mu.Unlock()
			return 0, io.EOF
		}
		if s.closed {
			err := s.err
			s.mu.Unlock()
			return 0, err
		}
		dl := st.readDL
		s.mu.Unlock()
		if err := st.wait(st.rsig, dl); err != nil {
			return 0, err
		}
	}
}

func (st *stream) Write(p []byte) (int, error) {
	st.wmu.Lock()
	defer st.wmu.Unlock()
	s := st.s
	total := 0
	for len(p) > 0 {
		s.mu.Lock()
		if st.writeClosed {
			s.mu.Unlock()
			return total, net.ErrClosed
		}
		if s.closed {
			err := s.err
			s.mu.Unlock()
			return total, err
		}
		dl := st.writeDL
		if !dl.IsZero() && !time.Now().Before(dl) {
			s.mu.Unlock()
			return total, os.ErrDeadlineExceeded
		}
		if st.sendWin == 0 {
			s.mu.Unlock()
			if err := st.wait(st.wsig, dl); err != nil {
				return total, err
			}
			continue
		}
		n := min(len(p), maxFramePayload, int(st.sendWin))
		st.sendWin -= uint64(n)
		s.mu.Unlock()
		if err := s.writeFrame(st.id, frameData, p[:n]); err != nil {
			return total, err
		}
		total += n
		p = p[n:]
	}
	return total, nil
}

// CloseWrite says this side will not write again (the close frame) and keeps
// reading: the peer reads io.EOF and can still answer.
func (st *stream) CloseWrite() error {
	s := st.s
	s.mu.Lock()
	if st.writeClosed {
		s.mu.Unlock()
		return nil
	}
	st.writeClosed = true
	s.mu.Unlock()
	signal(st.wsig) // a Write waiting for window gives up and lets go of wmu
	st.wmu.Lock()
	err := s.writeFrame(st.id, frameClose, nil)
	st.wmu.Unlock()
	s.mu.Lock()
	s.forgetIfDone(st)
	s.mu.Unlock()
	if s.isClosed() {
		return nil
	}
	return err
}

// Close says this side will not write again and stops reading. The stream ends
// when the peer has closed too.
func (st *stream) Close() error {
	s := st.s
	s.mu.Lock()
	if st.localClosed {
		s.mu.Unlock()
		return nil
	}
	st.localClosed = true
	sendClose := !st.writeClosed
	st.writeClosed = true
	// What sat unread is dropped; give it back so the peer can keep writing
	// until it sees our close and stops.
	grant := st.unacked + uint32(st.buf.Len())
	s.unread -= st.buf.Len()
	st.buf.Reset()
	st.unacked = 0
	if st.remoteClosed {
		grant = 0
	}
	st.recvAvail += grant
	s.mu.Unlock()
	signal(st.rsig)
	signal(st.wsig) // a Write waiting for window gives up and lets go of wmu

	var err error
	if sendClose {
		st.wmu.Lock()
		err = s.writeFrame(st.id, frameClose, nil)
		st.wmu.Unlock()
	}
	if grant > 0 && err == nil {
		var b [4]byte
		binary.BigEndian.PutUint32(b[:], grant)
		err = s.writeFrame(st.id, frameWindow, b[:])
	}

	s.mu.Lock()
	s.forgetIfDone(st)
	s.mu.Unlock()
	if s.isClosed() {
		return nil
	}
	return err
}

func (st *stream) LocalAddr() net.Addr  { return st.s.conn.LocalAddr() }
func (st *stream) RemoteAddr() net.Addr { return st.s.conn.RemoteAddr() }

func (st *stream) SetDeadline(t time.Time) error {
	st.SetReadDeadline(t)
	return st.SetWriteDeadline(t)
}

func (st *stream) SetReadDeadline(t time.Time) error {
	st.s.mu.Lock()
	st.readDL = t
	st.s.mu.Unlock()
	signal(st.rsig)
	return nil
}

func (st *stream) SetWriteDeadline(t time.Time) error {
	st.s.mu.Lock()
	st.writeDL = t
	st.s.mu.Unlock()
	signal(st.wsig)
	return nil
}
