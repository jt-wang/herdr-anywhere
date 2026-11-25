package chat

import (
	"encoding/base64"
	"fmt"
	"os"
	"strings"
	"unicode"
)

// TitleAnswer contains only display metadata, never transcript content or local paths.
type TitleAnswer struct {
	Title   string `json:"title,omitempty"`
	Session string `json:"session,omitempty"`
}

// TitleReader is optional: only readers with existing, attributable session titles implement it.
type TitleReader interface {
	Title(path string) (string, error)
}

// Title uses the same foreground-process discovery as chat, without parsing any turns.
func (s *Service) Title(addr string) TitleAnswer {
	app, r, path, _, _, err := s.find(addr)
	if err != nil || r == nil || path == "" {
		return TitleAnswer{}
	}
	tr, ok := r.(TitleReader)
	if !ok {
		return TitleAnswer{}
	}
	title, err := tr.Title(path)
	if err != nil {
		return TitleAnswer{}
	}
	title = strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return ' '
		}
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, title)
	title = strings.Join(strings.Fields(title), " ")
	runes := []rune(title)
	if len(runes) > 160 {
		title = string(runes[:160])
	}
	if title == "" {
		return TitleAnswer{}
	}
	return TitleAnswer{Title: title, Session: s.sessionIdentity(app, addr, path)}
}

// sessionIdentity is shared with chat history: stable across appends, different after file rotation.
func (s *Service) sessionIdentity(app, addr, path string) string {
	identity := app + "\x00" + addr + "\x00" + path
	if st, e := os.Stat(path); e == nil {
		identity += fmt.Sprint("\x00", Inode(st))
	}
	return base64.RawURLEncoding.EncodeToString(s.sign([]byte(identity)))
}
