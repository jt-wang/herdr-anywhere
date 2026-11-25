package chat

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
)

var ErrHistoryLimit = errors.New("chat history page exceeds read budget")
var ErrBadCursor = errors.New("invalid chat history cursor")

type historyCursor struct {
	Session string
	Before  string
}

type historyStart struct {
	session, first, before string
	err                    error
}

// Readers supply candidate cursors without counting a whole session. Confirm that a candidate actually has
// something to show; Pi can cross a window containing only an abandoned branch before reaching an ancestor.
func confirmedBefore(r HistoryReader, path, before string) (string, error) {
	for before != "" {
		p, err := r.History(path, before)
		if err != nil {
			return "", err
		}
		if len(p.Turns) > 0 {
			return before, nil
		}
		before = p.Before
	}
	return "", nil
}

func (s *Service) sign(b []byte) []byte {
	s.historyKey.Do(func() {
		if _, err := rand.Read(s.cursorKey[:]); err != nil {
			panic(err)
		}
	})
	h := hmac.New(sha256.New, s.cursorKey[:])
	h.Write(b)
	return h.Sum(nil)
}

// ChatPage always checks the current foreground/session. Signed cursors cannot select a path or another
// terminal/session; a terminal status change alone cannot invalidate an already read history page.
func (s *Service) ChatPage(addr, before string) (Answer, error) {
	var cursor historyCursor
	if before != "" {
		if len(before) > 4096 {
			return Answer{}, ErrBadCursor
		}
		x, y, ok := strings.Cut(before, ".")
		b, e1 := base64.RawURLEncoding.DecodeString(x)
		sig, e2 := base64.RawURLEncoding.DecodeString(y)
		if !ok || e1 != nil || e2 != nil || json.Unmarshal(b, &cursor) != nil {
			return Answer{}, ErrBadCursor
		}
		if !hmac.Equal(sig, s.sign(b)) {
			return Answer{}, ErrCursor
		}
	}
	app, r, path, version, state, msg, err := s.findMsg(addr)
	a := s.chatFound(app, r, path, version, state, msg, err)
	if err != nil || r == nil || a.Unknown != "" {
		if before != "" {
			return Answer{}, ErrCursor
		}
		return a, nil
	}
	hr, ok := r.(HistoryReader)
	if !ok {
		return a, nil
	}
	// OpenCode paths include #session; file records use the inode as well, to reject rotation at the same path.
	a.Session = s.sessionIdentity(app, addr, path)
	if before != "" && cursor.Session != a.Session {
		return Answer{}, ErrCursor
	}
	var next string
	if before == "" {
		if len(a.Turns) == 0 {
			return a, nil
		}
		first := a.Turns[0].ID
		s.mu.Lock()
		start, ok := s.historyStarts[addr]
		s.mu.Unlock()
		if ok && start.session == a.Session && start.first == first {
			next, err = start.before, start.err
		} else {
			next, err = hr.Before(path, first)
			if err == nil {
				next, err = confirmedBefore(hr, path, next)
			}
			// Appends after this boundary cannot create earlier turns. Do not reparse history at every 3 s poll.
			// A read failure can recover; only the fixed size limit is stable for an unchanged boundary.
			if err == nil || errors.Is(err, ErrHistoryLimit) {
				s.mu.Lock()
				if s.historyStarts == nil {
					s.historyStarts = map[string]historyStart{}
				}
				s.historyStarts[addr] = historyStart{session: a.Session, first: first, before: next, err: err}
				s.mu.Unlock()
			}
		}
	} else {
		var p HistoryPage
		p, err = hr.History(path, cursor.Before)
		if err != nil {
			return Answer{}, err
		}
		a.Turns = p.Turns
		next, err = confirmedBefore(hr, path, p.Before)
	}
	// A look-ahead failure must not hide the successfully read page, including an open turn larger than MaxBack.
	if err != nil {
		a.HistoryError = "history-unavailable"
		if errors.Is(err, ErrHistoryLimit) {
			a.HistoryError = "history-limit"
		}
		s.once("history "+a.Session, "chat history: %v", err)
		return a, nil
	}
	if next != "" {
		b, _ := json.Marshal(historyCursor{Session: a.Session, Before: next})
		a.Before = base64.RawURLEncoding.EncodeToString(b) + "." + base64.RawURLEncoding.EncodeToString(s.sign(b))
	}
	return a, nil
}
