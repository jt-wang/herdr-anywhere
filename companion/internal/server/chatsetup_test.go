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
	if one["agent"] != "pi" || one["name"] != "Pi" || one["state"] != "absent" || one["file"] != file || one["reload"] != "/reload" || one["version"] != float64(2) {
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
	_, info := setupReq(t, f, token, "GET", "/api/chat-setups")
	confirmation := info["setups"].([]any)[0].(map[string]any)["confirmation"].(string)
	if code, b := setupReq(t, f, token, "PUT", "/api/chat-setups/pi?force=1&confirmation="+confirmation); code != 200 || b["state"] != "installed" {
		t.Fatalf("forced: %d %v", code, b)
	}
}

func TestOMPChangedProfilesRequireExactConfirmation(t *testing.T) {
	for _, method := range []string{"PUT", "DELETE"} {
		t.Run(method, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home)
			for _, key := range []string{"PI_CONFIG_DIR", "PI_CODING_AGENT_DIR", "OMP_PROFILE", "PI_PROFILE", "XDG_DATA_HOME"} {
				t.Setenv(key, "")
			}
			os.MkdirAll(filepath.Join(home, ".omp", "agent"), 0o755)
			for _, name := range []string{"blue", "green"} {
				os.MkdirAll(filepath.Join(home, ".omp", "profiles", name, "agent"), 0o755)
			}
			s := pi.OMPSetup("/sample/hc")
			if err := s.Install(false); err != nil {
				t.Fatal(err)
			}
			var files []string
			for _, name := range []string{"blue", "green"} {
				file := filepath.Join(home, ".omp", "profiles", name, "agent", "extensions", "herdr-anywhere-omp.ts")
				if err := os.WriteFile(file, []byte("// edited\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				files = append(files, file)
			}
			f := newFixture(t, func(c *Config) { c.ChatSetups = map[string]chat.Setup{"omp": s} })
			_, token := f.pair(t)
			path := "/api/chat-setups/omp"
			code, info := setupReq(t, f, token, method, path)
			if code != 409 || len(info["changedFiles"].([]any)) != 2 {
				t.Fatalf("prompt: %d %v", code, info)
			}
			for i, name := range []string{"blue", "green"} {
				want := filepath.Join("~", ".omp", "profiles", name, "agent", "extensions", "herdr-anywhere-omp.ts")
				if info["changedFiles"].([]any)[i] != want {
					t.Fatalf("missing disclosed path: %v", info)
				}
			}
			old := info["confirmation"].(string)
			if code, _ := setupReq(t, f, token, method, path+"?force=1"); code != http.StatusUpgradeRequired {
				t.Fatal("unscoped force accepted")
			}
			if err := os.WriteFile(files[1], []byte("// new edits\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if code, _ := setupReq(t, f, token, method, path+"?force=1&confirmation="+old); code != 409 {
				t.Fatal("stale contents accepted")
			}
			third := filepath.Join(home, ".omp", "profiles", "red", "agent", "extensions", "herdr-anywhere-omp.ts")
			os.MkdirAll(filepath.Dir(third), 0o755)
			os.WriteFile(third, []byte("// red edits\n"), 0o600)
			code, info = setupReq(t, f, token, method, path+"?force=1&confirmation="+old)
			if code != 409 || len(info["changedFiles"].([]any)) != 3 {
				t.Fatalf("new conflict not reprompted: %d %v", code, info)
			}
			for _, file := range append(files, third) {
				if b, err := os.ReadFile(file); err != nil || len(b) > 30 {
					t.Fatalf("edited file changed: %s %v", file, err)
				}
			}
			code, info = setupReq(t, f, token, method, path+"?force=1&confirmation="+info["confirmation"].(string))
			want := "installed"
			if method == "DELETE" {
				want = "absent"
			}
			if code != 200 || info["state"] != want {
				t.Fatalf("confirmed: %d %v", code, info)
			}
		})
	}
}

func TestChatSetupTokenlessForceRequiresUpgrade(t *testing.T) {
	for _, method := range []string{"PUT", "DELETE"} {
		t.Run(method, func(t *testing.T) {
			f, token, file := setupFixture(t, true)
			if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
				t.Fatal(err)
			}
			const edited = "// edited\n"
			if err := os.WriteFile(file, []byte(edited), 0o600); err != nil {
				t.Fatal(err)
			}
			path := "/api/chat-setups/pi"
			code, info := setupReq(t, f, token, method, path)
			if code != http.StatusConflict || info["confirmation"] == "" {
				t.Fatalf("prompt: %d %v", code, info)
			}
			code, failure := setupReq(t, f, token, method, path+"?force=1")
			if code != http.StatusUpgradeRequired || failure["error"] != "upgrade-required" || failure["message"] == "" {
				t.Fatalf("legacy force: %d %v", code, failure)
			}
			if b, err := os.ReadFile(file); err != nil || string(b) != edited {
				t.Fatalf("touched edited file: %q %v", b, err)
			}
			code, info = setupReq(t, f, token, method, path+"?force=1&confirmation="+info["confirmation"].(string))
			want := "installed"
			if method == "DELETE" {
				want = "absent"
			}
			if code != http.StatusOK || info["state"] != want {
				t.Fatalf("token confirmation: %d %v", code, info)
			}
		})
	}
}

func TestChatSetupClearedEditsUseNormalOperation(t *testing.T) {
	for _, method := range []string{"PUT", "DELETE"} {
		for _, cleared := range []string{"restored", "deleted"} {
			t.Run(method+"/"+cleared, func(t *testing.T) {
				f, token, file := setupFixture(t, true)
				path := "/api/chat-setups/pi"
				if code, info := setupReq(t, f, token, "PUT", path); code != 200 {
					t.Fatalf("initial install: %d %v", code, info)
				}
				original, err := os.ReadFile(file)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(file, []byte("// edited\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				code, info := setupReq(t, f, token, method, path)
				if code != 409 {
					t.Fatalf("prompt: %d %v", code, info)
				}
				confirmation := info["confirmation"].(string)
				if cleared == "restored" {
					err = os.WriteFile(file, original, 0o600)
				} else {
					err = os.Remove(file)
				}
				if err != nil {
					t.Fatal(err)
				}
				code, info = setupReq(t, f, token, method, path+"?force=1&confirmation="+confirmation)
				want := "installed"
				if method == "DELETE" {
					want = "absent"
				}
				if code != 200 || info["state"] != want {
					t.Fatalf("cleared confirmation: %d %v", code, info)
				}
			})
		}
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
