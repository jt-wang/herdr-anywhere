package cursor

import (
	"bufio"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"herdr-anywhere/companion/internal/chat"
	"herdr-anywhere/companion/internal/testfake"
)

func TestProcessValidation(t *testing.T) {
	tree := map[int]process{10: {2, []string{"bash"}}, 11: {10, []string{"/installed/cursor-agent", "--disable-sigusr1", "/installed/index.js"}}, 12: {11, []string{"bash"}}, 13: {12, []string{"hook"}}, 20: {2, []string{"agent", "index.js"}}, 21: {2, []string{"agent", "unrelated.js"}}, 22: {22, []string{"cursor-agent", "index.js"}}}
	get := func(pid int) (process, bool) { p, ok := tree[pid]; return p, ok }
	if ancestor(12, get) != 11 {
		t.Fatal("did not walk through hook shell")
	}
	if ancestor(21, get) != 0 || ancestor(999, get) != 0 {
		t.Fatal("selected non-Cursor/dead PID")
	}
	for _, c := range []struct {
		pid, terminal int
		want          bool
	}{{11, 10, true}, {11, 11, true}, {20, 10, false}, {21, 2, false}, {999, 10, false}, {13, 10, false}, {22, 10, false}, {11, 0, false}} {
		if got := validProcess(c.pid, c.terminal, get); got != c.want {
			t.Errorf("pid=%d terminal=%d got=%v", c.pid, c.terminal, got)
		}
	}
	for _, argv := range [][]string{{"agent"}, {"node", "index.js"}, {"not-cursor-agent", "index.js"}, {"agent", "not-index.js"}} {
		if cursorCommand(argv) {
			t.Fatal("accepted other program", argv)
		}
	}
}

func helper(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fakecursor"+testfake.Ext())
	if out, err := exec.Command("go", "build", "-o", path, "herdr-anywhere/companion/internal/testfake/fakecursor").CombinedOutput(); err != nil {
		t.Fatalf("fake cursor: %s %v", out, err)
	}
	return path
}

func TestHookWalksRealGoProcessTree(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("proc-based process tree fixture")
	}
	exe := helper(t)
	dir := t.TempDir()
	hc := filepath.Join(dir, "hc")
	data, e := os.ReadFile(exe)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(hc, data, 0700); e != nil {
		t.Fatal(e)
	}
	for _, scenario := range []string{"valid", "malformed", "timeout"} {
		capture := filepath.Join(dir, "args-"+scenario+".json")
		c := exec.Command(exe, "index.js")
		c.Args[0] = "cursor-agent"
		c.Env = append(os.Environ(), "HERDR_CURSOR_TEST_HELPER=agent", "HERDR_CURSOR_TEST_HC="+hc, "HERDR_CURSOR_TEST_CAPTURE="+capture, "HC_TERMINAL_ID=t9")
		payload := fmt.Sprintf(`{"conversation_id":%q,"workspace_roots":[%q]}`, fixtureID, dir)
		if scenario == "timeout" {
			c.Env = append(c.Env, "HERDR_CURSOR_TEST_HANG=1")
		}
		if scenario == "malformed" {
			payload = `{"conversation_id":"../wrong","workspace_roots":[]}`
		}
		c.Stdin = strings.NewReader(payload)
		start := time.Now()
		out, e := c.CombinedOutput()
		if time.Since(start) > 2500*time.Millisecond {
			t.Fatal("hook blocked prompt")
		}
		if e != nil || len(out) != 0 {
			t.Fatalf("hook output %q %v", out, e)
		}
		b, e := os.ReadFile(capture)
		if scenario != "valid" {
			if !os.IsNotExist(e) {
				t.Fatal("invalid payload labeled pane")
			}
			continue
		}
		if e != nil {
			t.Fatal(e)
		}
		var args []string
		_ = json.Unmarshal(b, &args)
		if len(args) != 5 || args[0] != "label" || args[1] != "t9" || args[2] != chat.LabelPid+"="+strconv.Itoa(c.ProcessState.Pid()) || args[3] != chat.LabelSession+"="+fixtureID || args[4] != LabelCwd+"="+dir {
			t.Fatalf("wrong labels: %v", args)
		}
	}
}

func TestUnreportedRejectsForeignLabel(t *testing.T) {
	r := &Reader{}
	for _, pid := range []string{"0", "-1", "99999999", strconv.Itoa(os.Getpid()), "not-a-pid"} {
		if _, ok := r.UnreportedPane(os.Getpid(), map[string]string{chat.LabelPid: pid, chat.LabelSession: fixtureID, LabelCwd: t.TempDir()}); ok {
			t.Fatal("accepted invalid label", pid)
		}
	}
	Hook(strings.NewReader(`not json`), "relative-hc")
}

