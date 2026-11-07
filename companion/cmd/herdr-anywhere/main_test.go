package main

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"herdr-anywhere/companion/internal/store"
	"herdr-anywhere/companion/internal/tailcat"
	"herdr-anywhere/companion/internal/testfake"
)

func TestListenAddrs(t *testing.T) {
	lan := []netip.Addr{netip.MustParseAddr("192.168.1.5"), netip.MustParseAddr("10.0.0.2")}
	cases := []struct {
		name   string
		listen []string
		lan    bool
		want   []string
		err    bool
	}{
		{"default", nil, false, []string{"127.0.0.1:7681"}, false},
		{"lan adds private addresses on the same port", nil, true, []string{"127.0.0.1:7681", "192.168.1.5:7681", "10.0.0.2:7681"}, false},
		{"repeated --listen", []string{"127.0.0.1:9000", "[::1]:9000"}, false, []string{"127.0.0.1:9000", "[::1]:9000"}, false},
		{"lan takes the first --listen port", []string{"127.0.0.1:9000"}, true, []string{"127.0.0.1:9000", "192.168.1.5:9000", "10.0.0.2:9000"}, false},
		{"0.0.0.0 refused", []string{"0.0.0.0:7681"}, false, nil, true},
		{"empty host refused", []string{":7681"}, false, nil, true},
		{":: refused", []string{"[::]:7681"}, false, nil, true},
		{"no port refused", []string{"127.0.0.1"}, false, nil, true},
		{"duplicates dropped", []string{"192.168.1.5:7681"}, true, []string{"192.168.1.5:7681", "10.0.0.2:7681"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := listenAddrs(c.listen, c.lan, lan)
			if (err != nil) != c.err {
				t.Fatalf("err = %v, want error %v", err, c.err)
			}
			if !c.err && !slices.Equal(got, c.want) {
				t.Fatalf("got %v, want %v", got, c.want)
			}
		})
	}
}

func TestLanWithoutAddressesIsAnError(t *testing.T) {
	if _, err := listenAddrs(nil, true, nil); err == nil {
		t.Fatal("--lan with no LAN address must say so")
	}
}

func TestStateDir(t *testing.T) {
	if got := stateDir("/flag", "/env", "/home/u"); got != "/flag" {
		t.Fatal(got)
	}
	if got := stateDir("", "/env", "/home/u"); got != "/env" {
		t.Fatal(got)
	}
	if got := stateDir("", "", "/home/u"); got != filepath.Join("/home/u", ".config", "herdr-anywhere") {
		t.Fatal(got)
	}
}

func TestPairLink(t *testing.T) {
	if got := pairLink("http://192.168.1.5:7681/", "abc"); got != "http://192.168.1.5:7681/#pair=abc" {
		t.Fatal(got)
	}
	if got := pairLink("http://mac.tailnet.ts.net:7681", "abc"); got != "http://mac.tailnet.ts.net:7681/#pair=abc" {
		t.Fatal(got)
	}
}

func runCLI(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	var out, errb bytes.Buffer
	code := run(args, &out, &errb)
	return out.String(), errb.String(), code
}

func TestPairDevicesRevoke(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	t.Setenv("HERDR_ANYWHERE_STATE", dir)

	out, errs, code := runCLI(t, "pair", "--name", "My Phone", "--url", "http://10.0.0.2:7681")
	if code != 0 {
		t.Fatalf("pair exit %d: %s", code, errs)
	}
	m := regexp.MustCompile(`http://10\.0\.0\.2:7681/#pair=([A-Za-z0-9_-]{22,})`).FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("no pairing link in output:\n%s", out)
	}
	if !strings.Contains(out, "█") && !strings.Contains(out, "▀") && !strings.Contains(out, "▄") {
		t.Fatalf("no QR code in output:\n%s", out)
	}
	st, _ := store.Open(dir)
	d, _, err := st.Redeem(m[1], "")
	if err != nil || d.Name != "My Phone" {
		t.Fatalf("printed code does not pair: %+v %v", d, err)
	}

	out, _, code = runCLI(t, "devices")
	if code != 0 || !strings.Contains(out, d.ID) || !strings.Contains(out, "My Phone") {
		t.Fatalf("devices exit %d:\n%s", code, out)
	}

	if _, errs, code := runCLI(t, "revoke", d.ID); code != 0 {
		t.Fatalf("revoke exit %d: %s", code, errs)
	}
	if _, _, code := runCLI(t, "revoke", d.ID); code == 0 {
		t.Fatal("revoking an unknown id must fail")
	}
	out, _, _ = runCLI(t, "devices")
	if strings.Contains(out, d.ID) {
		t.Fatalf("revoked device still listed:\n%s", out)
	}
}

