package server

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"herdr-anywhere/companion/internal/chat"
	"herdr-anywhere/companion/internal/chat/pi"
	"herdr-anywhere/companion/internal/testfake"
)

// The last release's app, frozen: every request it sends, what it reads of each answer, and the answer its own
// Companion gave (app/fixtures/contract). A phone keeps that app until its person updates it, so the Companion of
// today must still answer it. app/src/contract.test.ts checks the other way: today's app against those answers.
// HM_CONTRACT_RECORD=1 writes the answers this Companion gives into the file; run it on the release's own source.
const contractFile = "../../../app/fixtures/contract/v0.3.11.json"

type contract struct {
	Release string         `json:"release"`
	Source  string         `json:"source"`
	About   string         `json:"about"`
	Calls   []contractCall `json:"calls"`
}

type contractCall struct {
	Name    string            `json:"name"`
	From    string            `json:"from"`
	Method  string            `json:"method"`
	Path    string            `json:"path"`
	Query   map[string]string `json:"query,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    json.RawMessage   `json:"body,omitempty"`
	Text    string            `json:"text,omitempty"`
	// Socket is the encoding of a WebSocket the app opens (hc/ws.ts): "bin" or "b64"; Send, the bytes it sends, in
	// base64. The answer's Text is the bytes that came back, in base64 too.
	Socket string `json:"socket,omitempty"`
	Send   string `json:"send,omitempty"`
	// Prepare is what happens on the computer first: "edit-setup", its person edits the chat setup's file;
	// "remote-hc", the terminal is on another machine, box, which hc exec reaches.
	Prepare string `json:"prepare,omitempty"`
	// Needs "sh": the request runs a shell script on the other machine, which Windows has no sh for.
	Needs  string          `json:"needs,omitempty"`
	Reads  []string        `json:"reads"`
	Answer *contractAnswer `json:"answer,omitempty"`
}

type contractAnswer struct {
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    json.RawMessage   `json:"body,omitempty"`
	Text    string            `json:"text,omitempty"`
}

// titledTurn is pagedTurn with the title Claude Code saved for its session, as chat/claude's reader reads it.
type titledTurn struct{ pagedTurn }

func (*titledTurn) Title(string) (string, error) { return "Fix the login redirect", nil }

// contractServer: a Companion whose terminal t1 is a Claude Code that is done, with a saved title and an earlier page, in ~/proj, which
// has subfolders (one a git repository, one hidden), notes and a picture; Pi's folder for its chat setup; a newer
// release out. It returns the home and Pi's setup file.
func contractServer(t *testing.T) (*fixture, string, string) {
	t.Helper()
	real := func(p string) string {
		r, err := filepath.EvalSymlinks(p)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	home := real(t.TempDir())
	proj := filepath.Join(home, "proj")
	for _, d := range []string{"src", filepath.Join("app", ".git"), ".cache", filepath.Join("..", ".pi", "agent")} {
		if err := os.MkdirAll(filepath.Join(proj, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	os.WriteFile(filepath.Join(proj, "NOTES.md"), []byte("# Notes\n\n- one\n"), 0o644)
	os.WriteFile(filepath.Join(proj, "shot.png"), pngBytes(t), 0o644)
	d := t.TempDir()
	get, _ := json.Marshal(map[string]any{"address": "t1", "cwd": home, "status": map[string]any{"app": "claude-code", "state": "done"},
		"foreground": map[string]any{"pid": 4242, "name": "claude", "cwd": proj}})
	os.WriteFile(filepath.Join(d, "get-t1.json"), get, 0o644)
	hc := testfake.HC(t, filepath.Join(d, "bin"), "hc", testfake.Spec{Version: "hc 0.1.20", Connect: "echo",
		On: map[string][]testfake.Reply{"get": {{Cat: filepath.Join(d, "get-{2}.json")}}}, Else: 1})
	setup := &pi.Setup{Dir: filepath.Join(home, ".pi", "agent"), HC: "/bin/hc"}
	f := newFixture(t, func(c *Config) {
		c.Name, c.Version, c.Home, c.HC, c.HCVersion = "studio", "0.3.11", home, hc, "hc 0.1.20"
		c.ImageTemp = []string{real(t.TempDir())}
		c.Updates = &fakeUpdates{latest: "0.3.12", can: true}
		c.PlainUntil = func() time.Time { return time.Date(2026, 12, 1, 12, 0, 0, 0, time.UTC) }
		c.Chat = &chat.Service{HC: hc, Readers: map[string]chat.Reader{"claude-code": &titledTurn{}}}
		c.ChatSetups = map[string]chat.Setup{"pi": setup}
	})
	return f, home, setup.File()
}

func TestTheLastReleasesAppStillGetsWhatItReads(t *testing.T) {
	raw, err := os.ReadFile(contractFile)
	if err != nil {
		t.Fatal(err)
	}
	var c contract
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	record := os.Getenv("HM_CONTRACT_RECORD") != ""
	f, home, setupFile := contractServer(t)
	box, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	code, err := f.store.CreateCode("Phone", 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	vars := map[string]string{"{code}": code}
	at := map[string]string{"{home}": home, "{box}": box}
	places := map[string]string{home: "/Users/sam", box: "/home/sam"}
	for i := range c.Calls {
		call := &c.Calls[i]
		if call.Needs == "sh" && runtime.GOOS == "windows" {
			t.Logf("%s: not on Windows, which has no sh for the other machine's script", call.Name)
			continue
		}
		hc := f.s.cfg.HC
		switch call.Prepare {
		case "edit-setup":
			os.MkdirAll(filepath.Dir(setupFile), 0o755)
			if err := os.WriteFile(setupFile, []byte("// mine\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		case "remote-hc":
			f.s.cfg.HC = fakeExecHC(t, box)
		}
		got := contractSend(t, f, call, vars)
		f.s.cfg.HC = hc
		switch {
		case record:
			call.Answer = contractScrub(got, places, vars)
		case call.Answer == nil:
			t.Errorf("%s: no answer recorded", call.Name)
		case got.Status != call.Answer.Status:
			t.Errorf("%s: status %d, the old app got %d (%s)", call.Name, got.Status, call.Answer.Status, got.Body)
		case call.Socket != "" && got.Text != call.Answer.Text:
			t.Errorf("%s: bytes back %q, the old app got %q", call.Name, got.Text, call.Answer.Text)
		}
		for _, spec := range call.Reads {
			if err := contractRead(got, contractFill(spec, at)); err != nil {
				t.Errorf("%s reads %s: %v (%s)", call.Name, spec, err, got.Body)
			}
		}
		var body struct {
			Token, Before, Confirmation string
		}
		json.Unmarshal(got.Body, &body)
		switch call.Name {
		case "pair":
			vars["{token}"] = body.Token
		case "chat":
			vars["{etag}"], vars["{before}"] = got.Headers["etag"], body.Before
		case "chat setup changed":
			vars["{confirmation}"] = body.Confirmation
		}
	}
	if record && !t.Failed() {
		var b bytes.Buffer
		e := json.NewEncoder(&b)
		e.SetEscapeHTML(false)
		e.SetIndent("", "  ")
		if err := e.Encode(c); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(contractFile, b.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func contractFill(s string, vars map[string]string) string {
	for k, v := range vars {
		s = strings.ReplaceAll(s, k, v)
	}
	return s
}

// contractSend makes the request as the old app does and gives back the answer it got.
func contractSend(t *testing.T, f *fixture, call *contractCall, vars map[string]string) *contractAnswer {
	t.Helper()
	q := url.Values{}
	for k, v := range call.Query {
		q.Set(k, contractFill(v, vars))
	}
	u := f.srv.URL + call.Path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	if call.Socket != "" {
		return contractSocket(t, call.Name, u, call.Socket, call.Send)
	}
	var body io.Reader
	if call.Body != nil {
		body = strings.NewReader(contractFill(string(call.Body), vars))
	} else if call.Text != "" {
		body = strings.NewReader(call.Text)
	}
	req, _ := http.NewRequest(call.Method, u, body)
	for k, v := range call.Headers {
		req.Header.Set(k, contractFill(v, vars))
	}
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s: %v", call.Name, err)
	}
	defer r.Body.Close()
	b, _ := io.ReadAll(r.Body)
	a := &contractAnswer{Status: r.StatusCode}
	if tag := r.Header.Get("ETag"); tag != "" {
		a.Headers = map[string]string{"etag": tag}
	}
	if json.Valid(b) && len(b) > 0 {
		a.Body = json.RawMessage(bytes.TrimSpace(b))
	} else {
		a.Text = string(b)
	}
	return a
}

// contractSocket opens /hc as hc/ws.ts does, sends the bytes, and reads them back through the fake hc's echo, in
// the messages the encoding takes (binary, or base64 text). Anything else fails the test, recording or not.
func contractSocket(t *testing.T, name, u, enc, send string) *contractAnswer {
	t.Helper()
	msg, err := base64.StdEncoding.DecodeString(send)
	if err != nil || len(msg) == 0 {
		t.Fatalf("%s: no bytes to send", name)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, r, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(u, "http"), nil)
	if err != nil {
		t.Fatalf("%s: %v (status %d)", name, err, status(r))
	}
	defer c.CloseNow()
	typ, out := websocket.MessageBinary, msg
	if enc == "b64" {
		typ, out = websocket.MessageText, []byte(send)
	}
	if err := c.Write(ctx, typ, out); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	got := readAll(t, c, len(msg), typ, 64*1024)
	if !bytes.Equal(got, msg) {
		t.Errorf("%s: sent %q, came back %q", name, msg, got)
	}
	return &contractAnswer{Status: r.StatusCode, Text: base64.StdEncoding.EncodeToString(got)}
}

var uploadTime = regexp.MustCompile(`/uploads/\d{8}-\d{6}-`)

// contractScrub is the answer as the file keeps it: the fixture's folders by the names in places, the token and the
// one-time file link's key as names, not secrets, and an upload folder's time as noon on the fixture's day, not when
// it was recorded.
func contractScrub(a *contractAnswer, places, vars map[string]string) *contractAnswer {
	swap := func(s string) string {
		for real, name := range places {
			esc, _ := json.Marshal(real)
			s = strings.ReplaceAll(s, strings.Trim(string(esc), `"`), name)
		}
		if tok := vars["{token}"]; tok != "" {
			s = strings.ReplaceAll(s, tok, "contract-token")
		}
		return uploadTime.ReplaceAllString(s, "/uploads/20261201-120000-")
	}
	out := &contractAnswer{Status: a.Status, Headers: a.Headers, Text: swap(a.Text)}
	if a.Body != nil {
		var v map[string]any
		if json.Unmarshal(a.Body, &v) == nil {
			if tok, ok := v["token"].(string); ok {
				vars["{token}"] = tok
				v["token"] = "contract-token"
			}
			if link, ok := v["url"].(string); ok && strings.HasPrefix(link, "/api/file?") {
				q, _ := url.ParseQuery(strings.TrimPrefix(link, "/api/file?"))
				q.Set("key", "contract-key")
				v["url"] = "/api/file?" + q.Encode()
			}
			var b bytes.Buffer
			e := json.NewEncoder(&b)
			e.SetEscapeHTML(false)
			e.Encode(v)
			out.Body = json.RawMessage(swap(strings.TrimSpace(b.String())))
		} else {
			out.Body = json.RawMessage(swap(string(a.Body)))
		}
	}
	return out
}

