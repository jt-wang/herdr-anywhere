package lan

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Before 0.3 a phone could pair and connect over the local network in plain HTTP, which anyone on that network can
// read and impersonate. From 0.3 the local network is end-to-end encrypted (TailPocket's lan route); the plain port
// stays only for phones paired that way, which have no Tailcat key to get an encryption key with and must pair again,
// and at most PlainGrace after the first serve of 0.3. Once it closes it stays closed.

// PlainFile, in the state dir, holds when the plain LAN port closes (RFC 3339).
const PlainFile = "lan-plain-until"

// PlainGrace is how long the plain LAN port stays after the upgrade, at most.
const PlainGrace = 72 * time.Hour

// PlainUntil is when the plain LAN port closes. The first call records it: now+PlainGrace when phones without a
// Tailcat key are paired (plainPhones > 0), else now, so a port with no one to serve never opens. Later calls
// return what was recorded.
func PlainUntil(dir string, plainPhones int, now time.Time) (time.Time, error) {
	p := filepath.Join(dir, PlainFile)
	b, err := os.ReadFile(p)
	if err == nil {
		t, perr := time.Parse(time.RFC3339, strings.TrimSpace(string(b)))
		if perr != nil {
			return time.Time{}, fmt.Errorf("%s: %w", p, perr)
		}
		return t, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return time.Time{}, err
	}
	until := now
	if plainPhones > 0 {
		until = now.Add(PlainGrace)
	}
	return until, ClosePlain(dir, until)
}

// ClosePlain records that the plain LAN port closes at until (now, when every phone that needed it is gone).
func ClosePlain(dir string, until time.Time) error {
	p := filepath.Join(dir, PlainFile)
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, []byte(until.UTC().Format(time.RFC3339)+"\n"), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}
