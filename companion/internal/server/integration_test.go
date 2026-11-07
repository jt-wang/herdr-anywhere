package server

// These tests run the real herdr-core through the companion, in a core of
// their own: HC_SESSION=hmtest-<random> with XDG_STATE_HOME, XDG_CONFIG_HOME
// and XDG_RUNTIME_DIR in scratch dirs, stopped with `hc server stop` at the
// end. They never reach the user's default core.

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"herdr-anywhere/companion/internal/hcbin"
	"herdr-anywhere/companion/internal/testfake"
)

const (
	kData   = 0
	kCredit = 1
	kClose  = 2
	kResize = 3
)

// realHC finds hc 0.1.x and points this test process (and so every hc the
// companion spawns) at a fresh, isolated core.
func realHC(t *testing.T) string {
	t.Helper()
	home, _ := os.UserHomeDir()
	hc, _, err := hcbin.Resolve(os.Getenv("HM_TEST_HC"), func(string) (string, error) { return "", exec.ErrNotFound }, home)
	if err != nil {
		t.Skipf("no hc for the integration test: %v", err)
	}
	b := make([]byte, 4)
	rand.Read(b)
	session := "hmtest-" + hex.EncodeToString(b)
	scratch := t.TempDir()
	// The core's sockets must fit macOS's 103-byte limit; t.TempDir() paths
	// are too long, so the runtime dir is a short one.
	run := testfake.ShortDir(t, "hm")
	env := map[string]string{
		"HC_SESSION":      session,
		"HC_SOCKET":       "",
		"XDG_STATE_HOME":  filepath.Join(scratch, "state"),
		"XDG_CONFIG_HOME": filepath.Join(scratch, "config"),
		"XDG_RUNTIME_DIR": run,
	}
	for k, v := range env {
		t.Setenv(k, v)
	}
	stopEnv := os.Environ()
	t.Cleanup(func() {
		cmd := exec.Command(hc, "server", "stop")
		cmd.Env = stopEnv
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Logf("hc server stop: %v: %s", err, out)
		}
	})
	return hc
}

// client speaks herdr-core's protocol over the companion's socket.
type client struct {
	t      *testing.T
	c      *websocket.Conn
	b64    bool
	buf    []byte            // bytes read but not yet parsed
	data   map[uint32][]byte // stream DATA by channel
	closed map[uint32]string // stream CLOSE payloads
	nextID int
}

func newClient(t *testing.T, f *fixture, token string, b64 bool) *client {
	t.Helper()
	q := "token=" + token
	if b64 {
		q += "&enc=b64"
	}
	c, r, err := dial(t, wsURL(f, q), nil)
	if err != nil {
		t.Fatalf("dial: status %d: %v", status(r), err)
	}
	return &client{t: t, c: c, b64: b64, data: map[uint32][]byte{}, closed: map[uint32]string{}, nextID: 1}
}

func (cl *client) send(b []byte) {
	cl.t.Helper()
	typ := websocket.MessageBinary
	if cl.b64 {
		typ, b = websocket.MessageText, []byte(base64.StdEncoding.EncodeToString(b))
	}
	if err := cl.c.Write(context.Background(), typ, b); err != nil {
		cl.t.Fatalf("write: %v", err)
	}
}

func (cl *client) more(ctx context.Context) error {
	typ, b, err := cl.c.Read(ctx)
	if err != nil {
		return err
	}
	if cl.b64 {
		if typ != websocket.MessageText {
			return fmt.Errorf("b64 mode got a %v message", typ)
		}
		if b, err = base64.StdEncoding.DecodeString(string(b)); err != nil {
			return err
		}
	} else if typ != websocket.MessageBinary {
		return fmt.Errorf("got a %v message", typ)
	}
	cl.buf = append(cl.buf, b...)
	return nil
}

func ctx10() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 10*time.Second)
}

