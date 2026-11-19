package connectlocal

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/curve25519"
	"golang.org/x/crypto/hkdf"
)

// Construction (both roles derive the same values; phonePub ‖ hostPub order is fixed by role):
//
//	shared = X25519(own private, peer public)
//	code   = "%06d" of uint64_be(SHA256(label ‖ phonePub ‖ hostPub ‖ shared)[0:8]) mod 1_000_000
//	key    = HKDF-SHA256(ikm = shared, salt = phonePub ‖ hostPub, info = label ‖ " invitation", 32 bytes)
//	sealed = nonce(12) ‖ ChaCha20-Poly1305(key, nonce, plaintext) ‖ tag(16)

type KeyPair struct{ Private, Public [32]byte }

func NewKeyPair() (KeyPair, error) {
	private := make([]byte, curve25519.ScalarSize)
	if _, err := io.ReadFull(rand.Reader, private); err != nil {
		return KeyPair{}, err
	}
	return KeyPairFromPrivate(private)
}

func KeyPairFromPrivate(private []byte) (KeyPair, error) {
	if len(private) != curve25519.ScalarSize {
		return KeyPair{}, errors.New("private key must be 32 bytes")
	}
	public, err := curve25519.X25519(private, curve25519.Basepoint)
	if err != nil {
		return KeyPair{}, err
	}
	var kp KeyPair
	copy(kp.Private[:], private)
	copy(kp.Public[:], public)
	return kp, nil
}

// Session is one side's view of a pairing exchange.
type Session struct {
	Code string
	key  []byte
}

func NewSession(label string, own KeyPair, peerPublic []byte, phone bool) (*Session, error) {
	if label == "" {
		return nil, errors.New("label is empty")
	}
	if len(peerPublic) != curve25519.PointSize {
		return nil, errors.New("peer key must be 32 bytes")
	}
	shared, err := curve25519.X25519(own.Private[:], peerPublic) // rejects low-order points
	if err != nil {
		return nil, err
	}
	phonePub, hostPub := own.Public[:], peerPublic
	if !phone {
		phonePub, hostPub = peerPublic, own.Public[:]
	}
	digest := sha256.New()
	digest.Write([]byte(label))
	digest.Write(phonePub)
	digest.Write(hostPub)
	digest.Write(shared)
	code := fmt.Sprintf("%06d", binary.BigEndian.Uint64(digest.Sum(nil)[:8])%1_000_000)
	salt := append(append([]byte{}, phonePub...), hostPub...)
	key := make([]byte, chacha20poly1305.KeySize)
	if _, err := io.ReadFull(hkdf.New(sha256.New, shared, salt, []byte(label+" invitation")), key); err != nil {
		return nil, err
	}
	return &Session{Code: code, key: key}, nil
}

func (s *Session) Seal(plain []byte) ([]byte, error) {
	nonce := make([]byte, chacha20poly1305.NonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return s.sealWithNonce(plain, nonce)
}

func (s *Session) sealWithNonce(plain, nonce []byte) ([]byte, error) {
	aead, err := chacha20poly1305.New(s.key)
	if err != nil {
		return nil, err
	}
	return aead.Seal(append([]byte{}, nonce...), nonce, plain, nil), nil
}

func (s *Session) Open(box []byte) ([]byte, error) {
	if len(box) < chacha20poly1305.NonceSize+chacha20poly1305.Overhead || len(box) > 128*1024 {
		return nil, errors.New("sealed box has the wrong size")
	}
	aead, err := chacha20poly1305.New(s.key)
	if err != nil {
		return nil, err
	}
	return aead.Open(nil, box[:chacha20poly1305.NonceSize], box[chacha20poly1305.NonceSize:], nil)
}
