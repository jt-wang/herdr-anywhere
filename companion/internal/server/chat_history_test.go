package server

import (
	"encoding/json"
	"errors"
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

type conditionalTurn struct {
	pagedTurn
	path       string
	unknown    bool
	updated    bool
	historyErr error
}

func (r *conditionalTurn) Find(int) (string, string, error) {
	if r.unknown {
		return "", "", &chat.Miss{Reason: chat.NoMatch}
	}
	return r.path, "", nil
}

func (r *conditionalTurn) Turns(path string) ([]chat.Turn, error) {
	turns, err := r.oneTurn.Turns(path)
	if r.updated {
		turns[0].Reply = "updated answer"
	}
	return turns, err
}

func (r *conditionalTurn) History(path, _ string) (chat.HistoryPage, error) {
	turns, _ := r.Turns(path)
	// Deliberately identical turns: only the page's Before metadata differs from the tail.
	return chat.HistoryPage{Turns: turns}, r.historyErr
}

func TestChatConditionalGET(t *testing.T) {
	r := &conditionalTurn{path: "/conditional-first"}
	f := newFixture(t, func(c *Config) {
		c.Chat = &chat.Service{HC: chatHC(t), Readers: map[string]chat.Reader{"claude-code": r}}
	})
	id, token := f.pair(t)
	const tail = "/api/chat/t1"
	res, body := chatGET(t, f, token, tail)
	tag := chatTag(t, res, body)
	var first chat.Answer
	if err := json.Unmarshal(body, &first); err != nil || first.Before == "" || first.Session == "" {
		t.Fatalf("initial page %s %v", body, err)
	}
	for _, validators := range [][]string{
		{tag}, {"W/" + tag}, {`"other,opaque", W/` + tag + `, "last"`}, {"*"}, {`"other"`, tag},
	} {
		res, body = chatGET(t, f, token, tail, validators...)
		if res.StatusCode != 304 || len(body) != 0 || res.Header.Get("ETag") != tag || res.Header.Get("Cache-Control") != "private, no-cache" {
			t.Fatalf("unchanged %v: %d %v %s", validators, res.StatusCode, res.Header, body)
		}
	}
	for _, validator := range []string{`"other"`, `W/"other", "last"`, `"opaque,*,tag"`, tag[1 : len(tag)-1]} {
		res, body = chatGET(t, f, token, tail, validator)
		if chatTag(t, res, body) != tag {
			t.Fatalf("nonmatching validator %q changed representation", validator)
		}
	}
	history := tail + "?before=" + url.QueryEscape(first.Before)
	res, body = chatGET(t, f, token, history, tag)
	historyTag := chatTag(t, res, body)
	if historyTag == tag {
		t.Fatal("different page with identical turns retained tail ETag")
	}
	res, body = chatGET(t, f, token, history, historyTag)
	if res.StatusCode != 304 || len(body) != 0 || res.Header.Get("ETag") != historyTag || res.Header.Get("Cache-Control") != "private, no-cache" {
		t.Fatalf("unchanged history: %d %v %s", res.StatusCode, res.Header, body)
	}
	res, body = chatGET(t, f, token, tail, historyTag)
	if chatTag(t, res, body) != tag {
		t.Fatal("history validator changed tail representation")
	}
	r.updated = true
	for _, page := range []struct{ path, tag string }{{tail, tag}, {history, historyTag}} {
		res, body = chatGET(t, f, token, page.path, page.tag)
		if chatTag(t, res, body) == page.tag {
			t.Fatal("updated record retained ETag")
		}
	}
	r.updated = false
	checkError := func(path string, code int, message string) {
		t.Helper()
		for _, validator := range []string{tag, "*"} {
			res, body := chatGET(t, f, token, path, validator)
			var got map[string]string
			if err := json.Unmarshal(body, &got); err != nil || res.StatusCode != code || got["error"] != message || res.Header.Get("ETag") != "" || res.Header.Get("Cache-Control") != "no-store" {
				t.Fatalf("error with %q: %d %v %s %v", validator, res.StatusCode, res.Header, body, err)
			}
		}
	}
	checkError(tail+"?before=bad", 400, "invalid-cursor")
	for _, c := range []struct {
		err     error
		code    int
		message string
	}{
		{chat.ErrCursor, 409, "history-changed"},
		{chat.ErrHistoryLimit, 422, "history-limit"},
		{errors.New("record unavailable"), 409, "history-unavailable"},
	} {
		r.historyErr = c.err
		checkError(history, c.code, c.message)
	}
	r.historyErr = nil
	// Session identity alone must change the tag, even when every turn is identical.
	r.path = "/conditional-second"
	res, body = chatGET(t, f, token, tail, tag)
	if chatTag(t, res, body) == tag {
		t.Fatal("changed session retained ETag")
	}
	checkError(history, 409, "history-changed")
	// Metadata-only recovery (HistoryError disappears and Before appears) also changes the tag.
	r.path = "/conditional-third"
	r.historyErr = errors.New("temporarily unavailable")
	res, body = chatGET(t, f, token, tail)
	metadataTag := chatTag(t, res, body)
	r.historyErr = nil
	res, body = chatGET(t, f, token, tail, metadataTag)
	if chatTag(t, res, body) == metadataTag {
		t.Fatal("history metadata recovery retained ETag")
	}
	currentTag := res.Header.Get("ETag")
	r.unknown = true
	for _, path := range []string{tail, "/api/chat/t2", "/api/chat/t9"} {
		res, body = chatGET(t, f, token, path, currentTag)
		if chatTag(t, res, body) == currentTag {
			t.Fatalf("unknown/missing state %s retained successful ETag", path)
		}
	}
	checkError(history, 409, "history-changed")
	for _, badToken := range []string{"", "invalid"} {
		res, body = chatGET(t, f, badToken, tail, "*")
		if res.StatusCode != 401 || len(body) == 0 || res.Header.Get("ETag") != "" {
			t.Fatalf("unauthenticated conditional request: %d %v %s", res.StatusCode, res.Header, body)
		}
	}
	if err := f.store.Revoke(id); err != nil {
		t.Fatal(err)
	}
	res, body = chatGET(t, f, token, tail, tag)
	if res.StatusCode != 401 || len(body) == 0 || res.Header.Get("ETag") != "" {
		t.Fatalf("revoked conditional request: %d %v %s", res.StatusCode, res.Header, body)
	}
}
