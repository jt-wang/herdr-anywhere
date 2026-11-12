package opencode

// The live check of a message sent while OpenCode works: its own screen (livetui), as a pane runs it, against the
// fake Responses API. Sent with Enter while a tool runs, the message is written at once (a user message, QUEUED on
// the screen) and taken before the model's next step, in the same turn. Opt in with the binary to run:
//
//	OPENCODE_LIVE=/path/to/opencode go test ./internal/chat/opencode -run LiveQueued            # check that version
//	OPENCODE_LIVE=/path/to/opencode go test ./internal/chat/opencode -run LiveQueued -update    # write testdata/opencode/<version>/queued.json

import (
	"database/sql"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"herdr-anywhere/companion/internal/chat"
	"herdr-anywhere/companion/internal/chat/livetui"
)

func TestLiveQueued(t *testing.T) {
	bin := os.Getenv("OPENCODE_LIVE")
	if bin == "" {
		t.Skip("OPENCODE_LIVE=/path/to/opencode runs that OpenCode")
	}
	r := liveRun(t, bin)
	said := strings.Fields(r.oc(t, "--version"))
	v := ""
	if len(said) > 0 {
		v = strings.TrimPrefix(said[len(said)-1], "v")
	}
	if !(strings.HasPrefix(v, "1.") || strings.HasPrefix(v, "2.")) || strings.Contains(v, "/") {
		t.Fatalf("opencode --version said %q; this reader is for OpenCode 1 and 2", said)
	}
	py, _ := exec.LookPath("python3")
	env := append([]string{}, r.env...)
	for i, e := range env {
		if strings.HasPrefix(e, "PATH=") {
			env[i] = e + ":" + filepath.Dir(py)
		}
	}
	u := livetui.Start(t, env, r.cwd, bin)
	// Nothing is typed until the screen shows the fake model and no question.
	s := u.Wait(30*time.Second, "the fake model", livetui.Has("GPT-5.5"))
	low := strings.ToLower(s)
	for _, w := range []string{"update", "upgrade", "log in", "login", "sign in"} {
		if strings.Contains(low, w) {
			t.Fatalf("OpenCode's screen asks something (%q); nothing typed:\n%s", w, s)
		}
	}
	u.Settle()
	u.Send("WORK through the three steps")
	u.Until(60*time.Second, "running its first step", func() bool { return r.recorded(t, "call_fake_w1") })
	u.Send("ALSO keep the old colors")
	// 1.19.0 marks it QUEUED until it is taken; 2.2.0 shows it as it is (seen once each).
	u.Wait(10*time.Second, "the message", livetui.Has("ALSO keep the old colors"))
	u.Until(90*time.Second, "done with the turn", func() bool { return r.idle(t) >= 1 && r.recorded(t, "All three steps are done.") })
	clean := r.dump(t)
	r.stopService()
	dir := filepath.Join("testdata", "opencode", v)
	if *update {
		os.MkdirAll(dir, 0o755)
		if err := os.WriteFile(filepath.Join(dir, "queued.json"), clean, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %s/queued.json; write or check its .want.json by hand", dir)
		return
	}
	want := filepath.Join(dir, "queued.want.json")
	b, err := os.ReadFile(want)
	if err != nil {
		t.Fatalf("no %s: look at the session, then run with -update", want)
	}
	var w []chat.Turn
	if err := json.Unmarshal(b, &w); err != nil {
		t.Fatalf("%s: %v", want, err)
	}
	if got := fromFixture(t, clean); !reflect.DeepEqual(norm(got), norm(w)) {
		g, _ := json.MarshalIndent(norm(got), "", "  ")
		t.Fatalf("turns differ from %s; got\n%s", want, g)
	}
}

// recorded: whether the run's database holds text anywhere in a session's rows (OpenCode 2) or parts (OpenCode 1).
func (r ocRun) recorded(t *testing.T, text string) bool {
	db := filepath.Join(r.home, ".local", "share", "opencode", "opencode.db")
	if _, err := os.Stat(db); err != nil {
		return false
	}
	d, err := sql.Open("sqlite", "file:"+db+"?mode=ro&_pragma=busy_timeout(2000)")
	if err != nil {
		return false
	}
	defer d.Close()
	q := "SELECT count(*) FROM part WHERE instr(data, ?) > 0"
	if table(d, "session_v2") {
		q = "SELECT count(*) FROM session_message WHERE instr(data, ?) > 0"
	}
	var n int
	d.QueryRow(q, text).Scan(&n)
	return n > 0
}
