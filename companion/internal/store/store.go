// Package store keeps paired devices and pairing codes in one JSON file in
// the state dir. Only SHA-256 hashes of codes and tokens are written.
//
// `herdr-anywhere pair` and `herdr-anywhere serve` are separate processes on the
// same file: every change takes an exclusive flock on state.lock, re-reads the
// file, and replaces it with a temp file + rename, so readers never see a
// half-written file and no writer's change is lost.
package store

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	stateName = "state.json"
	lockName  = "state.lock"
	// How long a used or expired code is remembered, so a late try gets
	// "gone" (410) rather than "unknown" (401).
	keepGone = 24 * time.Hour
)

var (
	ErrBadCode      = errors.New("unknown pairing code")
	ErrCodeGone     = errors.New("pairing code expired or already used")
	ErrUnknownToken = errors.New("unknown device token")
	ErrNoDevice     = errors.New("no such device")
)

// Device is a paired phone, as `devices` lists it.
type Device struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Created  time.Time `json:"created"`
	LastSeen time.Time `json:"last_seen,omitzero"`
	// TailcatKey is the phone's Tailcat key ("nodekey:…") when it paired
	// through Tailcat; serve takes it off the Tailcat allowlist on revoke.
	TailcatKey string `json:"tailcat_key,omitempty"`
}

type device struct {
	Device
	TokenSHA256 string `json:"token_sha256"`
}

type code struct {
	SHA256  string    `json:"sha256"`
	Name    string    `json:"name"`
	Expires time.Time `json:"expires"`
	Used    bool      `json:"used,omitempty"`
}

type state struct {
	Devices []device `json:"devices"`
	Codes   []code   `json:"codes"`
}

type Store struct {
	dir string
	now func() time.Time
}

// Open makes dir (0700) if needed and tightens it to 0700 if it exists.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, err
	}
	return &Store{dir: dir, now: time.Now}, nil
}

// Dir is the state directory.
func (s *Store) Dir() string { return s.dir }

func hash(secret string) string {
	h := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(h[:])
}

func random(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand never fails on supported platforms
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func equalHash(a, b string) bool { return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 }

// CreateCode stores a new single-use pairing code valid for ttl and returns it.
func (s *Store) CreateCode(name string, ttl time.Duration) (string, error) {
	c := random(16) // 128 bits
	err := s.update(func(st *state) error {
		st.Codes = append(st.Codes, code{SHA256: hash(c), Name: name, Expires: s.now().Add(ttl)})
		return nil
	})
	if err != nil {
		return "", err
	}
	return c, nil
}

// Redeem uses up a pairing code and pairs a new device. It returns the device
// and its token; the token is never stored, only its hash.
func (s *Store) Redeem(pairingCode, deviceName string) (Device, string, error) {
	return s.RedeemTailcat(pairingCode, deviceName, "")
}

// RedeemTailcat is Redeem for a phone pairing through Tailcat: the device
// also records the phone's Tailcat key.
func (s *Store) RedeemTailcat(pairingCode, deviceName, tailcatKey string) (Device, string, error) {
	var d device
	token := random(32) // 256 bits
	err := s.update(func(st *state) error {
		h := hash(pairingCode)
		for i := range st.Codes {
			c := &st.Codes[i]
			if !equalHash(c.SHA256, h) {
				continue
			}
			if c.Used || !s.now().Before(c.Expires) {
				return ErrCodeGone
			}
			c.Used = true
			name := deviceName
			if name == "" {
				name = c.Name
			}
			d = device{Device: Device{ID: newID(st), Name: name, Created: s.now(), TailcatKey: tailcatKey}, TokenSHA256: hash(token)}
			st.Devices = append(st.Devices, d)
			return nil
		}
		return ErrBadCode
	})
	if err != nil {
		return Device{}, "", err
	}
	return d.Device, token, nil
}

func newID(st *state) string {
	for {
		b := make([]byte, 4)
		_, _ = rand.Read(b)
		id := hex.EncodeToString(b)
		taken := false
		for _, d := range st.Devices {
			taken = taken || d.ID == id
		}
		if !taken {
			return id
		}
	}
}

// Authenticate finds the device a token belongs to.
func (s *Store) Authenticate(token string) (Device, error) {
	if token == "" {
		return Device{}, ErrUnknownToken
	}
	st, err := s.read()
	if err != nil {
		return Device{}, err
	}
	h := hash(token)
	for _, d := range st.Devices {
		if equalHash(d.TokenSHA256, h) {
			return d.Device, nil
		}
	}
	return Device{}, ErrUnknownToken
}

// Touch sets a device's last-seen time to now.
func (s *Store) Touch(id string) error {
	return s.update(func(st *state) error {
		for i := range st.Devices {
			if st.Devices[i].ID == id {
				st.Devices[i].LastSeen = s.now()
				return nil
			}
		}
		return ErrNoDevice
	})
}

// Devices lists the paired devices, oldest first.
func (s *Store) Devices() ([]Device, error) {
	st, err := s.read()
	if err != nil {
		return nil, err
	}
	out := make([]Device, 0, len(st.Devices))
	for _, d := range st.Devices {
		out = append(out, d.Device)
	}
	return out, nil
}

// Revoke forgets a device; its token stops working at once.
func (s *Store) Revoke(id string) error {
	return s.update(func(st *state) error {
		for i := range st.Devices {
			if st.Devices[i].ID == id {
				st.Devices = append(st.Devices[:i], st.Devices[i+1:]...)
				return nil
			}
		}
		return ErrNoDevice
	})
}

func (s *Store) read() (state, error) {
	var st state
	b, err := os.ReadFile(filepath.Join(s.dir, stateName))
	if errors.Is(err, os.ErrNotExist) {
		return st, nil
	}
	if err != nil {
		return st, err
	}
	if err := json.Unmarshal(b, &st); err != nil {
		// A broken file is an error, never silently replaced: it holds the pairings.
		return st, fmt.Errorf("%s: %w", filepath.Join(s.dir, stateName), err)
	}
	return st, nil
}

// update runs change on the current state under the lock and writes the
// result, dropping codes that ended more than a day ago. When change returns
// an error nothing is written.
func (s *Store) update(change func(*state) error) error {
	lock, err := os.OpenFile(filepath.Join(s.dir, lockName), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := lockFile(lock); err != nil {
		return err
	}
	defer unlockFile(lock)

	st, err := s.read()
	if err != nil {
		return err
	}
	if err := change(&st); err != nil {
		return err
	}
	kept := st.Codes[:0]
	for _, c := range st.Codes {
		if s.now().Before(c.Expires.Add(keepGone)) {
			kept = append(kept, c)
		}
	}
	st.Codes = kept
	return s.write(st)
}

func (s *Store) write(st state) error {
	if st.Devices == nil {
		st.Devices = []device{}
	}
	if st.Codes == nil {
		st.Codes = []code{}
	}
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.dir, stateName+".*.tmp") // created 0600
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(append(b, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(s.dir, stateName))
}
