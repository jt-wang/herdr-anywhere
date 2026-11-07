package tailcat

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jt-wang/tailpocket/connecthost"
	"github.com/tailscale/tailcat"
	"tailscale.com/types/key"

	"herdr-anywhere/companion/internal/store"
	"herdr-anywhere/companion/internal/testfake"
)

// newStore opens a store in a short dir: the control socket lives there, and
// macOS caps socket paths at 104 bytes, which t.TempDir() exceeds.
func newStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(testfake.ShortDir(t, "hmtc"))
	if err != nil {
		t.Fatal(err)
	}
	return st
}

// identityText builds a host identity in connecthost's format
// (connecthost/identity.go: {"v":1,"key":…,"psk":…}) without starting a node.
func identityText(t *testing.T) string {
	t.Helper()
	k, _ := key.NewNode().MarshalText()
	p, _ := tailcat.NewPresharedKey().MarshalText()
	b, _ := json.Marshal(map[string]any{"v": 1, "key": string(k), "psk": string(p)})
	return string(b)
}

func phoneKey() string { return key.NewNode().Public().String() }

func pairTailcat(t *testing.T, st *store.Store, phone string) store.Device {
	t.Helper()
	code, err := st.CreateCode("Phone", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	d, _, err := st.RedeemTailcat(code, "phone", phone)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func writeState(t *testing.T, st *store.Store, s connecthost.State) {
	t.Helper()
	b, _ := json.Marshal(s)
	if err := os.WriteFile(filepath.Join(st.Dir(), stateName), b, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestOpenRestoresStateAndDropsRevokedPhones(t *testing.T) {
	st := newStore(t)
	kept, revoked := phoneKey(), phoneKey()
	pairTailcat(t, st, kept)
	writeState(t, st, connecthost.State{Identity: identityText(t), Allowed: []string{kept, revoked}})
	s, err := Open(st, "Mac", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if got := s.host.Allowed(); !slices.Equal(got, []string{kept}) {
		t.Fatalf("allowed = %v, want only the phone that still has a device", got)
	}
}

// The file holds the host's private key and the paired phones: a broken one
// is an error, never silently replaced by a new identity.
func TestOpenRefusesABrokenStateFile(t *testing.T) {
	st := newStore(t)
	if err := os.WriteFile(filepath.Join(st.Dir(), stateName), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(st, "Mac", nil); err == nil {
		t.Fatal("a broken state file was accepted")
	}
}

func TestSaveWritesTheStateReadableOnlyByTheUser(t *testing.T) {
	st := newStore(t)
	s, err := Open(st, "Mac", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	want := connecthost.State{Identity: identityText(t), Allowed: []string{phoneKey()}}
	if err := s.save(want); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(st.Dir(), stateName)
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if testfake.PermBits() && fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, want 0600", fi.Mode().Perm())
	}
	var got connecthost.State
	b, _ := os.ReadFile(p)
	if err := json.Unmarshal(b, &got); err != nil || got.Identity != want.Identity || !slices.Equal(got.Allowed, want.Allowed) {
		t.Fatalf("saved %s (%v)", b, err)
	}
}

func TestPayloadIssuesOneDeviceTokenPerPairing(t *testing.T) {
	st := newStore(t)
	code, _ := st.CreateCode("Phone", time.Minute)
	iss := &issuer{store: st, code: code, name: "Mac"}
	phone := phoneKey()
	out, err := iss.payload(connecthost.Phone{Key: phone, Device: "Pixel 9"})
	if err != nil {
		t.Fatal(err)
	}
	var reply struct{ Token, Device, Name string }
	if err := json.Unmarshal([]byte(out), &reply); err != nil {
		t.Fatalf("payload %q: %v", out, err)
	}
	d, err := st.Authenticate(reply.Token)
	if err != nil || d.ID != reply.Device || d.Name != "Pixel 9" || d.TailcatKey != phone || reply.Name != "Mac" {
		t.Fatalf("payload %q gives device %+v (%v)", out, d, err)
	}
	// A phone whose reply got lost retries; it must get the same token, not a second device.
	again, err := iss.payload(connecthost.Phone{Key: phone, Device: "Pixel 9"})
	if err != nil || again != out {
		t.Fatalf("retry = %q, %v; want the first payload", again, err)
	}
	if _, err := iss.payload(connecthost.Phone{Key: phoneKey(), Device: "other"}); err == nil {
		t.Fatal("a second phone got a token from the same pairing")
	}
	if ds, _ := st.Devices(); len(ds) != 1 {
		t.Fatalf("devices = %+v, want one", ds)
	}
}

// Tailcat runs Payload before it admits the phone (connecthost/pairing.go:230-240);
// when the window ends without the phone joining, the token is taken back.
func TestUndoForgetsAPairingThatDidNotFinish(t *testing.T) {
	st := newStore(t)
	code, _ := st.CreateCode("Phone", time.Minute)
	iss := &issuer{store: st, code: code, name: "Mac"}
	iss.undo() // nothing issued yet: nothing to do
	if _, err := iss.payload(connecthost.Phone{Key: phoneKey(), Device: "p"}); err != nil {
		t.Fatal(err)
	}
	iss.undo()
	if ds, _ := st.Devices(); len(ds) != 0 {
		t.Fatalf("devices = %+v, want none", ds)
	}
}

type fakeAllowlist struct {
	mu      sync.Mutex
	allowed []string
	revoked []string
}

func (f *fakeAllowlist) Allowed() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.allowed)
}

func (f *fakeAllowlist) Revoke(_ context.Context, k string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.revoked = append(f.revoked, k)
	f.allowed = slices.DeleteFunc(f.allowed, func(a string) bool { return a == k })
	return nil
}

func TestPruneRevokesPhonesWhoseDeviceIsGone(t *testing.T) {
	st := newStore(t)
	kept, gone := phoneKey(), phoneKey()
	pairTailcat(t, st, kept)
	d := pairTailcat(t, st, gone)
	if err := st.Revoke(d.ID); err != nil {
		t.Fatal(err)
	}
	f := &fakeAllowlist{allowed: []string{kept, gone}}
	if err := prune(context.Background(), f, st, nil, nil); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(f.revoked, []string{gone}) || !slices.Equal(f.allowed, []string{kept}) {
		t.Fatalf("revoked %v, left %v", f.revoked, f.allowed)
	}
}

// Cutting the phone off Tailcat restarts the node, which drops the socket the server is closing with 4401: the phone
// would never hear it was revoked. While `hold` says such a socket is open, the phone stays allowed.
func TestPruneWaitsWhileARevokedPhonesSocketCloses(t *testing.T) {
	st := newStore(t)
	k := phoneKey()
	d := pairTailcat(t, st, k)
	if err := st.Revoke(d.ID); err != nil {
		t.Fatal(err)
	}
	f := &fakeAllowlist{allowed: []string{k}}
	closing := true
	hold := func() bool { return closing }
	if err := prune(context.Background(), f, st, nil, hold); err != nil {
		t.Fatal(err)
	}
	if len(f.revoked) != 0 {
		t.Fatalf("revoked %v while its socket was closing", f.revoked)
	}
	closing = false
	if err := prune(context.Background(), f, st, nil, hold); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(f.revoked, []string{k}) {
		t.Fatalf("revoked %v once the socket closed, want %v", f.revoked, []string{k})
	}
}

// --- control socket ------------------------------------------------------

func serveControl(t *testing.T, dir string, h http.Handler) net.Listener {
	t.Helper()
	ln, err := Listen(dir)
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: h}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	return ln
}

func TestPairRequestReachesServe(t *testing.T) {
	st := newStore(t)
	var got string
	open := func(_ context.Context, code string) (Window, error) {
		got = code
		return Window{Invite: Prefix + "abc", Expires: time.Unix(1_900_000_000, 0)}, nil
	}
	serveControl(t, st.Dir(), ControlHandler(open, []string{"http://192.168.1.5:7681"}))
	fi, err := os.Stat(filepath.Join(st.Dir(), SocketName))
	if err != nil {
		t.Fatal(err)
	}
	if testfake.PermBits() && fi.Mode().Perm() != 0o600 {
		t.Fatalf("socket mode = %v, want 0600", fi.Mode().Perm())
	}
	r, err := RequestPair(context.Background(), st.Dir(), "the-code")
	if err != nil {
		t.Fatal(err)
	}
	if got != "the-code" || r.Invite != Prefix+"abc" || !r.Expires.Equal(time.Unix(1_900_000_000, 0)) ||
		!slices.Equal(r.URLs, []string{"http://192.168.1.5:7681"}) {
		t.Fatalf("reply %+v, serve got code %q", r, got)
	}
}

func TestPairRequestCarriesServesError(t *testing.T) {
	st := newStore(t)
	open := func(context.Context, string) (Window, error) { return Window{}, errors.New("no relay reachable") }
	serveControl(t, st.Dir(), ControlHandler(open, nil))
	_, err := RequestPair(context.Background(), st.Dir(), "c")
	if err == nil || errors.Is(err, ErrNoServe) || !strings.Contains(err.Error(), "no relay reachable") {
		t.Fatalf("err = %v", err)
	}
}

func TestPairRequestWithoutServe(t *testing.T) {
	st := newStore(t)
	if _, err := RequestPair(context.Background(), st.Dir(), "c"); !errors.Is(err, ErrNoServe) {
		t.Fatalf("no socket: err = %v, want ErrNoServe", err)
	}
	// A serve that crashed leaves its socket file behind.
	stale(t, st.Dir())
	if _, err := RequestPair(context.Background(), st.Dir(), "c"); !errors.Is(err, ErrNoServe) {
		t.Fatalf("stale socket: err = %v, want ErrNoServe", err)
	}
}

func stale(t *testing.T, dir string) {
	t.Helper()
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: filepath.Join(dir, SocketName), Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	ln.SetUnlinkOnClose(false)
	ln.Close()
}

func TestListenRefusesASecondServeAndReplacesAStaleSocket(t *testing.T) {
	st := newStore(t)
	stale(t, st.Dir())
	ln, err := Listen(st.Dir())
	if err != nil {
		t.Fatalf("stale socket not replaced: %v", err)
	}
	defer ln.Close()
	if _, err := Listen(st.Dir()); err == nil {
		t.Fatal("a second serve took over the control socket")
	}
}
