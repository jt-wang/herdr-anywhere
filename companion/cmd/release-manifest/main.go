// release-manifest makes the signing key for releases, and signs a release's latest.json with it.
//
//	release-manifest keygen <key file>
//	release-manifest sign -key <key file> -version 0.1.8 -out <dir> <file>...
//
// keygen writes the private key, readable only by you, and prints the public key that goes into
// companion/internal/update/manifest.go. sign writes latest.json and latest.json.sig for the release; it refuses a key
// whose public half isn't the one built into the companion, since computers would then refuse that release.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"herdr-anywhere/companion/internal/update"
)

func main() {
	if err := runMain(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "release-manifest:", err)
		os.Exit(1)
	}
}

func runMain(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: release-manifest keygen <key file> | sign -key <key file> -version X.Y.Z -out <dir> <file>...")
	}
	switch args[0] {
	case "keygen":
		if len(args) != 2 {
			return errors.New("usage: release-manifest keygen <key file>")
		}
		return keygen(args[1], stdout)
	case "sign":
		return sign(args[1:], stdout)
	}
	return fmt.Errorf("unknown command %q", args[0])
}

func keygen(path string, stdout io.Writer) error {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintln(f, base64.StdEncoding.EncodeToString(priv.Seed())); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	_, err = fmt.Fprintln(stdout, base64.StdEncoding.EncodeToString(pub))
	return err
}

func readKey(path string) (ed25519.PrivateKey, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	seed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(b)))
	if err != nil || len(seed) != ed25519.SeedSize {
		return nil, fmt.Errorf("%s is not a key from release-manifest keygen", path)
	}
	return ed25519.NewKeyFromSeed(seed), nil
}

func sign(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("sign", flag.ContinueOnError)
	keyPath := fs.String("key", "", "the private key file")
	version := fs.String("version", "", "the release's version, such as 0.1.8")
	out := fs.String("out", ".", "where to write latest.json and latest.json.sig")
	anyKey := fs.Bool("any-key", false, "sign with a key the companion doesn't have (tests)")
	only := fs.Bool("only", false, "sign a release that leaves out some computers' files (tests)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *keyPath == "" || fs.NArg() == 0 {
		return errors.New("sign needs -key and the release's files")
	}
	key, err := readKey(*keyPath)
	if err != nil {
		return err
	}
	if !*anyKey && !key.Public().(ed25519.PublicKey).Equal(update.PublicKey) {
		return errors.New("this key isn't the one built into the companion; computers would refuse the release")
	}
	m := update.Manifest{Version: *version}
	var given []string
	for _, p := range fs.Args() {
		if !slices.Contains(update.ReleaseFiles, filepath.Base(p)) {
			return fmt.Errorf("no computer looks for %s; a release's files are named %s", filepath.Base(p), strings.Join(update.ReleaseFiles, ", "))
		}
		given = append(given, filepath.Base(p))
	}
	// A computer whose file is missing would hear of a version it can't install.
	if !*only {
		var missing []string
		for _, n := range update.ReleaseFiles {
			if !slices.Contains(given, n) {
				missing = append(missing, n)
			}
		}
		if len(missing) > 0 {
			return fmt.Errorf("the release has no %s; every computer needs its file", strings.Join(missing, ", "))
		}
	}
	for _, p := range fs.Args() {
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(b)
		m.Files = append(m.Files, update.File{Name: filepath.Base(p), SHA256: hex.EncodeToString(sum[:]), Bytes: int64(len(b))})
	}
	body, sig, err := update.Sign(key, m)
	if err != nil {
		return err
	}
	// The same check a computer makes, before anything is written.
	if _, err := update.Verify(key.Public().(ed25519.PublicKey), body, sig); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(*out, update.ManifestFile), body, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(*out, update.ManifestFile+".sig"), sig, 0o644); err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, "signed %s with %d files\n", m.Version, len(m.Files))
	return err
}
