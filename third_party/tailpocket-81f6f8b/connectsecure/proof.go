package connectsecure

import (
	"crypto/hmac"
	"crypto/sha256"

	"golang.org/x/crypto/curve25519"
)

// KeyProof shows the holder of the X25519 private key priv to the holder of peer's private key:
// HMAC-SHA256 over label and msg, keyed with X25519(priv, peer). Only those two can compute it, and
// label and msg bind it to one use. CheckKeyProof is the other side.
func KeyProof(priv []byte, peer [32]byte, label string, msg []byte) ([]byte, error) {
	shared, err := curve25519.X25519(priv, peer[:])
	if err != nil {
		return nil, err // a low-order peer key
	}
	m := hmac.New(sha256.New, shared)
	m.Write([]byte(label))
	m.Write([]byte{0})
	m.Write(msg)
	return m.Sum(nil), nil
}

// CheckKeyProof reports whether proof is KeyProof by the holder of pub's private key, checked with
// the private key own (whose public key that holder used as peer).
func CheckKeyProof(own []byte, pub [32]byte, label string, msg, proof []byte) bool {
	want, err := KeyProof(own, pub, label, msg)
	return err == nil && hmac.Equal(want, proof)
}
