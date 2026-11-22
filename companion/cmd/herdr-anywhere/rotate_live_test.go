package main

// TestLiveRotateIdentity runs rotate-identity against a real serve --routes lan and a real phone (connectphone, in
// this process), on this computer alone: the connected phone hears 4401 (the app's "Pair it again"), serve moves its
// keys aside unread and starts again with new ones, an old link no longer gets in, and a new pairing works with a new
// host key. No key reaches any output. Opt in with HM_TAILCAT_LIVE=1, as for TestLiveLANOnly (needs hc, no internet).

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/jt-wang/tailpocket/connectphone"
	"github.com/jt-wang/tailpocket/connectsecure"

	"herdr-anywhere/companion/internal/store"
)

func TestLiveRotateIdentity(t *testing.T) {
	if os.Getenv("HM_TAILCAT_LIVE") != "1" {
		t.Skip("set HM_TAILCAT_LIVE=1 to rotate a real serve's identity on the local network (needs hc)")
	}
	hc := isolatedHC(t)
	dir := shortStateDir(t)
	// serve starts itself again in place (execSelf); here the test does, with the same arguments.
	restarted := make(chan struct{}, 1)
	keep := restartSelf
	restartSelf = func() error { restarted <- struct{}{}; return nil }
	t.Cleanup(func() { restartSelf = keep })
	args := []string{"--routes", "lan", "--listen", "127.0.0.1:0", "--hc", hc, "--state", dir}
	_, logs := startServe(t, args...)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	phone := connectphone.Options{Routes: []string{"lan"}}
	link := pairLiveWith(t, dir, "lan phone", logs, phone)
	var reply struct{ Token string }
	if err := json.Unmarshal([]byte(link.Payload), &reply); err != nil || reply.Token == "" {
		t.Fatalf("the pairing's payload has no token: %v", err)
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
	ws, err := dialHC(ctx, fwd.Addr()+fwd.BasePath(), reply.Token)
	if err != nil {
		t.Fatalf("WebSocket through the forwarder: %v\nserve:\n%s", err, logs)
	}
	defer ws.CloseNow()
	if err := ws.Write(ctx, websocket.MessageBinary, []byte(`{"jsonrpc":"2.0","id":0,"method":"hello","params":{"framed":true}}`+"\n")); err != nil {
		t.Fatal(err)
	}
	if _, got, err := ws.Read(ctx); err != nil || !strings.Contains(string(got), `"result"`) {
		t.Fatalf("hello: %v\nserve:\n%s", err, logs)
	}
	// Read to compare with the retired file, and to look for in every output; never printed.
	before, err := os.ReadFile(filepath.Join(dir, "tailcat.json"))
	if err != nil {
		t.Fatal(err)
	}
	var secrets struct{ Identity string }
	if json.Unmarshal(before, &secrets) != nil || secrets.Identity == "" {
		t.Fatal("tailcat.json holds no identity after a pairing")
	}

	type result struct {
		out, errs string
		code      int
	}
	rotated := make(chan result, 1)
	go func() { o, e, c := runCLI(t, "rotate-identity", "--yes", "--state", dir); rotated <- result{o, e, c} }()

	closeCtx, closeCancel := context.WithTimeout(ctx, 15*time.Second)
	_, _, err = ws.Read(closeCtx)
	closeCancel()
	if got := websocket.CloseStatus(err); got != 4401 {
		t.Fatalf("the connected phone's line ended with %v (status %d), want 4401, which the app says as Pair it again", err, got)
	}
	select {
	case <-restarted:
	case <-time.After(30 * time.Second):
		t.Fatalf("serve did not stop to start again\nserve:\n%s", logs)
	}
	_, logs2 := startServe(t, args...)
	var r result
	select {
	case r = <-rotated:
	case <-time.After(time.Minute):
		t.Fatal("rotate-identity did not return")
	}
	if r.code != 0 || !strings.Contains(r.out, "new identity") {
		t.Fatalf("rotate-identity exit %d:\n%s\n%s", r.code, r.out, r.errs)
	}

	retired, _ := filepath.Glob(filepath.Join(dir, "tailcat.json.retired-*"))
	if len(retired) != 1 {
		t.Fatalf("%d retired identities", len(retired))
	}
	// The phones are gone from it (serve saved that once they were unpaired); the identity is the old one.
	moved, err := os.ReadFile(retired[0])
	var kept struct {
		Identity string
		Allowed  []string
	}
	if err != nil || json.Unmarshal(moved, &kept) != nil || kept.Identity != secrets.Identity || len(kept.Allowed) != 0 {
		t.Fatalf("the retired file is not the old identity without its phones (%d phones): %v", len(kept.Allowed), err)
	}
	if fi, err := os.Stat(retired[0]); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("the retired file's mode: %v %v", fi.Mode(), err)
	}
	if _, err := os.Stat(filepath.Join(dir, "tailcat.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a tailcat.json before any new pairing: %v", err)
	}
	st, _ := store.Open(dir)
	if ds, err := st.Devices(); err != nil || len(ds) != 0 {
		t.Fatalf("%d phones still paired: %v", len(ds), err)
	}
	for name, s := range map[string]string{"rotate-identity's output": r.out + r.errs, "serve's log": logs.String() + logs2.String()} {
		if strings.Contains(s, secrets.Identity) || strings.Contains(s, "privkey:") {
			t.Fatalf("%s holds a key", name)
		}
	}

	// A phone that was not connected comes back with its old link: it no longer gets in, and can't tell why.
	old, err := connectphone.Connect(link, phone)
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	dialCtx, dialCancel := context.WithTimeout(ctx, 20*time.Second)
	c, err := old.Dial(dialCtx)
	dialCancel()
	if err == nil {
		c.Close()
		t.Fatal("an old link still reaches serve after rotate-identity")
	}
	t.Logf("an old link after rotate-identity: unreachable %v, handshake-closed %v, revoked %v",
		errors.Is(err, connectphone.ErrUnreachable), errors.Is(err, connectsecure.ErrHandshakeClosed), errors.Is(err, connectphone.ErrRevoked))

	link2 := pairLiveWith(t, dir, "lan phone", logs2, phone)
	if link2.HostKey == "" || link2.HostKey == link.HostKey {
		t.Fatal("the new pairing pins the same host key as before")
	}
}