// contractRead checks one thing the old app reads, written path:what. A path goes through objects by name; x[] is
// every item of the list x, which has items; x[*] is some item; x[2] is that item. header.etag is the ETag header and
// text the whole answer as text. What: string, number, boolean, true, false, string^prefix, string=a|b (one of those,
// the values the old app tells apart) or time (RFC 3339, as the old app reads one).
func contractRead(a *contractAnswer, spec string) error {
	path, want, ok := strings.Cut(spec, ":")
	if !ok {
		return fmt.Errorf("no type")
	}
	switch {
	case strings.HasPrefix(path, "header."):
		v, ok := a.Headers[strings.TrimPrefix(path, "header.")]
		if !ok {
			return fmt.Errorf("no such header")
		}
		return contractIs(v, want)
	case path == "text":
		return contractIs(a.Text, want)
	}
	var v any
	if err := json.Unmarshal(a.Body, &v); err != nil {
		return fmt.Errorf("the answer is not JSON")
	}
	return contractCheck(v, path, want)
}

func contractCheck(v any, path, want string) error {
	if path == "" {
		return contractIs(v, want)
	}
	seg, rest, _ := strings.Cut(path, ".")
	name, idx, list := strings.Cut(seg, "[")
	m, ok := v.(map[string]any)
	if !ok {
		return fmt.Errorf("not an object at %s", name)
	}
	x, ok := m[name]
	if !ok {
		return fmt.Errorf("no %s", name)
	}
	if !list {
		return contractCheck(x, rest, want)
	}
	items, _ := x.([]any)
	if len(items) == 0 {
		return fmt.Errorf("%s is not a list with items", name)
	}
	switch idx = strings.TrimSuffix(idx, "]"); idx {
	case "":
		for i, item := range items {
			if err := contractCheck(item, rest, want); err != nil {
				return fmt.Errorf("%s[%d]: %w", name, i, err)
			}
		}
		return nil
	case "*":
		for _, item := range items {
			if contractCheck(item, rest, want) == nil {
				return nil
			}
		}
		return fmt.Errorf("no item of %s has it", name)
	default:
		n, err := strconv.Atoi(idx)
		if err != nil || n >= len(items) {
			return fmt.Errorf("%s has no item %s", name, idx)
		}
		return contractCheck(items[n], rest, want)
	}
}

func contractIs(v any, want string) error {
	kind, prefix, _ := strings.Cut(want, "^")
	kind, values, enum := strings.Cut(kind, "=")
	ok := false
	switch kind {
	case "string":
		s, is := v.(string)
		ok = is && strings.HasPrefix(s, prefix) && (!enum || slices.Contains(strings.Split(values, "|"), s))
	case "time":
		s, _ := v.(string)
		_, err := time.Parse(time.RFC3339, s)
		ok = err == nil
	case "number":
		_, ok = v.(float64)
	case "boolean":
		_, ok = v.(bool)
	case "true", "false":
		ok = v == (kind == "true")
	}
	if !ok {
		return fmt.Errorf("%#v is not %s", v, want)
	}
	return nil
}
