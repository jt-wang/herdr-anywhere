package main

// TestLiveTailcat runs a phone (connectphone, in this process) against
// `serve --tailcat` over Tailcat's real relays, the way the Herdr app will:
// scan the invite `pair` prints, pair, Connect + Forward, then open the /hc
// WebSocket on the 127.0.0.1 forwarder. Needs outbound internet and
// herdr-core's hc; opt in with HM_TAILCAT_LIVE=1.

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/jt-wang/tailpocket/connectinvite"
	"github.com/jt-wang/tailpocket/connectphone"

	"herdr-anywhere/companion/internal/hcbin"
	"herdr-anywhere/companion/internal/store"
	"herdr-anywhere/companion/internal/tailcat"
	"herdr-anywhere/companion/internal/testfake"
)

// isolatedHC points this process, and every hc serve spawns, at a fresh core
// of its own (as internal/server's integration tests do).
func isolatedHC(t *testing.T) string {
	t.Helper()
	home, _ := os.UserHomeDir()
	hc, _, err := hcbin.Resolve(os.Getenv("HM_TEST_HC"), exec.LookPath, home)
	if err != nil {
		t.Fatalf("HM_TAILCAT_LIVE needs herdr-core's hc: %v", err)
	}
	b := make([]byte, 4)
	rand.Read(b)
	scratch := t.TempDir()
	run := testfake.ShortDir(t, "hm") // the core's sockets must fit macOS's 104 bytes
	for k, v := range map[string]string{
		"HC_SESSION": "hmtest-" + hex.EncodeToString(b), "HC_SOCKET": "",
		"XDG_STATE_HOME": filepath.Join(scratch, "state"), "XDG_CONFIG_HOME": filepath.Join(scratch, "config"),
		"XDG_RUNTIME_DIR": run,
	} {
		t.Setenv(k, v)
	}
	env := os.Environ()
	t.Cleanup(func() {
		cmd := exec.Command(hc, "server", "stop")
		cmd.Env = env
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Logf("hc server stop: %v: %s", err, out)
		}
	})
	return hc
}

// dialHC opens /hc through the forwarder, retrying while the tunnel comes up.
func dialHC(ctx context.Context, addr, token string) (*websocket.Conn, error) {
	var last error
	for ctx.Err() == nil {
		attempt, cancel := context.WithTimeout(ctx, 20*time.Second)
		c, _, err := websocket.Dial(attempt, "ws://"+addr+"/hc?token="+token, nil)
		cancel()
		if err == nil {
			return c, nil
		}
		last = err
		time.Sleep(time.Second)
	}
	return nil, errors.Join(ctx.Err(), last)
}