func TestStateFlagOverridesEnv(t *testing.T) {
	envDir := filepath.Join(t.TempDir(), "env")
	flagDir := filepath.Join(t.TempDir(), "flag")
	t.Setenv("HERDR_ANYWHERE_STATE", envDir)
	if _, errs, code := runCLI(t, "pair", "--state", flagDir, "--url", "http://x:1"); code != 0 {
		t.Fatal(errs)
	}
	if !fileExists(filepath.Join(flagDir, "state.json")) || fileExists(filepath.Join(envDir, "state.json")) {
		t.Fatal("--state did not win over HERDR_ANYWHERE_STATE")
	}
}

func TestUsage(t *testing.T) {
	if _, errs, code := runCLI(t); code != 2 || !strings.Contains(errs, "serve") {
		t.Fatalf("no args: exit %d %q", code, errs)
	}
	if _, _, code := runCLI(t, "bogus"); code != 2 {
		t.Fatalf("unknown command: exit %d", code)
	}
	if _, _, code := runCLI(t, "revoke"); code != 2 {
		t.Fatalf("revoke without id: exit %d", code)
	}
}

// shortStateDir holds serve's control socket, whose path must stay short.
func shortStateDir(t *testing.T) string {
	t.Helper()
	return testfake.ShortDir(t, "hmst")
}

// fakeTailcatServe answers pair the way serve --tailcat does, recording the
// pairing code it was given.
func fakeTailcatServe(t *testing.T, dir string, urls []string) *string {
	t.Helper()
	got := new(string)
	ln, err := tailcat.Listen(dir)
	if err != nil {
		t.Fatal(err)
	}
	open := func(_ context.Context, code string) (tailcat.Window, error) {
		*got = code
		return tailcat.Window{Invite: tailcat.Prefix + "INVITE", Expires: time.Now().Add(2 * time.Minute)}, nil
	}
	srv := &http.Server{Handler: tailcat.ControlHandler(open, urls)}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	return got
}

func TestPairShowsATailcatInviteWhenServeHasTailcat(t *testing.T) {
	dir := shortStateDir(t)
	got := fakeTailcatServe(t, dir, []string{"http://127.0.0.1:7681", "http://10.0.0.2:7681"})
	out, errs, code := runCLI(t, "pair", "--state", dir, "--name", "My Phone")
	if code != 0 {
		t.Fatalf("pair exit %d: %s", code, errs)
	}
	if !strings.Contains(out, "herdr1:INVITE") {
		t.Fatalf("no invite in output:\n%s", out)
	}
	if !strings.Contains(out, "▀") && !strings.Contains(out, "▄") {
		t.Fatalf("no QR code in output:\n%s", out)
	}
	// serve listens on the LAN, so the http link is printed too, with the same code.
	m := regexp.MustCompile(`http://10\.0\.0\.2:7681/#pair=([A-Za-z0-9_-]{22,})`).FindStringSubmatch(out)
	if m == nil || m[1] != *got {
		t.Fatalf("link code %v, serve got %q:\n%s", m, *got, out)
	}
	st, _ := store.Open(dir)
	if d, _, err := st.Redeem(*got, ""); err != nil || d.Name != "My Phone" {
		t.Fatalf("the code serve got does not pair: %+v %v", d, err)
	}
}

func TestPairOverTailcatOnlyPrintsNoLinkWhenServeIsLocalOnly(t *testing.T) {
	dir := shortStateDir(t)
	fakeTailcatServe(t, dir, []string{"http://127.0.0.1:7681"})
	out, errs, code := runCLI(t, "pair", "--state", dir)
	if code != 0 {
		t.Fatalf("pair exit %d: %s", code, errs)
	}
	if !strings.Contains(out, "herdr1:INVITE") || strings.Contains(out, "#pair=") {
		t.Fatalf("want the invite and no http link:\n%s", out)
	}
	// --url still names a link the phone can reach (a tailnet name, say).
	out, _, _ = runCLI(t, "pair", "--state", dir, "--url", "http://mac.example.ts.net:7681")
	if !strings.Contains(out, "http://mac.example.ts.net:7681/#pair=") {
		t.Fatalf("--url link missing:\n%s", out)
	}
}