func TestServiceWithoutForeground(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("proc-based process tree fixture")
	}
	exe := helper(t)
	c := exec.Command(exe, "index.js")
	c.Args[0] = "cursor-agent"
	c.Env = append(os.Environ(), "HERDR_CURSOR_TEST_HELPER=hold")
	in, _ := c.StdinPipe()
	out, _ := c.StdoutPipe()
	if e := c.Start(); e != nil {
		t.Fatal(e)
	}
	defer func() { in.Close(); c.Wait() }()
	if line, e := bufio.NewReader(out).ReadString('\n'); e != nil || line != "ready\n" {
		t.Fatal("not ready", line, e)
	}
	r, _, db := fixture(t, "store.db")
	labels := map[string]string{chat.LabelPid: strconv.Itoa(c.Process.Pid), chat.LabelSession: fixtureID, LabelCwd: filepath.Join(string(filepath.Separator), "home", "cedar", "paper-garden")}
	get := map[string]any{"pid": os.Getpid(), "labels": labels, "status": nil}
	for _, scenario := range []string{"valid", "dead", "unrelated", "wrong-program", "foreground", "reported-other", "reported-cursor-without-foreground"} {
		labels[chat.LabelPid] = strconv.Itoa(c.Process.Pid)
		get["pid"] = os.Getpid()
		delete(get, "foreground")
		get["status"] = nil
		switch scenario {
		case "dead":
			labels[chat.LabelPid] = "99999999"
		case "unrelated":
			get["pid"] = 99999998
		case "wrong-program":
			labels[chat.LabelPid] = strconv.Itoa(os.Getpid())
		case "foreground":
			get["foreground"] = map[string]any{"pid": os.Getpid(), "name": "bash"}
		case "reported-other":
			get["status"] = map[string]string{"app": "pi", "state": "idle"}
		case "reported-cursor-without-foreground":
			get["status"] = map[string]string{"app": "cursor", "state": "idle"}
		}
		b, _ := json.Marshal(get)
		hc := testfake.HC(t, t.TempDir(), "hc", testfake.Spec{On: map[string][]testfake.Reply{"get": {{Print: string(b)}}}})
		s := chat.Service{HC: hc, Readers: map[string]chat.Reader{"cursor": r}, Log: log.New(io.Discard, "", 0)}
		a := s.Chat("t9")
		if scenario == "valid" {
			if !a.Chat || a.Program != "cursor" || a.Unknown != "" || len(a.Turns) != 6 {
				t.Fatalf("valid labels: %+v", a)
			}
		} else if scenario == "dead" || scenario == "wrong-program" {
			if !a.Chat || a.Unknown != chat.SetupNotLoaded || len(a.Turns) != 0 {
				t.Fatalf("stale labels leaked a conversation: %+v", a)
			}
		} else if a.Chat || a.Program != "" {
			t.Fatalf("accepted %s: %+v", scenario, a)
		}
	}
	delete(labels, chat.LabelPid)
	delete(get, "foreground")
	get["status"] = nil
	r.Setup = &Setup{Dir: t.TempDir(), Binary: exe, HC: exe}
	b, _ := json.Marshal(get)
	hc := testfake.HC(t, t.TempDir(), "hc", testfake.Spec{On: map[string][]testfake.Reply{"get": {{Print: string(b)}}}})
	s := chat.Service{HC: hc, Readers: map[string]chat.Reader{"cursor": r}, Log: log.New(io.Discard, "", 0)}
	if a := s.Chat("t9"); !a.Chat || a.Program != "cursor" || a.Unknown != chat.SetupNeeded || a.Setup != "cursor" || len(a.Turns) != 0 {
		t.Fatalf("unlabeled shell child before setup: %+v", a)
	}
	if err := r.Setup.Install(false); err != nil {
		t.Fatal(err)
	}
	if a := s.Chat("t9"); !a.Chat || a.Program != "cursor" || a.Unknown != chat.SetupNotLoaded || a.Setup != "cursor" || len(a.Turns) != 0 {
		t.Fatalf("unlabeled shell child after setup: %+v", a)
	}
	info, err := os.Stat(r.Setup.File())
	if err != nil {
		t.Fatal(err)
	}
	r.started = func(pid int) time.Time {
		if pid != c.Process.Pid {
			t.Fatal("used terminal PID instead of Cursor PID", pid)
		}
		return info.ModTime().Add(time.Second)
	}
	if a := s.Chat("t9"); !a.Chat || a.Program != "cursor" || a.Unknown != chat.AwaitingPrompt || a.Setup != "" || len(a.Turns) != 0 {
		t.Fatalf("Cursor started after setup must await a prompt, not restart: %+v", a)
	}
	labels[chat.LabelPid] = strconv.Itoa(c.Process.Pid)
	b, _ = json.Marshal(get)
	s.HC = testfake.HC(t, t.TempDir(), "hc", testfake.Spec{On: map[string][]testfake.Reply{"get": {{Print: string(b)}}}})
	a := s.Chat("t9")
	if len(a.Turns) != 6 {
		t.Fatal("installed Cursor lost its record", a)
	}
	var raw string
	if err := db.QueryRow(`SELECT value FROM meta WHERE key='0'`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	decoded, err := hex.DecodeString(raw)
	if err != nil {
		t.Fatal(err)
	}
	var meta map[string]any
	if err := json.Unmarshal(decoded, &meta); err != nil {
		t.Fatal(err)
	}
	meta["latestRootBlobId"] = ""
	pending, err := json.Marshal(meta)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE meta SET value=? WHERE key='0'`, hex.EncodeToString(pending)); err != nil {
		t.Fatal(err)
	}
	if a := s.Chat("t9"); !a.Chat || a.Program != "cursor" || a.Unknown != chat.NoRecordYet || len(a.Turns) != 0 {
		t.Fatalf("pending checkpoint must not claim ambiguous identity: %+v", a)
	}
	if _, err := db.Exec(`UPDATE meta SET value=? WHERE key='0'`, raw); err != nil {
		t.Fatal(err)
	}
	if a := s.Chat("t9"); a.Unknown != "" || len(a.Turns) != 6 {
		t.Fatal("completed checkpoint did not recover", a)
	}
	step := a.Turns[1].Steps[0].ID
	image := a.Turns[4].Images[0].ID
	if err := r.Setup.Remove(false); err != nil {
		t.Fatal(err)
	}
	if a := s.Chat("t9"); a.Unknown != chat.SetupNeeded || len(a.Turns) != 0 {
		t.Fatalf("removed setup exposed turns: %+v", a)
	}
	if _, err := s.Step("t9", step); !errors.Is(err, chat.ErrNoStep) {
		t.Fatal("removed setup exposed detail", err)
	}
	if _, _, err := s.Image("t9", image); !errors.Is(err, chat.ErrNoStep) {
		t.Fatal("removed setup exposed image", err)
	}
}

func TestStaleLabelsRediscoverWithoutMapping(t *testing.T) {
	old := supported
	supported = func() bool { return true }
	t.Cleanup(func() { supported = old })
	tree := map[int]process{10: {2, []string{"bash"}}, 11: {10, []string{"cursor-agent", "index.js"}}, 20: {2, []string{"cursor-agent", "index.js"}}}
	get := func(pid int) (process, bool) { p, ok := tree[pid]; return p, ok }
	for _, stale := range []string{"999", "20", "10", "invalid"} {
		labels := map[string]string{chat.LabelPid: stale, chat.LabelSession: fixtureID, LabelCwd: "/synthetic/old-garden"}
		p, ok := unreportedPane(10, labels, get, func() map[int]process { return tree })
		if !ok || p.Pid != 11 {
			t.Fatal("did not rediscover replacement", p, ok)
		}
		if len(p.Labels) != 0 || p.Cwd != "" {
			t.Fatal("reused stale mapping", p)
		}
		if labels[chat.LabelPid] != stale {
			t.Fatal("mutated original labels")
		}
	}
	tree[12] = process{10, []string{"agent", "index.js"}}
	if _, ok := unreportedPane(10, map[string]string{chat.LabelPid: "999"}, get, func() map[int]process { return tree }); ok {
		t.Fatal("guessed between replacements")
	}
}

func TestUnlabeledCursorDoesNotGuess(t *testing.T) {
	tree := map[int]process{10: {1, []string{"bash"}}, 11: {10, []string{"cursor-agent", "index.js"}}, 20: {1, []string{"agent", "index.js"}}}
	if got := unlabeledCursor(10, tree); got != 11 {
		t.Fatal("missed shell child", got)
	}
	tree[12] = process{10, []string{"agent", "index.js"}}
	if got := unlabeledCursor(10, tree); got != 0 {
		t.Fatal("guessed between two Cursors", got)
	}
	delete(tree, 11)
	delete(tree, 12)
	if got := unlabeledCursor(10, tree); got != 0 {
		t.Fatal("selected unrelated Cursor", got)
	}
	tree[11] = process{10, []string{"node", "index.js"}}
	if got := unlabeledCursor(10, tree); got != 0 {
		t.Fatal("selected non-Cursor child", got)
	}
}
