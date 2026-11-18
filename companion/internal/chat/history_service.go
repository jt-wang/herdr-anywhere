package chat

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

var ErrHistoryLimit = errors.New("chat history page exceeds read budget")
var ErrBadCursor = errors.New("invalid chat history cursor")

type historyCursor struct {
	Session string
	Before  string
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

// ChatPage always checks the current foreground/session and the LATEST turn's screen agreement, never the
// completed turn at the end of an older page. Signed cursors cannot select a path or another terminal/session.
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
	if err != nil || r == nil || (a.Unknown != "" && a.Unknown != Behind) {
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
	identity := app + "\x00" + addr + "\x00" + path
	if st, e := os.Stat(path); e == nil {
		identity += fmt.Sprint("\x00", Inode(st))
	}
	a.Session = base64.RawURLEncoding.EncodeToString(s.sign([]byte(identity)))
	if before != "" && cursor.Session != a.Session {
		return Answer{}, ErrCursor
	}
	var next string
	if before == "" {
		if len(a.Turns) == 0 {
			return a, nil
		}
		next, err = hr.Before(path, a.Turns[0].ID)
		if err != nil {
			return Answer{}, err
		}
	} else {
		p, err := hr.History(path, cursor.Before)
		if err != nil {
			return Answer{}, err
		}
		a.Turns = p.Turns
		next = p.Before
	}
	if next != "" {
		b, _ := json.Marshal(historyCursor{Session: a.Session, Before: next})
		a.Before = base64.RawURLEncoding.EncodeToString(b) + "." + base64.RawURLEncoding.EncodeToString(s.sign(b))
	}
	return a, nil
}
