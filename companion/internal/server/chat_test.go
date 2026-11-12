package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"herdr-anywhere/companion/internal/chat"
	"herdr-anywhere/companion/internal/testfake"
)

type oneTurn struct{}

func (oneTurn) Find(pid int) (string, string, error) {
	if pid != 4242 {
		return "", "", &chat.Miss{Reason: chat.NoMatch}
	}
	return "/s", "2.1.319", nil
}
func (oneTurn) Turns(string) ([]chat.Turn, error) {
	return []chat.Turn{{ID: "u1", From: "you", Said: "hi", Steps: []chat.Step{{ID: "toolu_1", Kind: "run", Title: "List files"}}, Reply: "hello", End: "done"}}, nil
}
func (oneTurn) Detail(_, id string) (chat.Detail, error) {
	if id != "toolu_1" {
		return chat.Detail{}, chat.ErrNoStep
	}
	return chat.Detail{Input: "ls", Output: "a.txt"}, nil
}
func (oneTurn) Image(_, id string) (string, []byte, error) {
	if id != "toolu_1.0" {
		return "", nil, chat.ErrNoStep
	}
	return "image/png", []byte("\x89PNG"), nil
}

// chatHC is an hc whose `get t1` is a Claude Code that is done, and every other terminal unknown.
// chatHC: an hc whose get answers for t1 (Claude Code, done) and t2 (a shell), and says not_found for any other.
// A compiled fake (testfake), so it runs on Windows too.
func chatHC(t *testing.T) string {
	d := t.TempDir()
	os.WriteFile(filepath.Join(d, "get-t1.json"), []byte(`{"status":{"app":"claude-code","state":"done"},"foreground":{"pid":4242}}`), 0o644)
	os.WriteFile(filepath.Join(d, "get-t2.json"), []byte(`{"status":null,"foreground":{"pid":1}}`), 0o644)
	os.WriteFile(filepath.Join(d, "not-found.json"), []byte(`{"error":"not_found"}`), 0o644)
	got := filepath.Join(d, "get-{2}.json")
	return testfake.HC(t, filepath.Join(d, "bin"), "hc", testfake.Spec{Version: "hc 0.1.20", On: map[string][]testfake.Reply{
		"get": {{If: got, Cat: got}, {Fail: filepath.Join(d, "not-found.json")}},
	}, Else: 1})
}

func getJSON(t *testing.T, f *fixture, token, path string) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest("GET", f.srv.URL+path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	var body map[string]any
	json.NewDecoder(r.Body).Decode(&body)
	return r.StatusCode, body
}

func chatFixture(t *testing.T) (*fixture, string) {
	hc := chatHC(t)
	f := newFixture(t, func(c *Config) {
		c.Chat = &chat.Service{HC: hc, Readers: map[string]chat.Reader{"claude-code": oneTurn{}}}
	})
	_, token := f.pair(t)
	return f, token
}

func TestChatNeedsAPairedDevice(t *testing.T) {
	f, _ := chatFixture(t)
	if code, _ := getJSON(t, f, "", "/api/chat/t1"); code != 401 {
		t.Fatalf("%d", code)
	}
	if code, _ := getJSON(t, f, "nope", "/api/chat/t1/steps/toolu_1"); code != 401 {
		t.Fatalf("%d", code)
	}
}

func TestChatTurns(t *testing.T) {
	f, token := chatFixture(t)
	code, body := getJSON(t, f, token, "/api/chat/t1")
	turns, _ := body["turns"].([]any)
	if code != 200 || body["chat"] != true || len(turns) != 1 {
		t.Fatalf("%d %v", code, body)
	}
	if turn := turns[0].(map[string]any); turn["reply"] != "hello" {
		t.Fatalf("%v", turn)
	}
	for _, a := range []string{"t2", "t9", "box%2Ft7"} {
		code, body := getJSON(t, f, token, "/api/chat/"+a)
		if code != 200 || body["chat"] != false {
			t.Fatalf("%s: %d %v", a, code, body)
		}
	}
}

func TestChatStep(t *testing.T) {
	f, token := chatFixture(t)
	code, body := getJSON(t, f, token, "/api/chat/t1/steps/toolu_1")
	if code != 200 || body["input"] != "ls" || body["output"] != "a.txt" {
		t.Fatalf("%d %v", code, body)
	}
	if code, _ := getJSON(t, f, token, "/api/chat/t1/steps/toolu_9"); code != 404 {
		t.Fatalf("%d", code)
	}
}

// A companion built without chat answers that there is none, as for any terminal without a reader.
func TestNoChatService(t *testing.T) {
	f := newFixture(t, nil)
	_, token := f.pair(t)
	if code, body := getJSON(t, f, token, "/api/chat/t1"); code != 200 || body["chat"] != false {
		t.Fatalf("%d %v", code, body)
	}
}

// An image goes to the phone as a data: URL, which the app's Image shows as it is.
func TestChatImage(t *testing.T) {
	f, token := chatFixture(t)
	code, body := getJSON(t, f, token, "/api/chat/t1/images/toolu_1.0")
	if code != 200 || body["url"] != "data:image/png;base64,iVBORw==" {
		t.Fatalf("%d %v", code, body)
	}
	if code, _ := getJSON(t, f, token, "/api/chat/t1/images/toolu_1.1"); code != 404 {
		t.Fatalf("%d", code)
	}
	if code, _ := getJSON(t, f, "", "/api/chat/t1/images/toolu_1.0"); code != 401 {
		t.Fatalf("no token: %d", code)
	}
}
