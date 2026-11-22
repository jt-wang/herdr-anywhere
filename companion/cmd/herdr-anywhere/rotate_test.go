package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"herdr-anywhere/companion/internal/store"
	"herdr-anywhere/companion/internal/testfake"
)

// rotateFixture is a state dir with two paired phones and an identity file whose content is a marker no output may hold.
func rotateFixture(t *testing.T) (dir, secret string) {
	t.Helper()
	dir = shortStateDir(t)
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Phone A", "Phone B"} {
		code, err := st.CreateCode(name, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := st.Redeem(code, name); err != nil {
			t.Fatal(err)
		}
	}
	secret = "identity-marker-7c1f"
	if err := os.WriteFile(filepath.Join(dir, "tailcat.json"), []byte(`{"identity":"`+secret+`"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir, secret
}

// With no serve running, rotate-identity unpairs every phone and moves the identity aside unread, as it was.
func TestRotateIdentityWithoutServe(t *testing.T) {
	dir, secret := rotateFixture(t)
	out, errs, code := runCLI(t, "rotate-identity", "--yes", "--state", dir)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errs)
	}
	if strings.Contains(out+errs, secret) {
		t.Fatal("the output holds the identity")
	}
	st, _ := store.Open(dir)
	if ds, _ := st.Devices(); len(ds) != 0 {
		t.Fatalf("%d phones still paired", len(ds))
	}
	if _, err := os.Stat(filepath.Join(dir, "tailcat.json")); !os.IsNotExist(err) {
		t.Fatalf("tailcat.json is still there: %v", err)
	}
	retired, _ := filepath.Glob(filepath.Join(dir, "tailcat.json.retired-*"))
	if len(retired) != 1 {
		t.Fatalf("%d retired files", len(retired))
	}
	if b, _ := os.ReadFile(retired[0]); !strings.Contains(string(b), secret) {
		t.Fatal("the retired file is not the old identity")
	}
	if fi, _ := os.Stat(retired[0]); testfake.PermBits() && fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", fi.Mode())
	}
	if !strings.Contains(out, "herdr-anywhere pair") || !strings.Contains(out, filepath.Base(retired[0])) {
		t.Fatalf("doesn't say how to pair again and where the old keys are:\n%s", out)
	}
}

// Anything but yes changes nothing.
func TestRotateIdentityAsksFirst(t *testing.T) {
	dir, _ := rotateFixture(t)
	keep := rotateInput
	rotateInput = strings.NewReader("\n")
	t.Cleanup(func() { rotateInput = keep })
	out, errs, code := runCLI(t, "rotate-identity", "--state", dir)
	if code != 0 || !strings.Contains(out, "nothing changed") {
		t.Fatalf("exit %d:\n%s%s", code, out, errs)
	}
	st, _ := store.Open(dir)
	if ds, _ := st.Devices(); len(ds) != 2 {
		t.Fatalf("%d phones paired, want 2", len(ds))
	}
	if _, err := os.Stat(filepath.Join(dir, "tailcat.json")); err != nil {
		t.Fatal(err)
	}
}

// A serve that recorded its pid but doesn't answer yet has read the old identity: rotate-identity changes nothing then.
func TestRotateIdentityWaitsForAServeStarting(t *testing.T) {
	dir, _ := rotateFixture(t)
	if err := os.WriteFile(filepath.Join(dir, serveURLsFile), []byte(`{"urls":[],"pid":`+itoa(os.Getppid())+`}`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, errs, code := runCLI(t, "rotate-identity", "--yes", "--state", dir)
	if code == 0 || !strings.Contains(errs, "starting") {
		t.Fatalf("exit %d: %s", code, errs)
	}
	st, _ := store.Open(dir)
	if ds, _ := st.Devices(); len(ds) != 2 {
		t.Fatalf("%d phones paired, want 2", len(ds))
	}
	if _, err := os.Stat(filepath.Join(dir, "tailcat.json")); err != nil {
		t.Fatal(err)
	}
}
