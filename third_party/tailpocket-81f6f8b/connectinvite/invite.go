// Package connectinvite is the pairing invite a host hands to a phone by QR code, link or same-Wi-Fi
// delivery. Holding a valid, unexpired invite is what lets a phone pair, so treat it as a secret.
package connectinvite

import (
	"bytes"
	"compress/zlib"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"
	"unicode"

	"github.com/tailscale/tailcat"
	"tailscale.com/types/key"
)

const (
	Version       = 1
	DefaultPrefix = "tailpocket1:"
	// MaxTextLength bounds both the QR text and the decompressed JSON.
	MaxTextLength = 4096
	MaxNameLength = 64
)

// Invite opens one pairing window: Address and Port reach the host's pairing node, Key is the
// one-time client identity that node admits, Proof authorizes the /pair request.
type Invite struct {
	Version   int    `json:"v"`
	Address   string `json:"address"`
	Port      int    `json:"port"`
	Key       string `json:"key"`
	Proof     string `json:"proof"`
	ExpiresAt int64  `json:"expiresAt"` // unix seconds
	Name      string `json:"name,omitempty"`
}

var ErrInvalid = errors.New("invalid invite")

// Encode returns prefix followed by the base64url (no padding) zlib-compressed JSON form.
func Encode(in Invite, prefix string) (string, error) {
	if err := in.Validate(); err != nil {
		return "", err
	}
	return pack(in, prefix)
}

func pack(v any, prefix string) (string, error) {
	if !validPrefix(prefix) {
		return "", errors.New("invalid prefix")
	}
	body, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	w, err := zlib.NewWriterLevel(&buf, zlib.BestCompression)
	if err != nil {
		return "", err
	}
	if _, err := w.Write(body); err != nil {
		return "", err
	}
	if err := w.Close(); err != nil {
		return "", err
	}
	text := prefix + base64.RawURLEncoding.EncodeToString(buf.Bytes())
	if len(text) > MaxTextLength {
		return "", ErrInvalid
	}
	return text, nil
}

// Decode parses the text Encode produced. Surrounding whitespace is ignored.
func Decode(text, prefix string) (Invite, error) {
	body, err := unpack(text, prefix)
	if err != nil {
		return Invite{}, err
	}
	return ParseJSON(body)
}

func unpack(text, prefix string) ([]byte, error) {
	text = strings.TrimSpace(text)
	if !validPrefix(prefix) || len(text) > MaxTextLength || !strings.HasPrefix(text, prefix) {
		return nil, ErrInvalid
	}
	packed, err := base64.RawURLEncoding.DecodeString(text[len(prefix):])
	if err != nil || len(packed) == 0 {
		return nil, ErrInvalid
	}
	r, err := zlib.NewReader(bytes.NewReader(packed))
	if err != nil {
		return nil, ErrInvalid
	}
	body, err := io.ReadAll(io.LimitReader(r, MaxTextLength+1))
	if err != nil || len(body) > MaxTextLength {
		return nil, ErrInvalid
	}
	return body, nil
}

// ParseJSON parses and validates the plain JSON form (what same-Wi-Fi pairing delivers sealed).
func ParseJSON(body []byte) (Invite, error) {
	if len(body) > MaxTextLength {
		return Invite{}, ErrInvalid
	}
	var in Invite
	if json.Unmarshal(body, &in) != nil {
		return Invite{}, ErrInvalid
	}
	if err := in.Validate(); err != nil {
		return Invite{}, err
	}
	return in, nil
}

// Validate checks every field's shape. It does not check expiry; see Expired.
func (i Invite) Validate() error {
	if i.Version != Version || !ValidAddress(i.Address) || i.Port < 1 || i.Port > 65535 || i.ExpiresAt <= 0 {
		return ErrInvalid
	}
	var private key.NodePrivate
	if private.UnmarshalText([]byte(i.Key)) != nil || private.IsZero() {
		return ErrInvalid
	}
	if proof, err := base64.RawURLEncoding.DecodeString(i.Proof); err != nil || len(proof) != 32 || len(i.Proof) != 43 {
		return ErrInvalid
	}
	if !ValidName(i.Name) {
		return ErrInvalid
	}
	return nil
}

// Expired reports whether the invite can no longer be used at now.
func (i Invite) Expired(now time.Time) bool { return now.Unix() >= i.ExpiresAt }

// NewProof returns 32 random bytes as 43 base64url characters.
func NewProof() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand never fails on supported platforms
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// ValidAddress reports whether text is a tailcat address with a server key.
func ValidAddress(text string) bool {
	if !strings.HasPrefix(text, "tc") || len(text) > 8192 {
		return false
	}
	info, err := tailcat.ParseAddr(tailcat.Addr(text))
	return err == nil && !info.ServerPublic.IsZero()
}

// ValidName allows up to MaxNameLength characters and no control characters.
func ValidName(name string) bool {
	if len([]rune(name)) > MaxNameLength {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

// CleanName makes any text a valid name: control characters removed, trimmed, cut to length.
func CleanName(name string) string {
	name = strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, name))
	if runes := []rune(name); len(runes) > MaxNameLength {
		name = strings.TrimSpace(string(runes[:MaxNameLength]))
	}
	return name
}

// validPrefix: non-empty, printable ASCII without spaces, exactly one ':' at the end.
func validPrefix(prefix string) bool {
	if len(prefix) < 2 || len(prefix) > 32 || !strings.HasSuffix(prefix, ":") || strings.Count(prefix, ":") != 1 {
		return false
	}
	for _, r := range prefix {
		if r <= ' ' || r > '~' {
			return false
		}
	}
	return true
}
