package store

import (
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"herdr-anywhere/companion/internal/testfake"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func newStore(t *testing.T) (*Store, *clock) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "state")
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	c := &clock{t: time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)}
	s.now = c.now
	return s, c
}

func decoded(t *testing.T, s string) []byte {
	t.Helper()
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		t.Fatalf("%q is not base64url: %v", s, err)
	}
	return b
}

func stateFile(t *testing.T, s *Store) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(s.dir, stateName))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestCodeIsRandomBase64URLAndOnlyItsHashIsStored(t *testing.T) {
	s, _ := newStore(t)
	a, err := s.CreateCode("Phone", 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := s.CreateCode("Phone", 10*time.Minute)
	if len(decoded(t, a)) < 16 {
		t.Fatalf("code %q has fewer than 128 bits", a)
	}
	if a == b {
		t.Fatal("two codes are equal")
	}
	file := stateFile(t, s)
	if strings.Contains(file, a) {
		t.Fatal("state file contains the plain code")
	}
	if !strings.Contains(file, hash(a)) {
		t.Fatal("state file lacks the code's SHA-256")
	}
}

func TestRedeemGivesATokenWhoseHashIsStored(t *testing.T) {
	s, c := newStore(t)
	code, _ := s.CreateCode("Phone", 10*time.Minute)
	d, token, err := s.Redeem(code, "Pixel 9")
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded(t, token)) < 32 {
		t.Fatalf("token %q has fewer than 256 bits", token)
	}
	if d.Name != "Pixel 9" || d.ID == "" || !d.Created.Equal(c.t) {
		t.Fatalf("device = %+v", d)
	}
	if strings.Contains(stateFile(t, s), token) {
		t.Fatal("state file contains the plain token")
	}
	got, err := s.Authenticate(token)
	if err != nil || got.ID != d.ID {
		t.Fatalf("Authenticate = %+v, %v", got, err)
	}
}

func TestRedeemWithoutDeviceNameUsesTheCodesName(t *testing.T) {
	s, _ := newStore(t)
	code, _ := s.CreateCode("Phone", 10*time.Minute)
	d, _, err := s.Redeem(code, "")
	if err != nil || d.Name != "Phone" {
		t.Fatalf("device = %+v, %v", d, err)
	}
}