// syncBuffer is an output sink tests read while serve writes.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// startServe runs serve in this process until the test ends.
func startServe(t *testing.T, args ...string) (stdout, stderr *syncBuffer) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	stdout, stderr = &syncBuffer{}, &syncBuffer{}
	done := make(chan int, 1)
	go func() { done <- runContext(ctx, append([]string{"serve"}, args...), stdout, stderr) }()
	t.Cleanup(func() {
		cancel()
		select {
		case code := <-done:
			if code != 0 {
				t.Errorf("serve exit %d: %s", code, stderr)
			}
		case <-time.After(15 * time.Second):
			t.Error("serve did not stop")
		}
	})
	waitFor(t, 30*time.Second, func() bool { return strings.Contains(stdout.String(), "state in ") }, "serve to start", stderr)
	return stdout, stderr
}

func waitFor(t *testing.T, d time.Duration, ok func() bool, what string, logs fmt.Stringer) {
	t.Helper()
	deadline := time.Now().Add(d)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s; logs:\n%s", what, logs)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestServeTailcatOpensTheControlSocket(t *testing.T) {
	dir := shortStateDir(t)
	startServe(t, "--tailcat", "--listen", "127.0.0.1:0", "--state", dir)
	fi, err := os.Stat(filepath.Join(dir, tailcat.SocketName))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode()&os.ModeSocket == 0 || (testfake.PermBits() && fi.Mode().Perm() != 0o600) {
		t.Fatalf("control socket mode = %v", fi.Mode())
	}
}

func TestServeWithoutTailcatIsUnchanged(t *testing.T) {
	dir := shortStateDir(t)
	startServe(t, "--listen", "127.0.0.1:0", "--state", dir)
	if fileExists(filepath.Join(dir, tailcat.SocketName)) {
		t.Fatal("serve without --tailcat opened the control socket")
	}
	// pair then works as before: a link, no invite.
	out, errs, code := runCLI(t, "pair", "--state", dir, "--url", "http://10.0.0.2:7681")
	if code != 0 || strings.Contains(out, tailcat.Prefix) || !strings.Contains(out, "http://10.0.0.2:7681/#pair=") {
		t.Fatalf("pair exit %d: %s\n%s", code, errs, out)
	}
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func TestPairBase(t *testing.T) {
	lan := []netip.Addr{netip.MustParseAddr("192.168.1.5")}
	cases := []struct {
		name string
		urls []string
		lan  []netip.Addr
		want string
	}{
		{"no serve running", nil, lan, "http://192.168.1.5:7681"},
		{"serve on the LAN", []string{"http://127.0.0.1:7781", "http://192.168.1.5:7781"}, lan, "http://192.168.1.5:7781"},
		{"serve on loopback only keeps its port", []string{"http://127.0.0.1:7781"}, lan, "http://192.168.1.5:7781"},
		{"no LAN address", []string{"http://127.0.0.1:7781"}, nil, "http://127.0.0.1:7781"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := pairBase(c.urls, c.lan); got != c.want {
				t.Fatalf("got %s, want %s", got, c.want)
			}
		})
	}
}

func TestPairLinksToThePortServeListensOn(t *testing.T) {
	dir := shortStateDir(t)
	stdout, _ := startServe(t, "--listen", "127.0.0.1:0", "--state", dir)
	port := regexp.MustCompile(`listening on http://127\.0\.0\.1:(\d+)`).FindStringSubmatch(stdout.String())
	if port == nil {
		t.Fatalf("no listen address in:\n%s", stdout)
	}
	out, errs, code := runCLI(t, "pair", "--state", dir)
	if code != 0 {
		t.Fatalf("pair exit %d: %s", code, errs)
	}
	if !regexp.MustCompile(`http://[0-9.]+:` + port[1] + `/#pair=`).MatchString(out) {
		t.Fatalf("link does not use serve's port %s:\n%s", port[1], out)
	}
}

func TestUsageNamesTheCommand(t *testing.T) {
	if !strings.HasPrefix(usage, "herdr-anywhere: ") || !strings.Contains(usage, "herdr-anywhere serve") {
		t.Fatalf("usage: %.60s", usage)
	}
}
