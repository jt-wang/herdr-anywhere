package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"herdr-anywhere/companion/internal/chat"
	"herdr-anywhere/companion/internal/chat/pi"
)

func setupReq(t *testing.T, f *fixture, token, method, path string) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest(method, f.srv.URL+path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	var out map[string]any
	json.NewDecoder(r.Body).Decode(&out)
	return r.StatusCode, out
}

func setupFixture(t *testing.T, withPi bool) (*fixture, string, string) {
	dir := filepath.Join(t.TempDir(), ".pi", "agent")
	if withPi {
		os.MkdirAll(dir, 0o755)
	}
	s := &pi.Setup{Dir: dir, HC: "/bin/hc"}
	f := newFixture(t, func(c *Config) { c.ChatSetups = map[string]chat.Setup{"pi": s} })
	_, token := f.pair(t)
	return f, token, s.File()
}

// The phone lists every agent's setup, and installs or removes one with one request.
func TestChatSetupInstallAndRemove(t *testing.T) {
	f, token, file := setupFixture(t, true)
	for _, m := range []string{"GET /api/chat-setups", "PUT /api/chat-setups/pi", "DELETE /api/chat-setups/pi"} {
		method, path, _ := cut(m)
		if code, _ := setupReq(t, f, "", method, path); code != 401 {
			t.Fatalf("unpaired %s: %d", m, code)
		}
	}
	code, b := setupReq(t, f, token, "GET", "/api/chat-setups")
	list, _ := b["setups"].([]any)
	if code != 200 || len(list) != 1 {
		t.Fatalf("list: %d %v", code, b)
	}
	one := list[0].(map[string]any)
	if one["agent"] != "pi" || one["name"] != "Pi" || one["state"] != "absent" || one["file"] != file || one["reload"] != "/reload" || one["version"] != float64(1) {
		t.Fatalf("pi: %v", one)
	}
	if code, b := setupReq(t, f, token, "PUT", "/api/chat-setups/pi"); code != 200 || b["state"] != "installed" {
		t.Fatalf("install: %d %v", code, b)
	}
	if _, err := os.Stat(file); err != nil {
		t.Fatal(err)
	}
	if code, b := setupReq(t, f, token, "DELETE", "/api/chat-setups/pi"); code != 200 || b["state"] != "absent" {
		t.Fatalf("remove: %d %v", code, b)
	}
	if code, _ := setupReq(t, f, token, "PUT", "/api/chat-setups/nope"); code != 404 {
		t.Fatalf("unknown agent: %d", code)
	}
}

// A changed file: 409 and the file left; with force=1 it is replaced or removed.
func TestChatSetupChangedFileAsksFirst(t *testing.T) {
	f, token, file := setupFixture(t, true)
	os.MkdirAll(filepath.Dir(file), 0o755)
	os.WriteFile(file, []byte("// mine\n"), 0o644)
	if code, b := setupReq(t, f, token, "PUT", "/api/chat-setups/pi"); code != 409 || b["state"] != "changed" {
		t.Fatalf("install over it: %d %v", code, b)
	}
	if code, b := setupReq(t, f, token, "DELETE", "/api/chat-setups/pi"); code != 409 || b["state"] != "changed" {
		t.Fatalf("remove it: %d %v", code, b)
	}
	if b, _ := os.ReadFile(file); string(b) != "// mine\n" {
		t.Fatalf("touched: %q", b)
	}
	if code, b := setupReq(t, f, token, "PUT", "/api/chat-setups/pi?force=1"); code != 200 || b["state"] != "installed" {
		t.Fatalf("forced: %d %v", code, b)
	}
}

// Without the agent on the computer: the phone hears so, and nothing is made.
func TestChatSetupWithoutTheAgent(t *testing.T) {
	f, token, file := setupFixture(t, false)
	if code, b := setupReq(t, f, token, "PUT", "/api/chat-setups/pi"); code != 409 || b["state"] != "no-agent" {
		t.Fatalf("install: %d %v", code, b)
	}
	if _, err := os.Stat(filepath.Dir(filepath.Dir(file))); !os.IsNotExist(err) {
		t.Fatalf("made the agent's folder")
	}
}

func cut(s string) (string, string, bool) {
	for i := range s {
		if s[i] == ' ' {
			return s[:i], s[i+1:], true
		}
	}
	return s, "", false
}