// line reads the plain JSON line that answers hello.
func (cl *client) line() map[string]any {
	cl.t.Helper()
	ctx, cancel := ctx10()
	defer cancel()
	for {
		if i := bytes.IndexByte(cl.buf, '\n'); i >= 0 {
			var v map[string]any
			if err := json.Unmarshal(cl.buf[:i], &v); err != nil {
				cl.t.Fatalf("hello reply %q: %v", cl.buf[:i], err)
			}
			cl.buf = cl.buf[i+1:]
			return v
		}
		if err := cl.more(ctx); err != nil {
			cl.t.Fatalf("reading the hello reply: %v", err)
		}
	}
}

func (cl *client) hello(params map[string]any) map[string]any {
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 0, "method": "hello", "params": params})
	cl.send(append(b, '\n'))
	return cl.line()
}

func frame(channel uint32, kind byte, payload []byte) []byte {
	out := binary.LittleEndian.AppendUint32(nil, channel)
	out = append(out, kind)
	out = binary.LittleEndian.AppendUint32(out, uint32(len(payload)))
	return append(out, payload...)
}

// next parses one frame, reading more as needed. Stream frames are kept and
// give a nil message; channel 0 messages are returned.
func (cl *client) next(ctx context.Context) (map[string]any, error) {
	for {
		for len(cl.buf) >= 9 {
			ch := binary.LittleEndian.Uint32(cl.buf[0:4])
			kind := cl.buf[4]
			n := int(binary.LittleEndian.Uint32(cl.buf[5:9]))
			if len(cl.buf) < 9+n {
				break
			}
			p := append([]byte(nil), cl.buf[9:9+n]...)
			cl.buf = cl.buf[9+n:]
			switch {
			case ch == 0 && kind == kData:
				var v map[string]any
				if err := json.Unmarshal(p, &v); err != nil {
					return nil, fmt.Errorf("channel 0: %q: %v", p, err)
				}
				return v, nil
			case kind == kData:
				cl.data[ch] = append(cl.data[ch], p...)
				// Give the core credit back for what we consumed.
				cl.send(frame(ch, kCredit, binary.LittleEndian.AppendUint32(nil, uint32(n))))
				return nil, nil
			case kind == kClose:
				cl.closed[ch] = string(p)
				return nil, nil
			}
		}
		if err := cl.more(ctx); err != nil {
			return nil, err
		}
	}
}

func (cl *client) call(method string, params map[string]any) map[string]any {
	cl.t.Helper()
	id := cl.nextID
	cl.nextID++
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	cl.send(frame(0, kData, b))
	ctx, cancel := ctx10()
	defer cancel()
	for {
		msg, err := cl.next(ctx)
		if err != nil {
			cl.t.Fatalf("%s: %v", method, err)
		}
		if msg == nil {
			continue
		}
		if got, ok := msg["id"].(float64); ok && int(got) == id {
			if e, ok := msg["error"]; ok {
				cl.t.Fatalf("%s: error %v", method, e)
			}
			r, _ := msg["result"].(map[string]any)
			return r
		}
	}
}

// waitFor reads frames until channel ch's DATA contains want.
func (cl *client) waitFor(ch uint32, want string) {
	cl.t.Helper()
	ctx, cancel := ctx10()
	defer cancel()
	for !strings.Contains(string(cl.data[ch]), want) {
		if _, err := cl.next(ctx); err != nil {
			cl.t.Fatalf("waiting for %q on channel %d (have %q): %v", want, ch, cl.data[ch], err)
		}
	}
}

func generation(v map[string]any) float64 {
	r, _ := v["result"].(map[string]any)
	g, _ := r["generation"].(float64)
	return g
}

