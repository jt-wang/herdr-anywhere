package server

import (
	"herdr-anywhere/companion/internal/chat"
	"testing"
)

type titledReader struct{ oneTurn }

func (titledReader) Title(string) (string, error)      { return "Repair garden gate", nil }
func (titledReader) Turns(string) ([]chat.Turn, error) { panic("read chat") }
func TestChatTitle(t *testing.T) {
	hc := chatHC(t)
	f := newFixture(t, func(c *Config) {
		c.Chat = &chat.Service{HC: hc, Readers: map[string]chat.Reader{"claude-code": titledReader{}}}
	})
	_, token := f.pair(t)
	for _, auth := range []string{"", "invalid"} {
		if code, _ := getJSON(t, f, auth, "/api/chat/t1/title"); code != 401 {
			t.Fatalf("auth %d", code)
		}
	}
	code, b := getJSON(t, f, token, "/api/chat/t1/title")
	if code != 200 || b["title"] != "Repair garden gate" || b["session"] == "" || len(b) != 2 {
		t.Fatalf("%d %v", code, b)
	}
	for _, a := range []string{"t2", "t9", "box%2Ft7"} {
		code, b = getJSON(t, f, token, "/api/chat/"+a+"/title")
		if code != 200 || len(b) != 0 {
			t.Fatalf("%d %v", code, b)
		}
	}
}
