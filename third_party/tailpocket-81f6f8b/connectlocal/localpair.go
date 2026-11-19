// Package connectlocal delivers a pairing invite to a phone on the same Wi-Fi: the phone finds the host
// over Bonjour, both run an X25519 exchange, both show a 6-digit code, and after the host's user
// confirms the codes match, the host sends the invite sealed with ChaCha20-Poly1305.
// It only delivers the invite; the connection itself always goes over Tailcat.
//
// Wire format (newline-terminated JSON lines, sorted keys, at most MaxLine bytes):
//
//	phone → host  {"device":"<≤64 chars>","publicKey":"<base64 X25519>","v":1}
//	host  → phone {"publicKey":"<base64 X25519>","v":1}
//	              both sides show the code; the host's user approves or denies
//	host  → phone {"sealed":"<base64>","status":"approved","v":1}
//	           or {"status":"denied","v":1} / {"reason":"expired","status":"denied","v":1}
package connectlocal

import (
	"errors"
	"strconv"
)

const (
	DefaultLabel = "tailpocket-local-pairing-v1"
	MaxLine      = 4096
	wireVersion  = 1
)

var (
	ErrDenied      = errors.New("local pairing denied")
	ErrExpired     = errors.New("local pairing window expired")
	ErrFailed      = errors.New("local pairing failed")
	ErrNotApproved = errors.New("no phone was approved before the window closed")
	ErrUnavailable = errors.New("no Wi-Fi/LAN address to serve local pairing on")
)

func itoa(i int) string { return strconv.Itoa(i) }
