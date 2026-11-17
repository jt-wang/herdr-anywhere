package server

import (
	"bufio"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"os/exec"
	"sync"
	"time"

	"github.com/coder/websocket"

	"herdr-anywhere/companion/internal/store"
)

const (
	// Largest stdout read sent as one message.
	chunk = 64 << 10
	// A WebSocket message may carry several frames of up to 16 MiB
	// (herdr-core wire.rs MAX_PAYLOAD), and base64 adds a third.
	readLimit = 32 << 20
	// Close code for a revoked device (4401).
	statusRevoked = 4401
	// Close code when hc is missing on the computer.
	statusNoCore = 4503
)

// How often a phone is asked to answer; a var so a test need not wait.
var pingEvery = 30 * time.Second

const slowLine = 300 * time.Millisecond

// session is one phone socket and the `hc connect` behind it.
type session struct {
	device string
	conn   *websocket.Conn
	cmd    *exec.Cmd
	once   sync.Once
}

// finish ends hc connect and closes the socket with code/reason. Only the
// first call counts; it can wait up to 5 s for the phone's close reply.
func (sess *session) finish(code websocket.StatusCode, reason string) {
	sess.once.Do(func() {
		if sess.cmd.Process != nil {
			_ = sess.cmd.Process.Kill()
		}
		_ = sess.conn.Close(code, reason)
	})
}

func (s *Server) hc(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		http.Error(w, "foreign origin", http.StatusForbidden)
		return
	}
	d, err := s.cfg.Store.Authenticate(r.URL.Query().Get("token"))
	if err != nil && !errors.Is(err, store.ErrUnknownToken) {
		s.log.Printf("hc: reading devices: %v", err)
		http.Error(w, "could not read the device list", http.StatusInternalServerError)
		return
	}
	b64 := r.URL.Query().Get("enc") == "b64"

	conn, aerr := websocket.Accept(w, r, &websocket.AcceptOptions{
		// sameOrigin above is the stricter check (it compares the scheme too).
		InsecureSkipVerify: true,
		CompressionMode:    websocket.CompressionDisabled,
	})
	if aerr != nil {
		return // Accept has answered
	}
	// A client cannot read the HTTP status of a failed handshake, so these open and close with a code the app reads.
	if err != nil {
		conn.Close(statusRevoked, "unknown device; pair again")
		return
	}
	if s.cfg.HC == "" {
		conn.Close(statusNoCore, "herdr-core's hc was not found on this computer")
		return
	}
	conn.SetReadLimit(readLimit)

	cmd := exec.Command(s.cfg.HC, "connect") // env inherited: HC_SESSION, HC_SOCKET, XDG_* pass through
	stdin, err1 := cmd.StdinPipe()
	stdout, err2 := cmd.StdoutPipe()
	stderr, err3 := cmd.StderrPipe()
	if err := errors.Join(err1, err2, err3); err != nil {
		s.log.Printf("hc connect: %v", err)
		conn.Close(websocket.StatusInternalError, "could not start hc connect")
		return
	}
	if err := cmd.Start(); err != nil {
		s.log.Printf("hc connect: %v", err)
		conn.Close(websocket.StatusInternalError, "could not start hc connect")
		return
	}
	sess := &session{device: d.ID, conn: conn, cmd: cmd}
	if !s.add(sess) {
		sess.finish(websocket.StatusGoingAway, "companion stopped")
		cmd.Wait()
		return
	}
	defer s.remove(sess)
	if err := s.cfg.Store.Touch(d.ID); err != nil {
		s.log.Printf("last seen: %v", err)
	}
	enc := "binary"
	if b64 {
		enc = "base64 text"
	}
	s.log.Printf("device %s connected (%s, hc pid %d)", d.ID, enc, cmd.Process.Pid)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var wg, pipes sync.WaitGroup
	wg.Add(2)
	pipes.Add(2)

	go func() { // hc's stderr into our log
		defer pipes.Done()
		sc := bufio.NewScanner(stderr)
		for sc.Scan() {
			s.log.Printf("hc connect [%s]: %s", d.ID, sc.Text())
		}
	}()

	go func() { // phone → hc stdin
		defer wg.Done()
		defer stdin.Close()
		want := websocket.MessageBinary
		if b64 {
			want = websocket.MessageText
		}
		for {
			typ, msg, err := conn.Read(ctx)
			if err != nil {
				sess.finish(websocket.StatusNormalClosure, "")
				return
			}
			if typ != want {
				if b64 {
					sess.finish(websocket.StatusUnsupportedData, "enc=b64 takes base64 text messages")
				} else {
					sess.finish(websocket.StatusUnsupportedData, "binary messages only (or connect with enc=b64)")
				}
				return
			}
			if b64 {
				msg, err = base64.StdEncoding.DecodeString(string(msg))
				if err != nil {
					sess.finish(websocket.StatusInvalidFramePayloadData, "bad base64")
					return
				}
			}
			if _, err := stdin.Write(msg); err != nil {
				sess.finish(websocket.StatusNormalClosure, "core closed")
				return
			}
		}
	}()

	go func() { // hc stdout → phone
		defer pipes.Done()
		buf := make([]byte, chunk)
		for {
			n, err := stdout.Read(buf)
			if n > 0 {
				typ, msg := websocket.MessageBinary, buf[:n]
				if b64 {
					typ, msg = websocket.MessageText, []byte(base64.StdEncoding.EncodeToString(buf[:n]))
				}
				if werr := conn.Write(ctx, typ, msg); werr != nil {
					sess.finish(websocket.StatusNormalClosure, "")
					io.Copy(io.Discard, stdout)
					return
				}
			}
			if err != nil {
				sess.finish(websocket.StatusNormalClosure, "core closed")
				return
			}
		}
	}()

	go func() { // a phone that vanished without closing (sleep, Wi-Fi gone)
		defer wg.Done()
		t := time.NewTicker(pingEvery)
		defer t.Stop()
		timed := false
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				pctx, pcancel := context.WithTimeout(ctx, 15*time.Second)
				start := time.Now()
				err := conn.Ping(pctx)
				pcancel()
				if err != nil {
					s.log.Printf("device %s no answer to a ping for %s", d.ID, time.Since(start).Round(time.Second))
					sess.finish(websocket.StatusGoingAway, "no pong")
					return
				}
				// The real line, for a report of lag: there and back through the tunnel and the phone's network. The first
				// answer and the slow ones, since a phone stays connected for days.
				if took := time.Since(start); !timed || took >= slowLine {
					timed = true
					s.log.Printf("device %s line %s", d.ID, took.Round(time.Millisecond))
				}
			}
		}
	}()

	// Both pipes must be drained before Wait, which closes them.
	pipes.Wait()
	err = cmd.Wait()
	sess.finish(websocket.StatusNormalClosure, "core closed")
	cancel()
	wg.Wait()
	if err := s.cfg.Store.Touch(d.ID); err != nil && !errors.Is(err, store.ErrNoDevice) {
		s.log.Printf("last seen: %v", err)
	}
	s.log.Printf("device %s disconnected (hc: %v) hc pid %d", d.ID, exitText(err), cmd.Process.Pid)
}

