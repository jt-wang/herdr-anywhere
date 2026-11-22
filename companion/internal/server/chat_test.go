package server

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"herdr-anywhere/companion/internal/chat"
	"herdr-anywhere/companion/internal/chat/claude"
	"herdr-anywhere/companion/internal/chat/codex"
	"herdr-anywhere/companion/internal/chat/opencode"
	"herdr-anywhere/companion/internal/chat/pi"
	"herdr-anywhere/companion/internal/testfake"
	"herdr-anywhere/companion/internal/testfake/replay"
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
	res, body := chatGET(t, f, token, "/api/chat/t1")
	tag := chatTag(t, res, body)
	res, body = chatGET(t, f, token, "/api/chat/t1", tag)
	if res.StatusCode != 304 || len(body) != 0 || res.Header.Get("ETag") != tag || res.Header.Get("Cache-Control") != "private, no-cache" {
		t.Fatalf("unchanged absent service: %d %v %s", res.StatusCode, res.Header, body)
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

func TestOMPExternalizedImageEndpoint(t *testing.T) {
	dir := filepath.Join("..", "chat", "pi", "testdata", "omp", "19.0.0", "externalized")
	reader := &pi.Reader{Setup: &pi.Setup{ID: "omp", BlobDir: filepath.Join(dir, "blobs")}}
	r := &chatLocatedReader{Reader: reader, HistoryReader: reader, path: filepath.Join(dir, "images.jsonl")}
	hc := chatHC(t)
	f := newFixture(t, func(c *Config) {
		c.Chat = &chat.Service{HC: hc, Readers: map[string]chat.Reader{"claude-code": r}}
	})
	_, token := f.pair(t)
	files, err := filepath.Glob(filepath.Join(dir, "blobs", "*"))
	if err != nil || len(files) != 1 {
		t.Fatalf("blob fixture: %v %v", files, err)
	}
	want, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"u1:1", "read1%230"} {
		code, body := getJSON(t, f, token, "/api/chat/t1/images/"+id)
		if code != 200 || body["url"] != "data:image/png;base64,"+base64.StdEncoding.EncodeToString(want) {
			t.Fatalf("image %s: %d %v", id, code, body)
		}
	}
}

func TestOMPTreeSwitchChangesETagAndHistoryIdentity(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", dir)
	t.Setenv("OMP_PROFILE", "")
	s := pi.OMPSetup("/sample/hc")
	if err := s.Install(false); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "sessions"), 0o700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "sessions", "tree.jsonl")
	journal := `{"type":"session","id":"s1","cwd":"/sample"}` + "\n"
	parent := "null"
	for i := 0; i < 40; i++ {
		journal += fmt.Sprintf(`{"type":"message","id":"u%d","parentId":%s,"message":{"role":"user","content":"sample question"}}`+"\n", i, parent)
		journal += fmt.Sprintf(`{"type":"message","id":"a%d","parentId":"u%d","message":{"role":"assistant","content":[{"type":"text","text":"sample answer"}],"stopReason":"stop"}}`+"\n", i, i)
		parent = fmt.Sprintf(`"a%d"`, i)
	}
	if err := os.WriteFile(file, []byte(journal), 0o600); err != nil {
		t.Fatal(err)
	}
	core := filepath.Join(dir, "terminal.json")
	selectLeaf := func(leaf string) {
		t.Helper()
		b, _ := json.Marshal(map[string]any{
			"status":     map[string]string{"app": "omp", "state": "done"},
			"foreground": map[string]int{"pid": 4242},
			"labels":     map[string]string{chat.LabelPid: "4242", chat.LabelSession: file, "herdr-anywhere.chat.leaf": leaf + ":a39"},
		})
		if err := os.WriteFile(core, b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	selectLeaf("a39")
	hc := testfake.HC(t, filepath.Join(dir, "bin"), "hc", testfake.Spec{On: map[string][]testfake.Reply{"get": {{Cat: core}}}})
	f := newFixture(t, func(c *Config) {
		c.Chat = &chat.Service{HC: hc, Readers: map[string]chat.Reader{"omp": &pi.Reader{Setup: s}}}
	})
	_, token := f.pair(t)
	res, body := chatGET(t, f, token, "/api/chat/t1")
	tag := chatTag(t, res, body)
	var first chat.Answer
	if err := json.Unmarshal(body, &first); err != nil || first.Before == "" || first.Session == "" {
		t.Fatalf("initial history: %s %v", body, err)
	}
	selectLeaf("a0")
	res, body = chatGET(t, f, token, "/api/chat/t1", tag)
	if next := chatTag(t, res, body); next == tag {
		t.Fatal("tree switch retained ETag")
	}
	var next chat.Answer
	if err := json.Unmarshal(body, &next); err != nil || next.Session == first.Session || len(next.Turns) != 1 || next.Turns[0].ID != "u0" {
		t.Fatalf("switched branch: %s %v", body, err)
	}
	res, body = chatGET(t, f, token, "/api/chat/t1?before="+url.QueryEscape(first.Before))
	if res.StatusCode != http.StatusConflict {
		t.Fatalf("old branch cursor accepted: %d %s", res.StatusCode, body)
	}
}

func chatGET(t *testing.T, f *fixture, token, path string, validators ...string) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, f.srv.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for _, v := range validators {
		req.Header.Add("If-None-Match", v)
	}
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	b, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatal(err)
	}
	return r, b
}

