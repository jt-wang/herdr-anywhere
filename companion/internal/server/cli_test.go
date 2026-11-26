package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// cliFake: this test binary as the companion's own program, for /api/cli. It prints the arguments it got and its
// state folder, one JSON object, writes a line to stderr, and exits 3 for `progress off`.
func cliFake() {
	if os.Getenv("HM_CLI_FAKE") != "1" {
		return
	}
	b, _ := json.Marshal(map[string]any{"args": os.Args[1:], "state": os.Getenv("HERDR_ANYWHERE_STATE")})
	fmt.Println(string(b))
	fmt.Fprintln(os.Stderr, "note: π 读不到")
	if len(os.Args) > 2 && os.Args[1] == "progress" && os.Args[2] == "off" {
		os.Exit(3)
	}
	os.Exit(0)
}

func TestMain(m *testing.M) {
	cliFake()
	os.Exit(m.Run())
}

func cliReq(t *testing.T, f *fixture, token, body string) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest("POST", f.srv.URL+"/api/cli", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	b, _ := io.ReadAll(r.Body)
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("%d %s", r.StatusCode, b)
	}
	return r.StatusCode, out
}

// The progress page's commands run as the companion's own program with exactly the arguments the phone sent, no
// shell between, on Windows as elsewhere; anything else the phone sends is refused before anything runs.
func TestCLIRunsTheCompanionWithoutAShell(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HM_CLI_FAKE", "1")
	t.Setenv("HERDR_ANYWHERE_STATE", filepath.Join(t.TempDir(), "the default"))
	hc := filepath.Join(t.TempDir(), "core", "hc")
	f := newFixture(t, func(c *Config) { c.Self = self; c.HC = hc })
	_, token := f.pair(t)

	if code, _ := cliReq(t, f, "", `{"args":["progress","agents"]}`); code != 401 {
		t.Fatalf("unpaired: %d", code)
	}
	// Only the progress page's own commands, word for word: no other command, no extra flag (each of --hc, --state
	// and --cmd names a program, a folder or a command line for the computer to use), no value that is a flag or
	// carries shell syntax.
	for _, args := range []string{`[]`, `["progress"]`, `["serve"]`, `["update","--yes"]`, `["progress","rm"]`,
		`["track","add","x"]`, `["track","tree"]`, `["track","tree","--json","--herdr","/bin/sh"]`,
		`["progress","on","--hc","C:\\x\\evil.exe"]`, `["progress","agents","--state","/tmp/x"]`,
		`["progress","organizer","--cmd","calc.exe"]`, `["progress","organizer","--agent","pi","--hc","/bin/sh"]`,
		`["progress","organizer","--agent","pi & calc","--lang","en"]`, `["progress","organizer","--agent","--hc","--lang","en"]`,
		`["progress","organizer","--agent","pi","--lang","en","--hc","/bin/sh"]`, `["progress","lang","--hc"]`,
		`["progress","lang","en;calc"]`, `["progress","lang","en","--state","/tmp/x"]`} {
		if code, b := cliReq(t, f, token, `{"args":`+args+`}`); code != 400 {
			t.Fatalf("%s: %d %v", args, code, b)
		}
	}

	// The program works on the paired companion's state folder and herdr-core, not the defaults: the companion adds
	// --hc to each command that reaches herdr-core.
	for _, c := range []struct{ send, got []string }{
		{[]string{"progress", "on"}, []string{"progress", "on", "--hc", hc}},
		{[]string{"progress", "agents"}, []string{"progress", "agents"}},
		{[]string{"progress", "lang", "zh-Hans"}, []string{"progress", "lang", "zh-Hans"}},
		{[]string{"track", "tree", "--json", "--herdr", "-"}, []string{"track", "tree", "--json", "--herdr", "-", "--hc", hc}},
		{[]string{"progress", "organizer", "--agent", "claude-code", "--lang", "zh-Hans-CN"},
			[]string{"progress", "organizer", "--agent", "claude-code", "--lang", "zh-Hans-CN", "--hc", hc}},
	} {
		j, _ := json.Marshal(map[string]any{"args": c.send})
		code, b := cliReq(t, f, token, string(j))
		if code != 200 || b["code"] != float64(0) || b["stderr"] != "note: π 读不到\n" && b["stderr"] != "note: π 读不到\r\n" {
			t.Fatalf("%q: %d %v", c.send, code, b)
		}
		var got struct {
			Args  []string
			State string
		}
		if err := json.Unmarshal([]byte(strings.TrimSpace(b["stdout"].(string))), &got); err != nil ||
			fmt.Sprint(got.Args) != fmt.Sprint(c.got) || got.State != f.store.Dir() {
			t.Fatalf("the program got %q in %q, want %q in %q", got.Args, got.State, c.got, f.store.Dir())
		}
	}
	if code, b := cliReq(t, f, token, `{"args":["progress","off"]}`); code != 200 || b["code"] != float64(3) {
		t.Fatalf("off: %d %v", code, b)
	}
}

// A companion that doesn't know its own program fails the call. A 404 would tell the phone it is older than /api/cli,
// and the phone would run the command the old way: through a shell, on its default state folder.
func TestCLIWithoutSelf(t *testing.T) {
	f := newFixture(t, nil)
	_, token := f.pair(t)
	if code, _ := cliReq(t, f, token, `{"args":["progress","agents"]}`); code != 500 {
		t.Fatalf("got %d", code)
	}
}
