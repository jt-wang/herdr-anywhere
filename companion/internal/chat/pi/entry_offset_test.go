package pi

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"herdr-anywhere/companion/internal/chat"
)

func TestEntryEndFrozenBoundsAndErrors(t *testing.T) {
	file := filepath.Join(t.TempDir(), "journal.jsonl")
	first := "\n {\"id\":\"first\"}\n"
	contents := first + "{\"id\":\"second\"}\n{\"id\":\"unfinished\"}"
	if err := os.WriteFile(file, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	if end, err := entryEnd(file, "first", -1); err != nil || end != int64(len(first)) {
		t.Fatalf("offset: %d %v", end, err)
	}
	for _, tc := range []struct {
		id  string
		end int64
	}{{"second", int64(len(first))}, {"unfinished", -1}, {"missing", -1}, {"first", int64(len(contents) + 1)}} {
		if _, err := entryEnd(file, tc.id, tc.end); !errors.Is(err, chat.ErrCursor) {
			t.Fatalf("%+v: %v", tc, err)
		}
	}
	if _, err := entryEnd(file+"-missing", "first", -1); !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("broken\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var format *chat.FormatError
	if _, err := entryEnd(file, "first", -1); !errors.As(err, &format) {
		t.Fatal(err)
	}
}