func chatTag(t *testing.T, r *http.Response, body []byte) string {
	t.Helper()
	tag := r.Header.Get("ETag")
	if want := fmt.Sprintf(`"%x"`, sha256.Sum256(body)); r.StatusCode != 200 || tag != want || r.Header.Get("Cache-Control") != "private, no-cache" || r.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("representation: status=%d headers=%v body=%s (want ETag %s)", r.StatusCode, r.Header, body, want)
	}
	return tag
}

// Only discovery is fixed; real replay records and all four production readers run behind the public HTTP route.
type chatLocatedReader struct {
	chat.Reader
	chat.HistoryReader
	path string
}

func (r *chatLocatedReader) Find(int) (string, string, error) { return r.path, "", nil }

func TestChatConditionalGETReplay(t *testing.T) {
	for _, c := range []struct {
		agent, fixture string
		reader         chat.Reader
	}{
		{"claude-code", "claude/testdata/claude-code/2.1.325/steps.jsonl", &claude.Reader{}},
		{"codex", "codex/testdata/codex/0.167.1/steps.jsonl", &codex.Reader{}},
		{"opencode", "opencode/testdata/opencode/2.2.0/steps.json", &opencode.Reader{}},
		{"pi", "pi/testdata/pi/1.4.0/steps.jsonl", &pi.Reader{}},
		{"omp", "pi/testdata/omp/19.0.0/turns.jsonl", &pi.Reader{}},
	} {
		t.Run(c.agent, func(t *testing.T) {
			format := c.agent
			if format == "omp" {
				format = "pi"
			}
			script, err := replay.Load(format, filepath.Join("..", "chat", c.fixture))
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			player, err := script.Start(replay.Options{Dir: dir, Cwd: "/work/meadow", Pid: 4242})
			if err != nil {
				t.Fatal(err)
			}
			defer player.Close()
			if err := player.Play("Inspect the meadow", 0); err != nil {
				t.Fatal(err)
			}
			core := filepath.Join(dir, "terminal.json")
			b, _ := json.Marshal(map[string]any{"status": map[string]string{"app": c.agent, "state": "done"}, "foreground": map[string]int{"pid": 4242}})
			if err := os.WriteFile(core, b, 0o600); err != nil {
				t.Fatal(err)
			}
			hc := testfake.HC(t, filepath.Join(dir, "bin"), "hc", testfake.Spec{On: map[string][]testfake.Reply{"get": {{Cat: core}}}})
			r := &chatLocatedReader{Reader: c.reader, HistoryReader: c.reader.(chat.HistoryReader), path: player.Path()}
			f := newFixture(t, func(cfg *Config) {
				cfg.Chat = &chat.Service{HC: hc, Readers: map[string]chat.Reader{c.agent: r}}
			})
			_, token := f.pair(t)
			res, body := chatGET(t, f, token, "/api/chat/t1")
			tag := chatTag(t, res, body)
			var first chat.Answer
			if err := json.Unmarshal(body, &first); err != nil || !first.Chat || first.Unknown != "" || first.Session == "" || len(first.Turns) != 1 || first.Turns[0].End != "done" {
				t.Fatalf("recorded first turn: %s %v", body, err)
			}
			res, body = chatGET(t, f, token, "/api/chat/t1", tag)
			if res.StatusCode != 304 || len(body) != 0 || res.Header.Get("ETag") != tag {
				t.Fatalf("unchanged replay: %d %v %s", res.StatusCode, res.Header, body)
			}
			if err := player.Play("Inspect the clearing", 0); err != nil {
				t.Fatal(err)
			}
			res, body = chatGET(t, f, token, "/api/chat/t1", tag)
			if next := chatTag(t, res, body); next == tag {
				t.Fatal("appended record retained ETag")
			}
			var next chat.Answer
			if err := json.Unmarshal(body, &next); err != nil || next.Session != first.Session || len(next.Turns) != 2 || next.Turns[1].Said != "Inspect the clearing" || next.Turns[1].End != "done" {
				t.Fatalf("recorded append: %s %v", body, err)
			}
			if err := player.Preload(chat.MaxTurns + 2); err != nil {
				t.Fatal(err)
			}
			res, body = chatGET(t, f, token, "/api/chat/t1")
			tailTag := chatTag(t, res, body)
			if err := json.Unmarshal(body, &next); err != nil || next.Before == "" {
				t.Fatalf("recorded history boundary: %s %v", body, err)
			}
			history := "/api/chat/t1?before=" + url.QueryEscape(next.Before)
			res, body = chatGET(t, f, token, history, tailTag)
			historyTag := chatTag(t, res, body)
			var older chat.Answer
			if err := json.Unmarshal(body, &older); err != nil || older.Session != first.Session || len(older.Turns) == 0 || historyTag == tailTag {
				t.Fatalf("recorded history: %s %v", body, err)
			}
			res, body = chatGET(t, f, token, history, historyTag)
			if res.StatusCode != 304 || len(body) != 0 || res.Header.Get("ETag") != historyTag {
				t.Fatalf("unchanged recorded history: %d %v %s", res.StatusCode, res.Header, body)
			}
		})
	}
}
