package main

// TestProbeWhatAPhoneSeesWhenItCannotConnect measures, over Tailcat's real relays, what a paired phone's Tailcat
// client gets in the cases the app has to tell apart: serve answering (the known yes), serve stopped, serve running
// again with the same state (the old pairing must still work), and serve running with the phone revoked. For each it
// logs how long the handshake took, its error, and every Tailcat log line that differs. It decides nothing: it is the
// measurement the phone's "can't connect" text rests on. Opt in with HM_TAILCAT_PROBE=1 (needs internet and hc).

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	tc "github.com/tailscale/tailcat"
	"tailscale.com/types/key"

	"github.com/jt-wang/tailpocket/connectinvite"
	"github.com/jt-wang/tailpocket/connectphone"

	"herdr-anywhere/companion/internal/tailcat"
)

// stoppableServe runs serve until the returned func is called, and waits for it to end.
func stoppableServe(t *testing.T, args ...string) (stop func(), logs *syncBuffer) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	stdout, stderr := &syncBuffer{}, &syncBuffer{}
	done := make(chan int, 1)
	go func() { done <- runContext(ctx, append([]string{"serve"}, args...), stdout, stderr) }()
	waitFor(t, 30*time.Second, func() bool { return strings.Contains(stdout.String(), "state in ") }, "serve to start", stderr)
	var once sync.Once
	stop = func() {
		once.Do(func() {
			cancel()
			select {
			case <-done:
			case <-time.After(15 * time.Second):
				t.Error("serve did not stop")
			}
		})
	}
	t.Cleanup(stop)
	return stop, stderr
}

type probeResult struct {
	took time.Duration
	err  error
	logs []string
}

// handshake is what a fresh phone session does first (connectphone.Session.Dial → tailcat Client.up → Ping): a new
// client each time, as the app opens a new tunnel after one fails (app/src/link.ts dropTunnel).
func handshake(t *testing.T, link connectphone.Link) probeResult {
	t.Helper()
	var k key.NodePrivate
	if err := k.UnmarshalText([]byte(link.Key)); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var lines []string
	c := &tc.Client{Server: tc.Addr(link.Address), Key: k, Logf: func(f string, a ...any) {
		mu.Lock()
		defer mu.Unlock()
		lines = append(lines, strings.TrimSpace(fmt.Sprintf(f, a...)))
	}}
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	start := time.Now()
	_, err := c.Ping(ctx)
	r := probeResult{took: time.Since(start).Round(10 * time.Millisecond), err: err}
	time.Sleep(time.Second) // late lines, such as a relay's answer about the peer
	mu.Lock()
	r.logs = append(r.logs, lines...)
	mu.Unlock()
	return r
}

var noisy = regexp.MustCompile(`[0-9a-f]{16,}|\[[A-Za-z0-9+/=_-]{5}\]|\d+(\.\d+)?(ms|µs|s)\b|\d{1,3}(\.\d{1,3}){3}(:\d+)?|\[[0-9a-f:]+\](:\d+)?`)

// shape drops keys, addresses and times, so lines from two runs compare.
func shape(lines []string) map[string]bool {
	out := map[string]bool{}
	for _, l := range lines {
		out[noisy.ReplaceAllString(l, "…")] = true
	}
	return out
}

func TestProbeWhatAPhoneSeesWhenItCannotConnect(t *testing.T) {
	if os.Getenv("HM_TAILCAT_PROBE") != "1" {
		t.Skip("set HM_TAILCAT_PROBE=1 to measure, over real Tailcat relays, what a phone sees when it can't connect")
	}
	hc := isolatedHC(t)
	dir := shortStateDir(t)
	args := []string{"--tailcat", "--listen", "127.0.0.1:0", "--hc", hc, "--state", dir}
	stop, logs := stoppableServe(t, args...)

	out, errs, code := runCLI(t, "pair", "--state", dir)
	if code != 0 {
		t.Fatalf("pair exit %d: %s\nserve:\n%s", code, errs, logs)
	}
	inv, err := connectinvite.Decode(regexp.MustCompile(regexp.QuoteMeta(tailcat.Prefix)+`[A-Za-z0-9_-]+`).FindString(out), tailcat.Prefix)
	if err != nil {
		t.Fatalf("invite: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	link, err := connectphone.Pair(ctx, inv, connectphone.NewKey(), connectphone.Options{Device: "probe phone"})
	cancel()
	if err != nil {
		t.Fatalf("pair: %v\nserve:\n%s", err, logs)
	}
	var reply struct{ Device string }
	json.Unmarshal([]byte(link.Payload), &reply)

	report := func(name string, r probeResult) {
		t.Logf("== %s: took %v, err %v, %d log lines", name, r.took, r.err, len(r.logs))
	}
	known := handshake(t, link)
	report("serve running (known yes)", known)
	if known.err != nil {
		t.Fatalf("the instrument fails on the known yes: %v", known.err)
	}

	stop()
	stopped := handshake(t, link)
	report("serve stopped", stopped)

	stop, logs = stoppableServe(t, args...)
	again := handshake(t, link)
	report("serve started again, same state", again)

	if _, errs, code := runCLI(t, "revoke", "--state", dir, reply.Device); code != 0 {
		t.Fatalf("revoke: %s", errs)
	}
	waitFor(t, 15*time.Second, func() bool {
		b, _ := os.ReadFile(filepath.Join(dir, "tailcat.json"))
		pub, _ := connectphone.PublicKey(link.Key)
		return len(b) > 0 && !strings.Contains(string(b), pub)
	}, "serve to drop the phone from Tailcat", logs)
	revoked := handshake(t, link)
	report("serve running, phone revoked", revoked)
	stop()

	a, b := shape(stopped.logs), shape(revoked.logs)
	for l := range a {
		if !b[l] {
			t.Logf("only when serve is stopped: %s", l)
		}
	}
	for l := range b {
		if !a[l] {
			t.Logf("only when the phone is revoked: %s", l)
		}
	}
}