func TestRealCoreHelloRunAttachAndType(t *testing.T) {
	hc := realHC(t)
	f := newFixture(t, func(c *Config) { c.HC = hc })
	_, token := f.pair(t)
	cl := newClient(t, f, token, false)

	if g := generation(cl.hello(map[string]any{"framed": true})); g != 1 {
		t.Fatalf("hello generation = %v, want 1", g)
	}
	// A shell that prints hm-ok, then runs what is typed. The typed line's
	// output differs from the line itself, so it shows the line ran.
	argv, typed, ran := []string{"/bin/sh", "-c", "echo hm-ok; exec sh -i"}, "echo hm-$((40+2))\r", "hm-42"
	if runtime.GOOS == "windows" {
		argv, typed, ran = []string{"cmd.exe", "/k", "echo hm-ok"}, "echo hm-%OS%\r", "hm-Windows_NT"
	}
	run := cl.call("terminal.run", map[string]any{
		"argv": argv,
		"keep": true,
		"env":  map[string]string{"TERM": "xterm-256color"},
	})
	addr, _ := run["address"].(string)
	if addr == "" {
		t.Fatalf("terminal.run returned %v", run)
	}
	att := cl.call("terminal.attach", map[string]any{"terminal": addr, "cols": 80, "rows": 24})
	chf, ok := att["channel"].(float64)
	if !ok || chf == 0 {
		t.Fatalf("terminal.attach returned %v", att)
	}
	ch := uint32(chf)
	cl.waitFor(ch, "hm-ok")

	cl.send(frame(ch, kData, []byte(typed)))
	cl.waitFor(ch, ran)

	cl.send(frame(ch, kResize, []byte{100, 0, 30, 0}))
	cl.call("terminal.kill", map[string]any{"terminal": addr})
}

func TestRealCoreRejectsGeneration2(t *testing.T) {
	hc := realHC(t)
	f := newFixture(t, func(c *Config) { c.HC = hc })
	_, token := f.pair(t)
	cl := newClient(t, f, token, false)
	reply := cl.hello(map[string]any{"generation": 2, "framed": true})
	e, _ := reply["error"].(map[string]any)
	data, _ := e["data"].(map[string]any)
	if data["kind"] != "unsupported_generation" {
		t.Fatalf("hello generation 2 = %v, want error kind unsupported_generation", reply)
	}
}

func TestRealCoreHelloOverB64(t *testing.T) {
	hc := realHC(t)
	f := newFixture(t, func(c *Config) { c.HC = hc })
	_, token := f.pair(t)
	cl := newClient(t, f, token, true)
	if g := generation(cl.hello(map[string]any{"framed": true})); g != 1 {
		t.Fatalf("hello generation over b64 = %v, want 1", g)
	}
	// Framed traffic works over b64 too.
	if r := cl.call("terminal.list", map[string]any{}); r == nil {
		t.Fatal("terminal.list over b64 returned nothing")
	}
}

func TestRealCoreBadTokenAndForeignOrigin(t *testing.T) {
	hc := realHC(t)
	f := newFixture(t, func(c *Config) { c.HC = hc })
	_, token := f.pair(t)
	c, _, berr := dial(t, wsURL(f, "token=bad"), nil)
	if berr != nil {
		t.Fatalf("bad token: handshake failed: %v", berr)
	}
	if code, _ := closeStatus(t, c, 3*time.Second); code != 4401 {
		t.Fatalf("bad token: close code %v, want 4401", code)
	}
	_, r, err := dial(t, wsURL(f, "token="+token), map[string][]string{"Origin": {"http://evil.example"}})
	if err == nil || status(r) != 403 {
		t.Fatalf("foreign origin: status %d err %v", status(r), err)
	}
}

func TestRealCoreRevokeClosesTheSocket(t *testing.T) {
	hc := realHC(t)
	f := newFixture(t, func(c *Config) { c.HC = hc })
	id, token := f.pair(t)
	cl := newClient(t, f, token, false)
	if g := generation(cl.hello(map[string]any{"framed": true})); g != 1 {
		t.Fatalf("hello generation = %v", g)
	}
	start := time.Now()
	if err := f.store.Revoke(id); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	for {
		_, err := cl.next(ctx)
		var ce websocket.CloseError
		if errors.As(err, &ce) {
			if ce.Code != 4401 {
				t.Fatalf("close code %v", ce.Code)
			}
			t.Logf("closed %v after revoke", time.Since(start).Round(time.Millisecond))
			return
		}
		if err != nil {
			t.Fatalf("socket not closed with 4401 within 3 s: %v", err)
		}
	}
}
