package server

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"herdr-anywhere/companion/internal/testfake"
)

func wsURL(f *fixture, query string) string {
	return "ws" + strings.TrimPrefix(f.srv.URL, "http") + "/hc?" + query
}

func dial(t *testing.T, url string, header http.Header) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, r, err := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: header})
	if c != nil {
		c.SetReadLimit(1 << 24)
		t.Cleanup(func() { c.CloseNow() })
	}
	return c, r, err
}

func status(r *http.Response) int {
	if r == nil {
		return 0
	}
	return r.StatusCode
}

func withHC(t *testing.T) func(*Config) {
	hc := fakeHC(t)
	return func(c *Config) { c.HC = hc; c.HCVersion = "hc 9.9.9" }
}

func TestHcRejectsMissingUnknownAndRevokedTokens(t *testing.T) {
	f := newFixture(t, withHC(t))
	id, token := f.pair(t)
	if err := f.store.Revoke(id); err != nil {
		t.Fatal(err)
	}
	// The socket opens and closes with 4401: a browser or NSH client cannot read an HTTP status from a failed
	// handshake, and the phone must tell "pair again" from "the computer is unreachable".
	for _, q := range []string{"", "token=", "token=unknown", "token=" + token} {
		c, _, err := dial(t, wsURL(f, q), nil)
		if err != nil {
			t.Errorf("query %q: handshake failed: %v", q, err)
			continue
		}
		if code, _ := closeStatus(t, c, 3*time.Second); code != 4401 {
			t.Errorf("query %q: close code %v, want 4401", q, code)
		}
	}
}

func TestHcRejectsForeignOrigin(t *testing.T) {
	f := newFixture(t, withHC(t))
	_, token := f.pair(t)
	_, r, err := dial(t, wsURL(f, "token="+token), http.Header{"Origin": {"http://evil.example"}})
	if err == nil || status(r) != http.StatusForbidden {
		t.Fatalf("status %d err %v, want 403", status(r), err)
	}
	// Our own origin passes.
	c, _, err := dial(t, wsURL(f, "token="+token), http.Header{"Origin": {f.srv.URL}})
	if err != nil {
		t.Fatalf("own origin: %v", err)
	}
	c.Close(websocket.StatusNormalClosure, "")
}

func TestHcWithoutHcClosesWith4503(t *testing.T) {
	f := newFixture(t, nil)
	_, token := f.pair(t)
	c, _, err := dial(t, wsURL(f, "token="+token), nil)
	if err != nil {
		t.Fatalf("handshake failed: %v", err)
	}
	if code, reason := closeStatus(t, c, 3*time.Second); code != 4503 || !strings.Contains(reason, "herdr-core") {
		t.Fatalf("close %v %q, want 4503 naming herdr-core", code, reason)
	}
}

func readAll(t *testing.T, c *websocket.Conn, want int, typ websocket.MessageType, maxMsg int) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var got []byte
	for len(got) < want {
		mt, b, err := c.Read(ctx)
		if err != nil {
			t.Fatalf("read after %d of %d bytes: %v", len(got), want, err)
		}
		if mt != typ {
			t.Fatalf("message type %v, want %v", mt, typ)
		}
		if typ == websocket.MessageText {
			b, err = base64.StdEncoding.DecodeString(string(b))
			if err != nil {
				t.Fatalf("not base64: %v", err)
			}
		}
		if len(b) > maxMsg {
			t.Fatalf("message of %d bytes, max %d", len(b), maxMsg)
		}
		got = append(got, b...)
	}
	return got
}

func TestBinaryBytesPassThroughUnchangedInChunksOfAtMost64KiB(t *testing.T) {
	f := newFixture(t, withHC(t))
	_, token := f.pair(t)
	c, _, err := dial(t, wsURL(f, "token="+token), nil)
	if err != nil {
		t.Fatal(err)
	}
	msg := make([]byte, 200*1024)
	for i := range msg {
		msg[i] = byte(i * 7)
	}
	if err := c.Write(context.Background(), websocket.MessageBinary, msg); err != nil {
		t.Fatal(err)
	}
	got := readAll(t, c, len(msg), websocket.MessageBinary, 64*1024)
	if !bytes.Equal(got, msg) {
		t.Fatal("bytes changed on the way through")
	}
	// The log says which encoding a phone got, so a phone that fell back to base64 shows there.
	if !strings.Contains(f.logs.String(), "connected (binary,") {
		t.Fatalf("log does not say binary: %q", f.logs.String())
	}
}

