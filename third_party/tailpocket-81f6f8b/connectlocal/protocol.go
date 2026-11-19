package connectlocal

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"sync"
	"time"

	"github.com/jt-wang/tailpocket/connectinvite"
)

const defaultHelloTimeout = 5 * time.Second

// HostOptions configures one same-Wi-Fi pairing window.
type HostOptions struct {
	// Label separates apps: phone and host must use the same one. Required.
	Label string
	// Plaintext is what an approved phone receives, normally the invite's JSON form.
	Plaintext []byte
	// Expires closes the window.
	Expires time.Time
	// Confirm shows code and device to the host's user and returns their decision. ctx ends when
	// the phone disconnects or the window closes. A denial keeps the window open for another phone.
	Confirm func(ctx context.Context, code, device string) bool
	// Bindings are the addresses to listen on; nil means LANAddresses(). Peers outside the
	// binding's subnet are dropped without a reply.
	Bindings []Binding
	// HelloTimeout drops a peer that sends nothing (default 5 s).
	HelloTimeout time.Duration
	// OnListening receives the port once every binding listens; advertise it over Bonjour.
	OnListening func(port int)
}

// Serve runs one window, serving one phone at a time, until a phone is approved (nil), the window
// expires (ErrNotApproved) or ctx ends (ctx.Err()).
func Serve(ctx context.Context, opts HostOptions) error {
	if opts.Label == "" || opts.Confirm == nil || len(opts.Plaintext) == 0 || !time.Now().Before(opts.Expires) {
		return errors.New("connectlocal: Label, Plaintext, Confirm and a future Expires are required")
	}
	bindings := opts.Bindings
	if bindings == nil {
		bindings = LANAddresses()
	}
	if len(bindings) == 0 {
		return ErrUnavailable
	}
	hello := opts.HelloTimeout
	if hello <= 0 {
		hello = defaultHelloTimeout
	}
	window, cancel := context.WithDeadline(ctx, opts.Expires)
	defer cancel()

	var listeners []net.Listener
	port := 0
	for i, b := range bindings {
		ln, err := net.Listen("tcp4", net.JoinHostPort(b.IP.String(), itoa(port)))
		if err != nil {
			if i == 0 {
				return ErrUnavailable
			}
			continue // e.g. Ethernet beside Wi-Fi with the port taken: serve the others
		}
		if i == 0 {
			port = ln.Addr().(*net.TCPAddr).Port
		}
		listeners = append(listeners, ln)
	}

	s := &server{opts: opts, hello: hello, window: window, done: make(chan struct{})}
	var wg sync.WaitGroup
	for i, ln := range listeners {
		wg.Add(1)
		go func(ln net.Listener, b Binding) {
			defer wg.Done()
			s.accept(ln, b, &wg)
		}(ln, bindings[i])
	}
	if opts.OnListening != nil {
		opts.OnListening(port)
	}
	select {
	case <-window.Done():
	case <-s.done:
	}
	for _, ln := range listeners {
		_ = ln.Close()
	}
	cancel()
	wg.Wait()
	s.mu.Lock()
	approved := s.approved
	s.mu.Unlock()
	switch {
	case approved:
		return nil
	case ctx.Err() != nil:
		return ctx.Err()
	default:
		return ErrNotApproved
	}
}

type server struct {
	opts     HostOptions
	hello    time.Duration
	window   context.Context
	mu       sync.Mutex
	busy     bool
	approved bool
	done     chan struct{}
	doneOnce sync.Once
}

func (s *server) accept(ln net.Listener, b Binding, wg *sync.WaitGroup) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.handle(conn, b)
		}()
	}
}

func (s *server) expired() bool { return !time.Now().Before(s.opts.Expires) }

func (s *server) denial() map[string]any {
	reply := map[string]any{"status": "denied", "v": wireVersion}
	if s.expired() {
		reply["reason"] = "expired"
	}
	return reply
}

