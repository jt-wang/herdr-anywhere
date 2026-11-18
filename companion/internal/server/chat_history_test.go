package server

import (
	"net/url"
	"testing"

	"herdr-anywhere/companion/internal/chat"
)

type pagedTurn struct {
	oneTurn
	limit bool
}

func (r *pagedTurn) Before(_, first string) (string, error) { return first, nil }
func (r *pagedTurn) History(_, before string) (chat.HistoryPage, error) {
	if r.limit {
		return chat.HistoryPage{}, chat.ErrHistoryLimit
	}
	return chat.HistoryPage{Turns: []chat.Turn{{ID: "older", From: "you", Said: "earlier question", Reply: "earlier answer", End: "done", Steps: []chat.Step{}}}}, nil
}

func TestChatHistoryHTTP(t *testing.T) {
	r := &pagedTurn{}
	f := newFixture(t, func(c *Config) {
		c.Chat = &chat.Service{HC: chatHC(t), Readers: map[string]chat.Reader{"claude-code": r}}
	})
	_, token := f.pair(t)
	code, b := getJSON(t, f, token, "/api/chat/t1")
	cursor, _ := b["before"].(string)
	session, _ := b["session"].(string)
	if code != 200 || cursor == "" || session == "" {
		t.Fatalf("initial %d %+v", code, b)
	}
	path := "/api/chat/t1?before=" + url.QueryEscape(cursor)
	if code, _ = getJSON(t, f, "", path); code != 401 {
		t.Fatalf("unpaired history %d", code)
	}
	code, b = getJSON(t, f, token, path)
	turns, _ := b["turns"].([]any)
	if code != 200 || len(turns) != 1 || turns[0].(map[string]any)["id"] != "older" || b["session"] != session || b["before"] != nil {
		t.Fatalf("older %d %+v", code, b)
	}
	if code, b = getJSON(t, f, token, "/api/chat/t1?before=bad"); code != 400 || b["error"] != "invalid-cursor" {
		t.Fatalf("bad %d %+v", code, b)
	}
	if code, b = getJSON(t, f, token, "/api/chat/t2?before="+url.QueryEscape(cursor)); code != 409 || b["error"] != "history-changed" {
		t.Fatalf("changed %d %+v", code, b)
	}
	r.limit = true
	if code, b = getJSON(t, f, token, path); code != 422 || b["error"] != "history-limit" {
		t.Fatalf("limit %d %+v", code, b)
	}
}
