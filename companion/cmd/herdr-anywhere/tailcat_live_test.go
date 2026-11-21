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
	"slices"
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

// dialHC opens /hc through the forwarder at base (its address and BasePath), retrying while the tunnel comes up.
func dialHC(ctx context.Context, base, token string) (*websocket.Conn, error) {
	var last error
	for ctx.Err() == nil {
		attempt, cancel := context.WithTimeout(ctx, 20*time.Second)
		c, _, err := websocket.Dial(attempt, "ws://"+base+"/hc?token="+token, nil)
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
	inv, err := connectinvite.DecodeV2(strings.TrimSpace(string(text)), tailcat.Prefix2)
	if err != nil {
		t.Fatalf("the code on screen is not an invite: %v", err)
	}
	// The dialog asks about the emoji; the script presses its Pair button.
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	onSAS := func(emoji []string) { t.Logf("the phone shows %s", strings.Join(emoji, " ")) }
	if _, err := connectphone.PairV2(ctx, inv, connectphone.NewKey(), connectphone.Options{Device: "Test Pixel", OnSAS: onSAS}); err != nil {
		t.Fatalf("pair: %v", err)
	}
}

// pairLive pairs a phone (connectphone, in this process) with the serve on dir as the Add phone dialog does: pair
// --json for the code, and, once the phone shows its emoji and the window asks with the same ones, --answer yes.
func pairLive(t *testing.T, dir, device string, logs fmt.Stringer) connectphone.Link {
	t.Helper()
	return pairLiveWith(t, dir, device, logs, connectphone.Options{TailcatOnly: true})
}

// pairLiveWith is pairLive with the phone's own route options (opts' Device and OnSAS are pairLive's).
func pairLiveWith(t *testing.T, dir, device string, logs fmt.Stringer, opts connectphone.Options) connectphone.Link {
	t.Helper()
	out, errs, code := runCLI(t, "pair", "--json", "--state", dir)
	var win struct{ Invite, ID string }
	if code != 0 || json.Unmarshal([]byte(out), &win) != nil || win.ID == "" {
		// CI logs are public: never print the invite or the output holding it.
		t.Fatalf("pair --json exit %d (%d bytes): %s\nserve:\n%s", code, len(out), errs, logs)
	}
	inv, err := connectinvite.DecodeV2(win.Invite, tailcat.Prefix2)
	if err != nil {
		t.Fatalf("invite (%d chars) doesn't decode: %v", len(win.Invite), err)
	}
	for _, r := range inv.Via {
		if opts.Routes != nil && !slices.Contains(opts.Routes, r.T) {
			t.Fatalf("the invite lists a %s route; serve runs only %v", r.T, opts.Routes)
		}
	}
	answered := make(chan error, 1)
	onSAS := func(emoji []string) {
		go func() {
			var st tailcat.Status
			for range 50 {
				out, _, _ := runCLI(t, "pair", "--json", "--window", win.ID, "--state", dir)
				if json.Unmarshal([]byte(out), &st) == nil && st.State == tailcat.StateAsking {
					break
				}
				time.Sleep(100 * time.Millisecond)
			}
			if st.Request == nil || strings.Join(st.Request.Emoji, " ") != strings.Join(emoji, " ") || st.Request.Device != device {
				answered <- fmt.Errorf("the computer asks about %+v, the phone shows %q", st.Request, emoji)
				runCLI(t, "pair", "--json", "--window", win.ID, "--answer", "no", "--state", dir)
				return
			}
			out, errs, code := runCLI(t, "pair", "--json", "--window", win.ID, "--answer", "yes", "--state", dir)
			if code != 0 {
				answered <- fmt.Errorf("--answer yes: exit %d: %s %s", code, out, errs)
				return
			}
			answered <- nil
		}()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	opts.Device, opts.OnSAS = device, onSAS
	link, err := connectphone.PairV2(ctx, inv, connectphone.NewKey(), opts)
	select {
	case aerr := <-answered:
		if aerr != nil {
			t.Fatal(aerr)
		}
	default:
		if err == nil {
			t.Fatal("paired without the computer's answer")
		}
	}
	if err != nil {
		t.Fatalf("pair: %v\nserve:\n%s", err, logs)
	}
	return link
}

func TestLiveTailcat(t *testing.T) {
	if os.Getenv("HM_TAILCAT_LIVE") != "1" {
		t.Skip("set HM_TAILCAT_LIVE=1 to pair and connect over real Tailcat relays (needs internet and hc)")
	}
	hc := isolatedHC(t)
	dir := shortStateDir(t)
	// Both sides use only Tailcat, never LAN or a configured tailpocket-relay.
	_, logs := startServe(t, "--tailcat", "--tailcat-only", "--listen", "127.0.0.1:0", "--hc", hc, "--state", dir)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	started := time.Now()
	link := pairLive(t, dir, "live phone", logs)
	t.Logf("paired after %v", time.Since(started).Round(time.Millisecond))
	var reply struct{ Token, Device, Name string }
	if err := json.Unmarshal([]byte(link.Payload), &reply); err != nil || reply.Token == "" {
		t.Fatalf("payload %q: %v", link.Payload, err)
	}
	st, _ := store.Open(dir)
	phonePub, _ := connectphone.PublicKey(link.Key)
	if d, err := st.Authenticate(reply.Token); err != nil || d.ID != reply.Device || d.Name != "live phone" || d.TailcatKey != phonePub {
		t.Fatalf("payload token gives %+v, %v", d, err)
	}

	sess, err := connectphone.Connect(link, connectphone.Options{TailcatOnly: true})
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
	// The forward takes only what carries its token: everything goes under its BasePath, as the app's baseUrl does.
	base := fwd.Addr() + fwd.BasePath()
	ws, err := dialHC(ctx, base, reply.Token)
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
	if err := info(ctx, base, 25*time.Second); err != nil {
		t.Fatalf("/api/info through the forwarder before the revoke: %v", err)
	}

	// HM_TAILCAT_ROUTE=relay requires a real non-DNS UDP firewall block on both sides, not a debug switch.
	want := os.Getenv("HM_TAILCAT_ROUTE")
	var measured connectphone.Route
	waitFor(t, 20*time.Second, func() bool {
		pathCtx, pathCancel := context.WithTimeout(ctx, 3*time.Second)
		defer pathCancel()
		var perr error
		measured, perr = sess.Route(pathCtx)
		route, _ := measured.Path()
		return perr == nil && (want == "" || route == want)
	}, "the route "+want, logs)
	route, relay := measured.Path()
	t.Logf("route %q relay %q kind %q DERP region %d code %q host %q", route, relay, measured.Kind, measured.DERPRegion, measured.DERPCode, measured.DERPHost)
	if want == "relay" && (measured.Kind != connectphone.KindDERP || relay == "") {
		t.Fatalf("Tailcat-only blocked-UDP connection did not report a named DERP route: %+v", measured)
	}

	// An idle connection has no Herdr token watcher to close it. Removing the host's last phone must close
	// this connection too, not just the authenticated /hc socket (TailPocket v0.3.0 left it open).
	idle, err := sess.Dial(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer idle.Close()

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
	idle.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := idle.Read(make([]byte, 1)); err == nil {
		t.Fatal("revoked phone's idle connection received data")
	} else if e, ok := err.(interface{ Timeout() bool }); ok && e.Timeout() {
		t.Fatal("revoked last phone's idle connection stayed open")
	}
	t.Logf("idle connection closed %v after last-phone revoke", time.Since(started).Round(time.Millisecond))
	// /api/info needs no token, so it tells whether Tailcat still carries the
	// phone to serve at all (it did before the revoke, above). The device list
	// is the revoke: from here on, before serve takes the phone off Tailcat,
	// the phone must reach nothing.
	if b := reach(t, ctx, sess); len(b) > 0 {
		t.Fatalf("a revoked phone still reaches serve through Tailcat %v after revoke: %.80q", time.Since(started).Round(time.Millisecond), b)
	}
	waitFor(t, 15*time.Second, func() bool {
		b, _ := os.ReadFile(filepath.Join(dir, "tailcat.json"))
		return len(b) > 0 && !strings.Contains(string(b), phonePub)
	}, "serve to drop the phone from Tailcat", logs)
	t.Logf("phone off the Tailcat allowlist %v after revoke", time.Since(started).Round(time.Millisecond))
	if b := reach(t, ctx, sess); len(b) > 0 {
		t.Fatalf("a revoked phone still reaches serve through Tailcat %v after revoke: %.80q", time.Since(started).Round(time.Millisecond), b)
	}
}

// reach GETs /api/info over a new Tailcat connection and returns what came
// back: nothing when the dial is refused or serve closes the connection. It
// dials the session directly: in older TailPocket versions a failed dial
// behind the forwarder panics the process (the forwarder closed the typed-nil
// conn DialTCPPort returns with its error).
func reach(t *testing.T, ctx context.Context, sess *connectphone.Session) []byte {
	t.Helper()
	dialCtx, dialCancel := context.WithTimeout(ctx, 20*time.Second)
	defer dialCancel()
	c, err := sess.Dial(dialCtx)
	if err != nil {
		t.Logf("revoked phone refused: %v", err)
		return nil
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(10 * time.Second))
	if _, err := c.Write([]byte("GET /api/info HTTP/1.1\r\nHost: x\r\nConnection: close\r\n\r\n")); err != nil {
		t.Logf("revoked phone refused: %v", err)
		return nil
	}
	b, _ := io.ReadAll(c)
	if len(b) == 0 {
		t.Log("revoked phone refused: its connection carries nothing")
	}
	return b
}

// info GETs /api/info through the forwarder at base (its address and BasePath).
func info(ctx context.Context, base string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+base+"/api/info", nil)
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