// TestPairFromScreen is the phone's half of app/e2e/desktop-add-phone.sh: it
// pairs over Tailcat's real relays with the invite read off the Add phone
// dialog's pixels. The script names the file holding it.
func TestPairFromScreen(t *testing.T) {
	path := os.Getenv("HM_SCREEN_INVITE")
	if path == "" {
		t.Skip("run by app/e2e/desktop-add-phone.sh")
	}
	text, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	inv, err := connectinvite.Decode(strings.TrimSpace(string(text)), tailcat.Prefix)
	if err != nil {
		t.Fatalf("the code on screen is not an invite: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if _, err := connectphone.Pair(ctx, inv, connectphone.NewKey(), connectphone.Options{Device: "Test Pixel"}); err != nil {
		t.Fatalf("pair: %v", err)
	}
}

func TestLiveTailcat(t *testing.T) {
	if os.Getenv("HM_TAILCAT_LIVE") != "1" {
		t.Skip("set HM_TAILCAT_LIVE=1 to pair and connect over real Tailcat relays (needs internet and hc)")
	}
	hc := isolatedHC(t)
	dir := shortStateDir(t)
	_, logs := startServe(t, "--tailcat", "--listen", "127.0.0.1:0", "--hc", hc, "--state", dir)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	started := time.Now()
	out, errs, code := runCLI(t, "pair", "--state", dir)
	if code != 0 {
		t.Fatalf("pair exit %d: %s\nserve:\n%s", code, errs, logs)
	}
	text := regexp.MustCompile(regexp.QuoteMeta(tailcat.Prefix) + `[A-Za-z0-9_-]+`).FindString(out)
	t.Logf("pairing window open after %v; invite %d chars", time.Since(started).Round(time.Millisecond), len(text))
	inv, err := connectinvite.Decode(text, tailcat.Prefix)
	if err != nil {
		// CI logs are public: never print the invite or the output holding it.
		t.Fatalf("invite (%d chars) doesn't decode: %v", len(text), err)
	}

	started = time.Now()
	phoneKey := connectphone.NewKey()
	pairCtx, pairCancel := context.WithTimeout(ctx, 90*time.Second)
	link, err := connectphone.Pair(pairCtx, inv, phoneKey, connectphone.Options{Device: "live phone"})
	pairCancel()
	if err != nil {
		t.Fatalf("pair: %v\nserve:\n%s", err, logs)
	}
	t.Logf("paired after %v", time.Since(started).Round(time.Millisecond))
	var reply struct{ Token, Device, Name string }
	if err := json.Unmarshal([]byte(link.Payload), &reply); err != nil || reply.Token == "" {
		t.Fatalf("payload %q: %v", link.Payload, err)
	}
	st, _ := store.Open(dir)
	phonePub, _ := connectphone.PublicKey(phoneKey)
	if d, err := st.Authenticate(reply.Token); err != nil || d.ID != reply.Device || d.Name != "live phone" || d.TailcatKey != phonePub {
		t.Fatalf("payload token gives %+v, %v", d, err)
	}

	sess, err := connectphone.Connect(link, connectphone.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	fwd, err := sess.Forward()
	if err != nil {
		t.Fatal(err)
	}
	defer fwd.Close()
	started = time.Now()
	ws, err := dialHC(ctx, fwd.Addr(), reply.Token)
	if err != nil {
		t.Fatalf("WebSocket through the forwarder: %v\nserve:\n%s", err, logs)
	}
	defer ws.CloseNow()
	t.Logf("WebSocket open after %v", time.Since(started).Round(time.Millisecond))

	hello := []byte(`{"jsonrpc":"2.0","id":0,"method":"hello","params":{"framed":true}}` + "\n")
	if err := ws.Write(ctx, websocket.MessageBinary, hello); err != nil {
		t.Fatal(err)
	}
	var got []byte
	readCtx, readCancel := context.WithTimeout(ctx, 30*time.Second)
	for !bytes.Contains(got, []byte("\n")) {
		typ, b, err := ws.Read(readCtx)
		if err != nil {
			t.Fatalf("hello reply: %v\nserve:\n%s", err, logs)
		}
		if typ != websocket.MessageBinary {
			t.Fatalf("got a %v message", typ)
		}
		got = append(got, b...)
	}
	readCancel()
	t.Logf("hello answered after %v: %.120s", time.Since(started).Round(time.Millisecond), got)
	if !strings.Contains(string(got), `"result"`) {
		t.Fatalf("hello reply %q", got)
	}
	if !strings.Contains(logs.String(), "connected (binary,") {
		t.Fatalf("serve did not log a binary connection:\n%s", logs)
	}
	if err := info(ctx, fwd.Addr(), 25*time.Second); err != nil {
		t.Fatalf("/api/info through the forwarder before the revoke: %v", err)
	}

	// How the tunnel travels, as Settings shows it. HM_TAILCAT_ROUTE names what must come back, so one run
	// expects direct, and one with TS_DEBUG_ALWAYS_USE_DERP=1 expects relay.
	want := os.Getenv("HM_TAILCAT_ROUTE")
	var route, relay string
	waitFor(t, 20*time.Second, func() bool {
		pathCtx, pathCancel := context.WithTimeout(ctx, 3*time.Second)
		defer pathCancel()
		var perr error
		route, relay, perr = sess.Path(pathCtx)
		return perr == nil && (want == "" || route == want)
	}, "the route "+want, logs)
	t.Logf("route %q relay %q", route, relay)

	// Revoke from another "process": the live socket closes with 4401, and
	// serve takes the phone off Tailcat, so it can no longer reach serve.
	started = time.Now()
	if _, errs, code := runCLI(t, "revoke", "--state", dir, reply.Device); code != 0 {
		t.Fatalf("revoke exit %d: %s", code, errs)
	}
	closeCtx, closeCancel := context.WithTimeout(ctx, 10*time.Second)
	_, _, err = ws.Read(closeCtx)
	closeCancel()
	if errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("the revoked phone's WebSocket stayed open")
	}
	// 4401 when the token check closes it first; the node restart that the
	// Tailcat revoke causes may cut the connection before that frame arrives.
	t.Logf("socket closed %v after revoke: status %d (%v)", time.Since(started).Round(time.Millisecond), websocket.CloseStatus(err), err)
	waitFor(t, 15*time.Second, func() bool {
		b, _ := os.ReadFile(filepath.Join(dir, "tailcat.json"))
		return len(b) > 0 && !strings.Contains(string(b), phonePub)
	}, "serve to drop the phone from Tailcat", logs)
	t.Logf("phone off the Tailcat allowlist %v after revoke", time.Since(started).Round(time.Millisecond))
	// /api/info needs no token, so it tells whether Tailcat still carries the
	// phone to serve at all (it did before the revoke, above). This dials the
	// session directly: at 905975f a failed dial behind the forwarder panics
	// the process (connectphone/forward.go:74-77 closes the typed-nil conn
	// DialTCPPort returns with its error; seen here 2026-10-30).
	dialCtx, dialCancel := context.WithTimeout(ctx, 20*time.Second)
	defer dialCancel()
	c, err := sess.Dial(dialCtx)
	if err != nil {
		t.Logf("revoked phone refused: %v", err)
		return
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(10 * time.Second))
	if _, err := c.Write([]byte("GET /api/info HTTP/1.1\r\nHost: x\r\nConnection: close\r\n\r\n")); err == nil {
		if b, _ := io.ReadAll(c); len(b) > 0 {
			t.Fatalf("a revoked phone still reaches serve through Tailcat: %.80q", b)
		}
	}
	t.Log("revoked phone refused: its connection carries nothing")
}

// info GETs /api/info through the forwarder.
func info(ctx context.Context, addr string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+"/api/info", nil)
	client := &http.Client{Transport: &http.Transport{DisableKeepAlives: true}}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	return nil
}