func TestB64TextMessagesBothWays(t *testing.T) {
	f := newFixture(t, withHC(t))
	_, token := f.pair(t)
	c, _, err := dial(t, wsURL(f, "token="+token+"&enc=b64"), nil)
	if err != nil {
		t.Fatal(err)
	}
	msg := []byte("\x00\x01\x02hello\xff")
	if err := c.Write(context.Background(), websocket.MessageText, []byte(base64.StdEncoding.EncodeToString(msg))); err != nil {
		t.Fatal(err)
	}
	if got := readAll(t, c, len(msg), websocket.MessageText, 64*1024); !bytes.Equal(got, msg) {
		t.Fatalf("got %q", got)
	}
	if !strings.Contains(f.logs.String(), "connected (base64 text,") {
		t.Fatalf("log does not say base64: %q", f.logs.String())
	}
}

func closeStatus(t *testing.T, c *websocket.Conn, within time.Duration) (websocket.StatusCode, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), within)
	defer cancel()
	for {
		_, _, err := c.Read(ctx)
		if err == nil {
			continue
		}
		var ce websocket.CloseError
		if errors.As(err, &ce) {
			return ce.Code, ce.Reason
		}
		t.Fatalf("socket did not close within %v: %v", within, err)
	}
}

func TestWrongMessageTypeClosesTheSocket(t *testing.T) {
	f := newFixture(t, withHC(t))
	_, token := f.pair(t)
	c, _, err := dial(t, wsURL(f, "token="+token), nil)
	if err != nil {
		t.Fatal(err)
	}
	c.Write(context.Background(), websocket.MessageText, []byte("text in binary mode"))
	if code, _ := closeStatus(t, c, 3*time.Second); code != websocket.StatusUnsupportedData {
		t.Fatalf("close code %v, want 1003", code)
	}
}

func TestCoreExitClosesTheSocket(t *testing.T) {
	p := testfake.HC(t, t.TempDir(), "hc", testfake.Spec{Version: "hc 9", Connect: "print:bye"})
	f := newFixture(t, func(c *Config) { c.HC = p })
	_, token := f.pair(t)
	c, _, err := dial(t, wsURL(f, "token="+token), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := readAll(t, c, 3, websocket.MessageBinary, 64*1024); string(got) != "bye" {
		t.Fatalf("got %q", got)
	}
	code, reason := closeStatus(t, c, 3*time.Second)
	if reason != "core closed" {
		t.Fatalf("close %v %q, want reason \"core closed\"", code, reason)
	}
}

func TestSocketCloseEndsHcConnect(t *testing.T) {
	pidfile := filepath.Join(t.TempDir(), "pid")
	t.Setenv("HM_PIDFILE", pidfile)
	f := newFixture(t, withHC(t))
	_, token := f.pair(t)
	c, _, err := dial(t, wsURL(f, "token="+token), nil)
	if err != nil {
		t.Fatal(err)
	}
	var pid int
	for deadline := time.Now().Add(3 * time.Second); pid == 0 && time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		b, _ := os.ReadFile(pidfile)
		pid, _ = strconv.Atoi(strings.TrimSpace(string(b)))
	}
	if pid == 0 || !alive(pid) {
		t.Fatalf("hc connect not running (pid %d)", pid)
	}
	c.Close(websocket.StatusNormalClosure, "bye")
	for deadline := time.Now().Add(3 * time.Second); alive(pid); time.Sleep(20 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("hc connect (pid %d) still runs 3 s after the socket closed", pid)
		}
	}
}

