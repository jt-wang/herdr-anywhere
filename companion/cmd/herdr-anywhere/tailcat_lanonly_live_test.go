package main

// TestLiveLANOnly is TestLiveTailcat without Tailcat: serve --routes lan and a phone that may use only the local
// network pair with the emoji, connect, talk to the core and see a revoke, all on this computer. It needs hc but
// no internet. Nothing may go through Tailcat: the invite lists only the local network, serve says so, and on
// Linux every socket the test process (serve and the phone) still holds is on loopback or this computer's own
// addresses, with no UDP at all (Tailcat's WireGuard). app/e2e/no-tailcat-packets.sh also counts, with iptables,
// every packet the run sends off the machine. Opt in with HM_TAILCAT_LIVE=1, as for TestLiveTailcat.

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/jt-wang/tailpocket/connectphone"
)

func TestLiveLANOnly(t *testing.T) {
	if os.Getenv("HM_TAILCAT_LIVE") != "1" {
		t.Skip("set HM_TAILCAT_LIVE=1 to pair and connect on the local network alone (needs hc)")
	}
	hc := isolatedHC(t)
	dir := shortStateDir(t)
	checkSockets := noTailcatSockets(t)
	out, logs := startServe(t, "--routes", "lan", "--listen", "127.0.0.1:0", "--hc", hc, "--state", dir)
	if s := out.String(); !strings.Contains(s, "listening for phones on the local network, not through Tailcat") {
		t.Fatalf("serve --routes lan says:\n%s", s)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	phone := connectphone.Options{Routes: []string{"lan"}}
	link := pairLiveWith(t, dir, "lan phone", logs, phone)
	var reply struct{ Token, Device string }
	if err := json.Unmarshal([]byte(link.Payload), &reply); err != nil || reply.Token == "" {
		t.Fatalf("payload %q: %v", link.Payload, err)
	}
	sess, err := connectphone.Connect(link, phone)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	fwd, err := sess.Forward()
	if err != nil {
		t.Fatal(err)
	}
	defer fwd.Close()
	base := fwd.Addr() + fwd.BasePath()
	ws, err := dialHC(ctx, base, reply.Token)
	if err != nil {
		t.Fatalf("WebSocket through the forwarder: %v\nserve:\n%s", err, logs)
	}
	defer ws.CloseNow()
	if err := ws.Write(ctx, websocket.MessageBinary, []byte(`{"jsonrpc":"2.0","id":0,"method":"hello","params":{"framed":true}}`+"\n")); err != nil {
		t.Fatal(err)
	}
	readCtx, readCancel := context.WithTimeout(ctx, 30*time.Second)
	_, got, err := ws.Read(readCtx)
	readCancel()
	if err != nil || !strings.Contains(string(got), `"result"`) {
		t.Fatalf("hello reply %q: %v\nserve:\n%s", got, err, logs)
	}
	pathCtx, pathCancel := context.WithTimeout(ctx, 5*time.Second)
	route, err := sess.Route(pathCtx)
	pathCancel()
	if err != nil || route.Kind != connectphone.KindLAN {
		t.Fatalf("the phone's route is %+v (%v), want the local network", route, err)
	}
	checkSockets()

	if _, errs, code := runCLI(t, "revoke", "--state", dir, reply.Device); code != 0 {
		t.Fatalf("revoke exit %d: %s", code, errs)
	}
	closeCtx, closeCancel := context.WithTimeout(ctx, 10*time.Second)
	_, _, err = ws.Read(closeCtx)
	closeCancel()
	if errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("the revoked phone's WebSocket stayed open")
	}
	if b := reach(t, ctx, sess); len(b) > 0 {
		t.Fatalf("a revoked phone still reaches serve on the local network: %.80q", b)
	}
	if strings.Contains(strings.ToLower(logs.String()), "derp") {
		t.Fatalf("serve --routes lan logged DERP:\n%s", logs)
	}
}
