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
	intel := filepath.Join(dir, intelMac)
	win := filepath.Join(dir, update.WindowsAsset)
	linux := filepath.Join(dir, update.LinuxAsset)
	linuxArm := filepath.Join(dir, update.LinuxArmAsset)
	apk := filepath.Join(dir, update.AppAsset)
	os.WriteFile(mac, []byte("mac file"), 0o755)
	os.WriteFile(intel, []byte("intel mac file"), 0o755)
	os.WriteFile(win, []byte("windows file"), 0o644)
	os.WriteFile(linux, []byte("linux file"), 0o755)
	os.WriteFile(linuxArm, []byte("linux arm file"), 0o755)
	os.WriteFile(apk, []byte("android file"), 0o644)
	rel := filepath.Join(dir, "out")
	os.Mkdir(rel, 0o755)
	if _, err := run(t, "sign", "-key", key, "-version", "0.1.8", "-out", rel, "-any-key", mac, intel, win, linux, linuxArm, apk); err != nil {
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
	names := []string{update.MacAsset, intelMac, update.WindowsAsset, update.LinuxAsset, update.LinuxArmAsset, update.AppAsset}
	if m.Version != "0.1.8" || len(m.Files) != len(names) || m.Files[0] != want {
		t.Fatalf("%+v", m)
	}
	for i, n := range names {
		if m.Files[i].Name != n {
			t.Errorf("file %d is %q, want %q", i, m.Files[i].Name, n)
		}
	}
}

// The Intel Mac's file, as an Intel Mac's companion looks for it (update.AssetFor("darwin", "amd64")).
const intelMac = "herdr-anywhere-macos-amd64"

// A release that leaves out one computer's file, such as the Intel Mac's when a release script built only the others,
// would tell that computer of a version it can't install. sign refuses it unless asked for part of a release (-only,
// for tests that need one computer's file).
func TestSignRefusesAReleaseMissingAComputersFile(t *testing.T) {
	dir := t.TempDir()
	key := filepath.Join(dir, "update.key")
	run(t, "keygen", key)
	var files []string
	for _, n := range update.ReleaseFiles {
		if n == intelMac {
			continue
		}
		p := filepath.Join(dir, n)
		os.WriteFile(p, []byte(n), 0o644)
		files = append(files, p)
	}
	if len(files) != len(update.ReleaseFiles)-1 {
		t.Fatalf("a release's files are %v, without %s", update.ReleaseFiles, intelMac)
	}
	out, err := run(t, append([]string{"sign", "-key", key, "-version", "0.1.8", "-out", dir, "-any-key"}, files...)...)
	if err == nil || !strings.Contains(err.Error(), intelMac) {
		t.Fatalf("%v %s", err, out)
	}
	if _, err := os.Stat(filepath.Join(dir, update.ManifestFile)); err == nil {
		t.Fatal("wrote a manifest anyway")
	}
	if _, err := run(t, append([]string{"sign", "-key", key, "-version", "0.1.8", "-out", dir, "-any-key", "-only"}, files...)...); err != nil {
		t.Fatalf("-only: %v", err)
	}
}

// A file under a name no computer looks for (here Herdr's own spelling of the machine) would leave those computers
// without the update, so sign refuses it.
func TestSignRefusesAFileNoComputerLooksFor(t *testing.T) {
	dir := t.TempDir()
	key := filepath.Join(dir, "update.key")
	run(t, "keygen", key)
	wrong := filepath.Join(dir, "herdr-anywhere-linux-x86_64")
	os.WriteFile(wrong, []byte("x"), 0o755)
	out, err := run(t, "sign", "-key", key, "-version", "0.1.8", "-out", dir, "-any-key", wrong)
	if err == nil || !strings.Contains(err.Error(), "herdr-anywhere-linux-x86_64") {
		t.Fatalf("%v %s", err, out)
	}
	if _, err := os.Stat(filepath.Join(dir, update.ManifestFile)); err == nil {
		t.Fatal("wrote a manifest anyway")
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
	if out, err := run(t, "sign", "-key", key, "-version", "v0.1", "-out", dir, "-any-key", "-only", mac); err == nil || !strings.Contains(err.Error(), "v0.1") {
		t.Fatalf("%v %s", err, out)
	}
}
