package cursor

import (
	"encoding/binary"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestLinuxStarted(t *testing.T) {
	word := strconv.IntSize / 8
	auxv := make([]byte, 6*word)
	for i, n := range []uint64{6, 4096, 17, 100, 0, 0} {
		if word == 4 {
			binary.NativeEndian.PutUint32(auxv[i*word:], uint32(n))
		} else {
			binary.NativeEndian.PutUint64(auxv[i*word:], n)
		}
	}
	now := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)
	stat := "42 (paper ) garden) S " + strings.Repeat("0 ", 18) + "12345 0 0"
	want := now.Add(-476550 * time.Millisecond)
	if got := linuxStarted(stat, auxv, now, 10*time.Minute); !got.Equal(want) {
		t.Fatalf("start from ticks: %s, want %s", got, want)
	}
	for _, c := range []struct {
		name, stat string
		auxv       []byte
	}{
		{"missing-stat", "", auxv},
		{"short-stat", "42 (garden) S 0", auxv},
		{"invalid-ticks", strings.Replace(stat, "12345", "bad", 1), auxv},
		{"missing-clock-ticks", stat, nil},
		{"truncated-auxv", stat, auxv[:word]},
		{"future-ticks", strings.Replace(stat, "12345", "60001", 1), auxv},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := linuxStarted(c.stat, c.auxv, now, 10*time.Minute); !got.IsZero() {
				t.Fatalf("unknown start time became %s", got)
			}
		})
	}
}

func TestProcessStarted(t *testing.T) {
	now := time.Now()
	if got := processStarted(os.Getpid()); got.IsZero() || got.After(now) || got.Before(now.Add(-time.Hour)) {
		t.Fatalf("native process start time: %s, now %s", got, now)
	}
}