func exitText(err error) string {
	if err == nil {
		return "exited 0"
	}
	return err.Error()
}

// add registers a live session; false once the server is closing.
func (s *Server) add(sess *session) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		return false
	}
	s.sessions[sess] = struct{}{}
	s.active.Add(1)
	return true
}

func (s *Server) remove(sess *session) {
	s.mu.Lock()
	delete(s.sessions, sess)
	s.mu.Unlock()
	s.active.Done()
}

// Revoking says a revoked device's socket is still open: the 4401 that tells the phone to pair again is on its way, or
// the phone has not answered it yet. Over Tailcat, cutting the phone off restarts the node, which drops the socket with
// it, so the Tailcat side waits for this (tailcat.Service.Watch).
func (s *Server) Revoking() bool {
	s.mu.Lock()
	live := make([]string, 0, len(s.sessions))
	for sess := range s.sessions {
		live = append(live, sess.device)
	}
	s.mu.Unlock()
	if len(live) == 0 {
		return false
	}
	devices, err := s.cfg.Store.Devices()
	if err != nil {
		return false
	}
	paired := map[string]bool{}
	for _, d := range devices {
		paired[d.ID] = true
	}
	for _, id := range live {
		if !paired[id] {
			return true
		}
	}
	return false
}

// watchRevocations re-reads the device list while sockets are open and ends
// the sockets of devices no longer on it. `revoke` runs as another process,
// so the file is the only signal.
func (s *Server) watchRevocations() {
	t := time.NewTicker(s.cfg.RevokePoll)
	defer t.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-t.C:
		}
		s.mu.Lock()
		live := make([]*session, 0, len(s.sessions))
		for sess := range s.sessions {
			live = append(live, sess)
		}
		s.mu.Unlock()
		if len(live) == 0 {
			continue
		}
		devices, err := s.cfg.Store.Devices()
		if err != nil {
			s.log.Printf("revocation check: %v", err)
			continue
		}
		paired := map[string]bool{}
		for _, d := range devices {
			paired[d.ID] = true
		}
		for _, sess := range live {
			if !paired[sess.device] {
				s.log.Printf("device %s revoked: closing its socket", sess.device)
				go sess.finish(statusRevoked, "revoked")
			}
		}
	}
}
