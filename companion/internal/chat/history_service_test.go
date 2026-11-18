package chat

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

type historyReader struct {
	fakeReader
	calls int
}

func (r *historyReader) Before(_, first string) (string, error) { return first, nil }
func (r *historyReader) History(_, before string) (HistoryPage, error) {
	r.calls++
	if before != "u1" {
		return HistoryPage{}, ErrCursor
	}
	return HistoryPage{Turns: []Turn{{ID: "old", End: "done"}}}, nil
}

func TestChatHistoryPreservesLatestSafetyAndDefault(t *testing.T) {
	for _, latest := range [][]Turn{openTurn, doneTurn} {
		r := &historyReader{fakeReader: fakeReader{path: "/session", turns: latest}}
		s, _, _ := service(working, r)
		want := s.Chat("t1")
		a, err := s.ChatPage("t1", "")
		if err != nil || a.Before == "" || a.Session == "" {
			t.Fatalf("initial: %+v %v", a, err)
		}
		if !reflect.DeepEqual(a.Turns, want.Turns) || a.Unknown != want.Unknown || r.calls != 0 {
			t.Fatalf("default changed or reparsed: %+v", a)
		}
		old, err := s.ChatPage("t1", a.Before)
		if err != nil || len(old.Turns) != 1 || old.Turns[0].ID != "old" || old.Unknown != want.Unknown || old.Session != a.Session || old.Before != "" {
			t.Fatalf("older: %+v %v", old, err)
		}
		// A cursor must not silently return a new session's old turns, even in the same terminal.
		r.path = "/other-session"
		if _, err = s.ChatPage("t1", a.Before); !errors.Is(err, ErrCursor) {
			t.Fatalf("session changed: %v", err)
		}
		if r.calls != 1 {
			t.Fatal("read old history after session changed")
		}
		r.findErr = &Miss{Reason: Ambiguous}
		if _, err = s.ChatPage("t1", a.Before); !errors.Is(err, ErrCursor) {
			t.Fatalf("ambiguous session: %v", err)
		}
	}
}

func TestChatHistoryCursorBoundToTerminalAndRecord(t *testing.T) {
	p := filepath.Join(t.TempDir(), "session")
	if err := os.WriteFile(p, []byte("record"), 0600); err != nil {
		t.Fatal(err)
	}
	r := &historyReader{fakeReader: fakeReader{path: p, turns: doneTurn}}
	s, _, _ := service(done, r)
	a, err := s.ChatPage("t1", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ChatPage("t2", a.Before); !errors.Is(err, ErrCursor) {
		t.Fatalf("other terminal: %v", err)
	}
	if _, err = s.ChatPage("t1", "bad"); !errors.Is(err, ErrBadCursor) {
		t.Fatalf("bad cursor: %v", err)
	}
	st, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if Inode(st) == 0 {
		t.Skip("this platform's FileInfo does not expose record replacement identity")
	}
	// Replacing the file at its old name is not the same record. Rename instead of remove: reliable inode change.
	if err := os.Rename(p, p+".old"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("replacement"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ChatPage("t1", a.Before); !errors.Is(err, ErrCursor) {
		t.Fatalf("rotated record: %v", err)
	}
	if r.calls != 0 {
		t.Fatal("read history from wrong terminal/record")
	}
}