func TestRevokeClosesLiveSocketsWithin3s(t *testing.T) {
	f := newFixture(t, withHC(t))
	id, token := f.pair(t)
	_, other := f.pair(t)
	c, _, err := dial(t, wsURL(f, "token="+token), nil)
	if err != nil {
		t.Fatal(err)
	}
	keep, _, err := dial(t, wsURL(f, "token="+other), nil)
	if err != nil {
		t.Fatal(err)
	}
	// Revoke through the store, as `herdr-anywhere revoke` in another process does.
	if err := f.store.Revoke(id); err != nil {
		t.Fatal(err)
	}
	if code, _ := closeStatus(t, c, 3*time.Second); code != 4401 {
		t.Fatalf("close code %v, want 4401", code)
	}
	// The other device stays connected.
	if err := keep.Write(context.Background(), websocket.MessageBinary, []byte("still")); err != nil {
		t.Fatal(err)
	}
	if got := readAll(t, keep, 5, websocket.MessageBinary, 64*1024); string(got) != "still" {
		t.Fatalf("got %q", got)
	}
}

// Over Tailcat, revoking a phone also restarts the node, which would cut its socket before the 4401 gets there (the
// iOS app then stayed on Reconnecting). Revoking says a revoked device's socket is still closing, for Tailcat to wait.
func TestRevokingLastsUntilTheRevokedSocketHasClosed(t *testing.T) {
	f := newFixture(t, withHC(t))
	id, token := f.pair(t)
	otherID, other := f.pair(t)
	c, _, err := dial(t, wsURL(f, "token="+token), nil)
	if err != nil {
		t.Fatal(err)
	}
	keep, _, err := dial(t, wsURL(f, "token="+other), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer keep.Close(websocket.StatusNormalClosure, "")
	for deadline := time.Now().Add(3 * time.Second); !strings.Contains(f.logs.String(), "device "+id+" connected") || !strings.Contains(f.logs.String(), "device "+otherID+" connected"); time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("not connected: %s", f.logs.String())
		}
	}
	if f.s.Revoking() {
		t.Fatal("revoking before any device was revoked")
	}
	if err := f.store.Revoke(id); err != nil {
		t.Fatal(err)
	}
	// The phone has not read the close yet, so its socket is still closing.
	if !f.s.Revoking() {
		t.Fatal("not revoking while the revoked device's socket is open")
	}
	if code, _ := closeStatus(t, c, 3*time.Second); code != 4401 {
		t.Fatalf("close code %v, want 4401", code)
	}
	for deadline := time.Now().Add(3 * time.Second); f.s.Revoking(); time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("still revoking 3 s after the socket closed")
		}
	}
}

func TestPingsLogHowLongTheLineTook(t *testing.T) {
	old := pingEvery
	pingEvery = 20 * time.Millisecond
	t.Cleanup(func() { pingEvery = old })
	f := newFixture(t, withHC(t))
	id, token := f.pair(t)
	c, _, err := dial(t, wsURL(f, "token="+token), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close(websocket.StatusNormalClosure, "")
	// The client side answers pings only while it reads.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go c.Read(ctx)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && !strings.Contains(f.logs.String(), "device "+id+" line ") {
		time.Sleep(10 * time.Millisecond)
	}
	if !strings.Contains(f.logs.String(), "device "+id+" line ") {
		t.Fatalf("no line time in the log: %s", f.logs.String())
	}
	// Quick answers after the first stay out of the log: a phone stays connected for days.
	time.Sleep(150 * time.Millisecond)
	if n := strings.Count(f.logs.String(), "device "+id+" line "); n != 1 {
		t.Fatalf("%d line times in the log, want the first only: %s", n, f.logs.String())
	}
}

func TestConnectUpdatesLastSeenAndLogsNoToken(t *testing.T) {
	f := newFixture(t, withHC(t))
	id, token := f.pair(t)
	before := time.Now()
	c, _, err := dial(t, wsURL(f, "token="+token), nil)
	if err != nil {
		t.Fatal(err)
	}
	c.Close(websocket.StatusNormalClosure, "")
	ds, _ := f.store.Devices()
	if len(ds) != 1 || ds[0].ID != id || ds[0].LastSeen.Before(before.Add(-time.Second)) {
		t.Fatalf("devices = %+v", ds)
	}
	if strings.Contains(f.logs.String(), token) {
		t.Fatal("log contains the token")
	}
}
