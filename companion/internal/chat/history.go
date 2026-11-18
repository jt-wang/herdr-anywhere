package chat

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
)

// HistoryReader reads a bounded, chronological page. The cursor is private to the reader.
type HistoryReader interface {
	History(path, before string) (HistoryPage, error)
	Before(path, first string) (string, error)
}

type HistoryPage struct {
	Turns  []Turn
	Before string
}

var ErrCursor = errors.New("chat history changed")

type fileCursor struct {
	End    int64
	Skip   int
	Head   string
	Anchor string
}

// FileBefore freezes the initial end without reparsing the polling cache. An empty final page is allowed:
// the normal tail deliberately does not count the complete session just to decide whether to show a button.
func FileBefore(path, first string) (string, error) {
	st, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	b, _ := json.Marshal(fileCursor{End: st.Size(), Anchor: first})
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// FileHistory searches at most MaxBack bytes and parses at most MaxBack, never a growing prefix of the record.
// Small turns are paged within that fixed window; exhausted windows continue at a prompt boundary. No history
// is kept in the polling cache.
func FileHistory(path, before string, isStart func([]byte) bool, parse func(int64, int64) ([]Turn, error)) (HistoryPage, error) {
	return FileBranchHistory(path, before, isStart, func(off, end int64, _ string) ([]Turn, string, error) {
		ts, err := parse(off, end)
		return ts, "continue", err
	})
}

// FileBranchHistory also carries the parent entry at the edge of a branching record (Pi).
func FileBranchHistory(path, before string, isStart func([]byte) bool, parse func(int64, int64, string) ([]Turn, string, error)) (HistoryPage, error) {
	f, err := os.Open(path)
	if err != nil {
		return HistoryPage{}, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return HistoryPage{}, err
	}
	c := fileCursor{End: st.Size()}
	if before != "" {
		b, err := base64.RawURLEncoding.DecodeString(before)
		if err != nil || json.Unmarshal(b, &c) != nil || c.End <= 0 || c.End > st.Size() || c.Skip < 0 {
			return HistoryPage{}, ErrCursor
		}
	}
	floor := max(c.End-MaxBack, 0)
	b := make([]byte, c.End-floor)
	if _, err := f.ReadAt(b, floor); err != nil {
		return HistoryPage{}, err
	}
	// Include the prompt preceding the tail, so the first older page also has the complete turn the normal
	// tail dropped. If no prompt precedes it inside the budget, use the first whole prompt. Skip a cut first line.
	off, preceding := int64(-1), int64(-1)
	pos := 0
	for pos < len(b) {
		i := bytes.IndexByte(b[pos:], '\n')
		if i < 0 {
			break
		}
		at := floor + int64(pos)
		line := b[pos : pos+i]
		if (pos > 0 || floor == 0) && isStart(line) {
			if at >= c.End-MaxTail {
				off = at
				if preceding >= 0 {
					off = preceding
				}
				break
			}
			preceding = at
		}
		pos += i + 1
	}
	if off < 0 {
		off = preceding
	}
	if off < 0 {
		if floor > 0 {
			return HistoryPage{}, ErrHistoryLimit
		}
		off = floor
	}
	turns, head, err := parse(off, c.End, c.Head)
	if err != nil {
		return HistoryPage{}, err
	}
	if c.Skip > len(turns) {
		return HistoryPage{}, ErrCursor
	}
	if c.Anchor != "" {
		found := false
		for i, t := range turns {
			if t.ID == c.Anchor {
				c.Skip = len(turns) - i
				found = true
				break
			}
		}
		if !found {
			return HistoryPage{}, ErrCursor
		}
		c.Anchor = ""
	}
	turns = turns[:len(turns)-c.Skip]
	p := HistoryPage{}
	if len(turns) > MaxTurns {
		p.Turns = turns[len(turns)-MaxTurns:]
		c.Skip += MaxTurns
	} else {
		p.Turns = turns
		c = fileCursor{End: off, Head: head}
		if head == "" {
			c.End = 0
		}
	}
	if c.End > 0 {
		b, _ := json.Marshal(c)
		p.Before = base64.RawURLEncoding.EncodeToString(b)
	}
	return p, nil
}
