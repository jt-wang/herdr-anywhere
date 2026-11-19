package lan

import (
	"testing"
	"time"
)

// The plain LAN port lasts 3 days from the first serve of 0.3, not from each start; with no phone to serve it never
// opens, and once closed it stays closed.
func TestPlainUntil(t *testing.T) {
	now := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	got, err := PlainUntil(dir, 2, now)
	if err != nil || !got.Equal(now.Add(72*time.Hour)) {
		t.Fatalf("first serve: %v %v, want 3 days on", got, err)
	}
	// A restart two days later keeps the same deadline, also with more phones.
	if again, _ := PlainUntil(dir, 5, now.Add(48*time.Hour)); !again.Equal(got) {
		t.Fatalf("restart moved the deadline to %v", again)
	}
	if err := ClosePlain(dir, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if after, _ := PlainUntil(dir, 5, now.Add(2*time.Hour)); !after.Equal(now.Add(time.Hour)) {
		t.Fatalf("closed port reopened until %v", after)
	}

	none := t.TempDir()
	if got, _ := PlainUntil(none, 0, now); !got.Equal(now) {
		t.Fatalf("no plain phones: %v, want closed now", got)
	}
	// A plain phone appearing later does not open it: from 0.3 nothing pairs in plain HTTP.
	if got, _ := PlainUntil(none, 1, now.Add(time.Minute)); !got.Equal(now) {
		t.Fatalf("reopened for a later phone: %v", got)
	}
}