func TestCodeIsSingleUse(t *testing.T) {
	s, _ := newStore(t)
	code, _ := s.CreateCode("Phone", 10*time.Minute)
	if _, _, err := s.Redeem(code, "a"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Redeem(code, "b"); !errors.Is(err, ErrCodeGone) {
		t.Fatalf("second redeem err = %v, want ErrCodeGone", err)
	}
}

func TestCodeExpires(t *testing.T) {
	s, c := newStore(t)
	code, _ := s.CreateCode("Phone", 10*time.Minute)
	c.t = c.t.Add(10*time.Minute + time.Second)
	if _, _, err := s.Redeem(code, "a"); !errors.Is(err, ErrCodeGone) {
		t.Fatalf("err = %v, want ErrCodeGone", err)
	}
}

func TestUnknownCodeAndTokenAreRejected(t *testing.T) {
	s, _ := newStore(t)
	if _, _, err := s.Redeem("nope", "a"); !errors.Is(err, ErrBadCode) {
		t.Fatalf("err = %v, want ErrBadCode", err)
	}
	if _, err := s.Authenticate("nope"); !errors.Is(err, ErrUnknownToken) {
		t.Fatalf("err = %v, want ErrUnknownToken", err)
	}
	if _, err := s.Authenticate(""); !errors.Is(err, ErrUnknownToken) {
		t.Fatalf("empty token err = %v, want ErrUnknownToken", err)
	}
}

func TestRevokeRemovesTheDevice(t *testing.T) {
	s, _ := newStore(t)
	code, _ := s.CreateCode("Phone", 10*time.Minute)
	d, token, _ := s.Redeem(code, "a")
	if err := s.Revoke(d.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Authenticate(token); !errors.Is(err, ErrUnknownToken) {
		t.Fatalf("revoked token err = %v", err)
	}
	if err := s.Revoke(d.ID); !errors.Is(err, ErrNoDevice) {
		t.Fatalf("second revoke err = %v, want ErrNoDevice", err)
	}
	ds, _ := s.Devices()
	if len(ds) != 0 {
		t.Fatalf("devices = %+v", ds)
	}
}

func TestTouchUpdatesLastSeen(t *testing.T) {
	s, c := newStore(t)
	code, _ := s.CreateCode("Phone", 10*time.Minute)
	d, _, _ := s.Redeem(code, "a")
	c.t = c.t.Add(time.Hour)
	if err := s.Touch(d.ID); err != nil {
		t.Fatal(err)
	}
	ds, _ := s.Devices()
	if len(ds) != 1 || !ds[0].LastSeen.Equal(c.t) {
		t.Fatalf("devices = %+v", ds)
	}
}

func TestPermissions(t *testing.T) {
	s, _ := newStore(t)
	if _, err := s.CreateCode("Phone", time.Minute); err != nil {
		t.Fatal(err)
	}
	di, _ := os.Stat(s.dir)
	if testfake.PermBits() && di.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode = %v", di.Mode().Perm())
	}
	fi, _ := os.Stat(filepath.Join(s.dir, stateName))
	if testfake.PermBits() && fi.Mode().Perm() != 0o600 {
		t.Fatalf("file mode = %v", fi.Mode().Perm())
	}
}

func TestOpenTightensAnExistingLooseDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(dir); err != nil {
		t.Fatal(err)
	}
	di, _ := os.Stat(dir)
	if testfake.PermBits() && di.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode = %v", di.Mode().Perm())
	}
}

// pair and serve are separate processes on one state dir: none of their
// writes may be lost.
func TestConcurrentWritersFromSeparateStoresLoseNothing(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	const n = 20
	codes := make([]string, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, err := Open(dir)
			if err != nil {
				t.Error(err)
				return
			}
			codes[i], err = s.CreateCode("p", time.Minute)
			if err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	s, _ := Open(dir)
	for _, c := range codes {
		if _, _, err := s.Redeem(c, "x"); err != nil {
			t.Fatalf("code lost: %v", err)
		}
	}
}

// A phone paired through Tailcat keeps its Tailcat key with the device, so
// revoking the device can take the phone off the Tailcat allowlist too.
func TestRedeemTailcatKeepsThePhonesKey(t *testing.T) {
	s, _ := newStore(t)
	code, _ := s.CreateCode("Phone", 10*time.Minute)
	d, token, err := s.RedeemTailcat(code, "Pixel 9", "nodekey:abc")
	if err != nil {
		t.Fatal(err)
	}
	if d.TailcatKey != "nodekey:abc" || d.Name != "Pixel 9" {
		t.Fatalf("device = %+v", d)
	}
	if got, err := s.Authenticate(token); err != nil || got.TailcatKey != "nodekey:abc" {
		t.Fatalf("Authenticate = %+v, %v", got, err)
	}
	ds, _ := s.Devices()
	if len(ds) != 1 || ds[0].TailcatKey != "nodekey:abc" {
		t.Fatalf("devices = %+v", ds)
	}
	if _, _, err := s.RedeemTailcat(code, "again", "nodekey:def"); !errors.Is(err, ErrCodeGone) {
		t.Fatalf("second redeem err = %v, want ErrCodeGone", err)
	}
}

func TestOldCodesArePurged(t *testing.T) {
	s, c := newStore(t)
	old, _ := s.CreateCode("Phone", time.Minute)
	c.t = c.t.Add(25 * time.Hour)
	if _, err := s.CreateCode("Phone", time.Minute); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stateFile(t, s), hash(old)) {
		t.Fatal("a code expired a day ago is still stored")
	}
}