func (s *server) handle(conn net.Conn, b Binding) {
	defer conn.Close()
	remote, _ := conn.RemoteAddr().(*net.TCPAddr)
	if remote == nil || !b.Contains(remote.IP) {
		return // routed or tunneled peer: no reply at all
	}
	s.mu.Lock()
	if s.busy || s.approved || s.window.Err() != nil {
		s.mu.Unlock()
		_ = writeLine(conn, s.denial())
		return
	}
	s.busy = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.busy = false
		s.mu.Unlock()
	}()

	// A silent peer must not hold the single slot for the whole window.
	_ = conn.SetDeadline(minTime(time.Now().Add(s.hello), s.opts.Expires))
	reader := bufio.NewReaderSize(conn, MaxLine+1)
	line, err := readLine(reader)
	if err != nil {
		return
	}
	var msg struct {
		V         int    `json:"v"`
		PublicKey string `json:"publicKey"`
		Device    string `json:"device"`
	}
	if json.Unmarshal(line, &msg) != nil || msg.V != wireVersion {
		_ = writeLine(conn, s.denial())
		return
	}
	phoneKey, err := base64.StdEncoding.DecodeString(msg.PublicKey)
	if err != nil || len(phoneKey) != 32 || base64.StdEncoding.EncodeToString(phoneKey) != msg.PublicKey {
		_ = writeLine(conn, s.denial())
		return
	}
	device := connectinvite.CleanName(msg.Device)
	if device == "" {
		device = "Phone"
	}
	own, err := NewKeyPair()
	if err != nil {
		return
	}
	session, err := NewSession(s.opts.Label, own, phoneKey, false)
	if err != nil {
		_ = writeLine(conn, s.denial())
		return
	}
	_ = conn.SetDeadline(s.opts.Expires)
	if writeLine(conn, map[string]any{"publicKey": base64.StdEncoding.EncodeToString(own.Public[:]), "v": wireVersion}) != nil {
		return
	}

	// The host's prompt goes away when the phone leaves or the window closes.
	decide, stop := context.WithCancel(s.window)
	defer stop()
	go func() {
		_, _ = reader.ReadByte() // the phone sends nothing more; any return means it left
		stop()
	}()
	allowed := s.opts.Confirm(decide, session.Code, device)
	if !allowed || decide.Err() != nil || s.expired() {
		_ = writeLine(conn, s.denial())
		return
	}
	sealed, err := session.Seal(s.opts.Plaintext)
	if err != nil {
		_ = writeLine(conn, s.denial())
		return
	}
	s.mu.Lock()
	if s.approved {
		s.mu.Unlock()
		_ = writeLine(conn, s.denial())
		return
	}
	s.mu.Unlock()
	if writeLine(conn, map[string]any{"sealed": base64.StdEncoding.EncodeToString(sealed), "status": "approved", "v": wireVersion}) != nil {
		return
	}
	s.mu.Lock()
	s.approved = true
	s.mu.Unlock()
	s.doneOnce.Do(func() { close(s.done) })
}

// PhoneOptions configures the phone side.
type PhoneOptions struct {
	Label  string
	Device string
	// OnCode receives the 6-digit code to show while the host's user decides.
	OnCode func(code string)
}

// Pair connects to a host found over Bonjour (address is "ip:port") and returns the plaintext the
// host sends once its user approves.
func Pair(ctx context.Context, address string, opts PhoneOptions) ([]byte, error) {
	if opts.Label == "" {
		return nil, errors.New("connectlocal: Label is required")
	}
	conn, err := (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, "tcp", address)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, ErrFailed
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Unix(1, 0)) })
	defer stop()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	} else {
		_ = conn.SetDeadline(time.Now().Add(5 * time.Minute))
	}
	result, err := pairOn(conn, opts)
	if err != nil && ctx.Err() != nil && !errors.Is(err, ErrDenied) {
		return nil, ctx.Err()
	}
	return result, err
}

func pairOn(conn net.Conn, opts PhoneOptions) ([]byte, error) {
	own, err := NewKeyPair()
	if err != nil {
		return nil, ErrFailed
	}
	if err := writeLine(conn, map[string]any{"device": connectinvite.CleanName(opts.Device), "publicKey": base64.StdEncoding.EncodeToString(own.Public[:]), "v": wireVersion}); err != nil {
		return nil, mapNetErr(err)
	}
	reader := bufio.NewReaderSize(conn, MaxLine+1)
	hostHello, err := readMessage(reader)
	if err != nil {
		return nil, err
	}
	hostKey, err := base64.StdEncoding.DecodeString(hostHello.PublicKey)
	if err != nil || len(hostKey) != 32 {
		return nil, ErrFailed
	}
	session, err := NewSession(opts.Label, own, hostKey, true)
	if err != nil {
		return nil, ErrFailed
	}
	if opts.OnCode != nil {
		opts.OnCode(session.Code)
	}
	approval, err := readMessage(reader)
	if err != nil {
		return nil, err
	}
	if approval.Status != "approved" || approval.Sealed == "" {
		return nil, ErrFailed
	}
	box, err := base64.StdEncoding.DecodeString(approval.Sealed)
	if err != nil {
		return nil, ErrFailed
	}
	plain, err := session.Open(box)
	if err != nil {
		return nil, ErrFailed
	}
	return plain, nil
}

type message struct {
	V         int    `json:"v"`
	PublicKey string `json:"publicKey"`
	Status    string `json:"status"`
	Reason    string `json:"reason"`
	Sealed    string `json:"sealed"`
}

// readMessage reads one host line; a denial at any step becomes ErrDenied or ErrExpired.
func readMessage(r *bufio.Reader) (message, error) {
	line, err := readLine(r)
	if err != nil {
		return message{}, mapNetErr(err)
	}
	var m message
	if json.Unmarshal(line, &m) != nil || m.V != wireVersion {
		return message{}, ErrFailed
	}
	if m.Status == "denied" {
		if m.Reason == "expired" {
			return message{}, ErrExpired
		}
		return message{}, ErrDenied
	}
	return m, nil
}

func mapNetErr(err error) error {
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return ErrExpired
	}
	return ErrFailed
}

var errLineTooLong = errors.New("line too long")

func readLine(r *bufio.Reader) ([]byte, error) {
	line, err := r.ReadSlice('\n')
	if errors.Is(err, bufio.ErrBufferFull) || len(line) > MaxLine {
		return nil, errLineTooLong
	}
	if err != nil {
		return nil, err
	}
	return append([]byte{}, line[:len(line)-1]...), nil
}

// writeLine writes v as one JSON line; encoding/json sorts map keys, as the wire format requires.
func writeLine(conn net.Conn, v map[string]any) error {
	body, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = conn.Write(append(body, '\n'))
	return err
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
