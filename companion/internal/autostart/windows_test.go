//go:build windows

package autostart

import (
	"crypto/rand"
	"encoding/hex"
	"testing"
)

// The Run key in this user's registry, under a name of the test's own.
func TestRunKeyOnWindows(t *testing.T) {
	b := make([]byte, 4)
	rand.Read(b)
	s := &Service{Name: "Herdr Anywhere test " + hex.EncodeToString(b)}
	t.Cleanup(func() { s.Remove() })
	e := Entry{Exe: `C:\Program Files\h\herdr-anywhere.exe`, Args: []string{"serve", "--state", `C:\a b`}}
	if changed, err := s.Install(e); err != nil || !changed {
		t.Fatalf("install: %v %v", changed, err)
	}
	if changed, err := s.Install(e); err != nil || changed {
		t.Fatalf("again: %v %v", changed, err)
	}
	if v, err := s.Value(); err != nil || v != windowsCommandLine(e) {
		t.Fatalf("value %q %v", v, err)
	}
	if err := s.Start(); err != ErrNoManager {
		t.Fatalf("start: %v", err)
	}
	if removed, err := s.Remove(); err != nil || !removed {
		t.Fatalf("remove: %v %v", removed, err)
	}
	if removed, err := s.Remove(); err != nil || removed {
		t.Fatalf("remove again: %v %v", removed, err)
	}
}
