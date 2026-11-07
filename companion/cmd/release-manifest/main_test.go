package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"herdr-anywhere/companion/internal/testfake"
	"herdr-anywhere/companion/internal/update"
)

func run(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	err := runMain(args, &out)
	return out.String(), err
}

func TestKeygenKeepsTheKeyToItsOwner(t *testing.T) {
	key := filepath.Join(t.TempDir(), "update.key")
	out, err := run(t, "keygen", key)
	if err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(key)
	if err != nil || (testfake.PermBits() && fi.Mode().Perm() != 0o600) {
		t.Fatalf("key file %v, %v", fi, err)
	}
	pub, err := base64.StdEncoding.DecodeString(strings.TrimSpace(out))
	if err != nil || len(pub) != ed25519.PublicKeySize {
		t.Fatalf("printed %q", out)
	}
	before, _ := os.ReadFile(key)
	if _, err := run(t, "keygen", key); err == nil {
		t.Fatal("overwrote a key")
	}
	if after, _ := os.ReadFile(key); !bytes.Equal(before, after) {
		t.Fatal("the key changed")
	}
}

func TestSignWritesWhatACompanionAccepts(t *testing.T) {
	dir := t.TempDir()
	key := filepath.Join(dir, "update.key")
	out, err := run(t, "keygen", key)
	if err != nil {
		t.Fatal(err)
	}
	pub, _ := base64.StdEncoding.DecodeString(strings.TrimSpace(out))
	mac := filepath.Join(dir, update.MacAsset)
	win := filepath.Join(dir, update.WindowsAsset)
	apk := filepath.Join(dir, update.AppAsset)
	os.WriteFile(mac, []byte("mac file"), 0o755)
	os.WriteFile(win, []byte("windows file"), 0o644)
	os.WriteFile(apk, []byte("android file"), 0o644)
	rel := filepath.Join(dir, "out")
	os.Mkdir(rel, 0o755)
	if _, err := run(t, "sign", "-key", key, "-version", "0.1.8", "-out", rel, "-any-key", mac, win, apk); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(filepath.Join(rel, update.ManifestFile))
	sig, _ := os.ReadFile(filepath.Join(rel, update.ManifestFile+".sig"))
	m, err := update.Verify(ed25519.PublicKey(pub), body, sig)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("mac file"))
	want := update.File{Name: update.MacAsset, SHA256: hex.EncodeToString(sum[:]), Bytes: 8}
	if m.Version != "0.1.8" || len(m.Files) != 3 || m.Files[0] != want || m.Files[1].Name != update.WindowsAsset || m.Files[2].Name != update.AppAsset {
		t.Fatalf("%+v", m)
	}
}

// Signing with a key computers don't have would leave them refusing every later release.
func TestSignRefusesAKeyTheCompanionDoesNotTrust(t *testing.T) {
	dir := t.TempDir()
	key := filepath.Join(dir, "update.key")
	if _, err := run(t, "keygen", key); err != nil {
		t.Fatal(err)
	}
	mac := filepath.Join(dir, update.MacAsset)
	os.WriteFile(mac, []byte("mac file"), 0o755)
	if _, err := run(t, "sign", "-key", key, "-version", "0.1.8", "-out", dir, mac); err == nil {
		t.Fatal("signed with a key the companion doesn't trust")
	}
	if _, err := os.Stat(filepath.Join(dir, update.ManifestFile)); err == nil {
		t.Fatal("wrote a manifest anyway")
	}
}

func TestSignRefusesAVersionThatIsNotOne(t *testing.T) {
	dir := t.TempDir()
	key := filepath.Join(dir, "update.key")
	run(t, "keygen", key)
	mac := filepath.Join(dir, update.MacAsset)
	os.WriteFile(mac, []byte("x"), 0o755)
	if _, err := run(t, "sign", "-key", key, "-version", "v0.1", "-out", dir, "-any-key", mac); err == nil {
		t.Fatal("no error")
	}
}
