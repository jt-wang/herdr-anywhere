package chat

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestHistoryReadBudget(t *testing.T) {
	// Sparse, disposable 64 MB record with no prompt inside the allowed window. Parsing it would be unbounded.
	p := filepath.Join(t.TempDir(), "record")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Truncate(2 * MaxBack); err != nil {
		t.Fatal(err)
	}
	f.Close()
	parsed := false
	_, err = FileHistory(p, "", func([]byte) bool { return false }, func(off, end int64) ([]Turn, error) { parsed = true; return nil, nil })
	if !errors.Is(err, ErrHistoryLimit) || parsed {
		t.Fatalf("budget: parsed=%v, err=%v", parsed, err)
	}
}

func TestHistoryFixedEndAndTruncation(t *testing.T) {
	p := record(t, "one", "two", "three")
	st, _ := os.Stat(p)
	var rerr error
	n := 0
	for range LinesUntil(p, 0, st.Size(), &rerr) {
		n++
	}
	if rerr != nil || n != 3 {
		t.Fatalf("lines %d %v", n, rerr)
	}
	cursor, err := FileBefore(p, "three")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Truncate(p, 1); err != nil {
		t.Fatal(err)
	}
	_, err = FileHistory(p, cursor, isStart, func(off, end int64) ([]Turn, error) { t.Fatal("parsed truncated record"); return nil, nil })
	if !errors.Is(err, ErrCursor) {
		t.Fatalf("truncation: %v", err)
	}
}
