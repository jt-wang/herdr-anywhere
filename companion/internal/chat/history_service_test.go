package chat

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

type historyReader struct {
	fakeReader
	calls int
	pages map[string]HistoryPage
}

func (r *historyReader) Before(_, first string) (string, error) { return first, nil }
func (r *historyReader) History(_, before string) (HistoryPage, error) {
	r.calls++
	if r.pages != nil {
		p, ok := r.pages[before]
		if !ok {
			return HistoryPage{}, ErrCursor
		}
		return p, nil
	}
	if before != "u1" {
		return HistoryPage{}, ErrCursor
	}
	return HistoryPage{Turns: []Turn{{ID: "old", End: "done"}}}, nil
}

func TestChatHistoryOnlyOffersNonemptyPages(t *testing.T) {
	for _, n := range []int{1, 4, MaxTurns, MaxTurns + 1, 2 * MaxTurns} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			all := make([]Turn, n)
			for i := range all {
				all[i] = Turn{ID: fmt.Sprintf("u%d", i), From: "you", Said: "Inspect the grove", Reply: "Ready", End: "done"}
			}
			// The last turn is still working, including a new session's first five tool calls.
			all[n-1].End, all[n-1].Reply = "open", ""
			all[n-1].Steps = make([]Step, 5)
			start := max(n-MaxTurns, 0)
			first := all[start].ID
			r := &historyReader{fakeReader: fakeReader{path: "/session", turns: all[start:]}, pages: map[string]HistoryPage{
				first: {Turns: all[:start]},
			}}
			s, _, _ := service(working, r)
			a, err := s.ChatPage("t1", "")
			if err != nil || (a.Before != "") != (n > MaxTurns) || len(a.Turns) != min(n, MaxTurns) {
				t.Fatalf("initial page: turns=%d before=%q err=%v", len(a.Turns), a.Before, err)
			}
			for range 3 {
				poll, err := s.ChatPage("t1", "")
				if err != nil || poll.Before != a.Before {
					t.Fatalf("poll: %+v %v", poll, err)
				}
			}
			if a.Before != "" {
				older, err := s.ChatPage("t1", a.Before)
				if err != nil || len(older.Turns) != start || older.Before != "" {
					t.Fatalf("last page: %+v %v", older, err)
				}
			}
		})
	}
}

func TestChatHistorySkipsEmptyBranchWindows(t *testing.T) {
	r := &historyReader{fakeReader: fakeReader{path: "/session", turns: openTurn}, pages: map[string]HistoryPage{
		"u1":        {Before: "abandoned"},
		"abandoned": {Before: "ancestor"},
		"ancestor":  {Turns: []Turn{{ID: "root", End: "done"}}},
	}}
	s, _, _ := service(working, r)
	a, err := s.ChatPage("t1", "")
	if err != nil || a.Before == "" {
		t.Fatalf("initial: %+v %v", a, err)
	}
	a, err = s.ChatPage("t1", a.Before)
	if err != nil || len(a.Turns) != 1 || a.Turns[0].ID != "root" || a.Before != "" {
		t.Fatalf("offered an empty window instead of an ancestor: %+v %v", a, err)
	}
}

func TestChatHistoryProbeRecoversWithoutLosingTheCurrentPage(t *testing.T) {
	r := &historyReader{fakeReader: fakeReader{path: "/session", turns: openTurn}, pages: map[string]HistoryPage{}}
	s, _, _ := service(working, r)
	a, err := s.ChatPage("t1", "")
	if err != nil || !reflect.DeepEqual(a.Turns, openTurn) || a.Before != "" || a.HistoryError != "history-unavailable" {
		t.Fatalf("current page lost to failed look-ahead: %+v %v", a, err)
	}
	r.pages["u1"] = HistoryPage{Turns: []Turn{{ID: "root", End: "done"}}}
	a, err = s.ChatPage("t1", "")
	if err != nil || a.Before == "" || a.HistoryError != "" || !reflect.DeepEqual(a.Turns, openTurn) {
		t.Fatalf("look-ahead did not recover: %+v %v", a, err)
	}
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
		if !reflect.DeepEqual(a.Turns, want.Turns) || a.Unknown != want.Unknown {
			t.Fatalf("default changed: %+v", a)
		}
		old, err := s.ChatPage("t1", a.Before)
		if err != nil || len(old.Turns) != 1 || old.Turns[0].ID != "old" || old.Unknown != want.Unknown || old.Session != a.Session || old.Before != "" {
			t.Fatalf("older: %+v %v", old, err)
		}
		// A cursor must not silently return a new session's old turns, even in the same terminal.
		calls := r.calls
		r.path = "/other-session"
		if _, err = s.ChatPage("t1", a.Before); !errors.Is(err, ErrCursor) {
			t.Fatalf("session changed: %v", err)
		}
		if r.calls != calls {
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
	calls := r.calls
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
	if r.calls != calls {
		t.Fatal("read history from wrong terminal/record")
	}
}
