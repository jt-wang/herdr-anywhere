package main

import (
	"encoding/json"
	"herdr-anywhere/companion/internal/testfake"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"herdr-anywhere/companion/internal/track"
	"herdr-anywhere/companion/internal/watch"
)

func trackOK(t *testing.T, common []string) func(args ...string) string {
	return func(args ...string) string {
		t.Helper()
		out, e, c := runTrack(t, append(args[:1:1], append(common, args[1:]...)...)...)
		if c != 0 {
			t.Fatalf("%v: %d %s", args, c, e)
		}
		return out
	}
}

func TestTrackNestedTasksMoveAndTree(t *testing.T) {
	file := filepath.Join(t.TempDir(), "track.json")
	hc, herdr := fakeLists(t,
		`{"terminals":[{"address":"t2","name":"api","state":"live","status":"blocked","turn":3,"cwd":"/tmp/p/acme-api","agent":"claude-code"}]}`,
		`{"result":{"agents":[]}}`)
	common := []string{"--file", file, "--hc", hc, "--herdr", herdr}
	ok := trackOK(t, common)
	ok("add", "--std", "release 页能打开", "P1", "Acme 1.0 发布")
	ok("add", "P2", "文档改版")
	ok("add", "--under", "P1", "M1", "发版")
	ok("add", "--under", "M1", "T1", "打 tag 并 push")
	ok("add", "--under", "P2", "--auto", "T2", "签名构建")
	handedTo(t, common, "T1", "hc:api", "tag and push")
	ok("move", "T2", "M1")
	if _, _, c := runTrack(t, append([]string{"add"}, append(common, "--under", "P9", "T3", "x")...)...); c == 0 {
		t.Fatal("add under an unknown task succeeded")
	}
	var tr struct {
		NeedsYou []struct {
			ID   string   `json:"id"`
			Path []string `json:"path"`
		} `json:"needs_you"`
		Tasks []struct {
			ID       string                  `json:"id"`
			Group    string                  `json:"group"`
			Std      string                  `json:"std"`
			Notes    []struct{ Text string } `json:"notes"`
			Children []struct {
				ID       string `json:"id"`
				Children []struct {
					ID   string `json:"id"`
					Auto bool   `json:"auto"`
				} `json:"children"`
			} `json:"children"`
		} `json:"tasks"`
	}
	js := ok("tree", "--json")
	if err := json.Unmarshal([]byte(js), &tr); err != nil {
		t.Fatalf("%v\n%s", err, js)
	}
	if len(tr.NeedsYou) != 1 || tr.NeedsYou[0].ID != "T1" || strings.Join(tr.NeedsYou[0].Path, " › ") != "Acme 1.0 发布 › 发版" {
		t.Fatalf("needs you: %s", js)
	}
	if len(tr.Tasks) != 2 || tr.Tasks[0].ID != "P1" || tr.Tasks[0].Group != "waiting" || tr.Tasks[0].Std != "release 页能打开" {
		t.Fatalf("tasks: %s", js)
	}
	if m := tr.Tasks[0].Children; len(m) != 1 || len(m[0].Children) != 2 || m[0].Children[1].ID != "T2" || m[0].Children[1].Auto {
		t.Fatalf("P1 › M1: %s", js)
	}
	if n := tr.Tasks[1].Notes; len(n) != 1 || !strings.Contains(n[0].Text, "签名构建") {
		t.Fatalf("scope note on P2: %s", js)
	}
	out := ok("tree")
	for _, want := range []string{"Needs you", "Acme 1.0 发布 › 发版", "T1 打 tag 并 push", "文档改版", "t2 api blocked"} {
		if !strings.Contains(out, want) {
			t.Fatalf("tree text lacks %q:\n%s", want, out)
		}
	}
	ok("set", "--stage", "done", "P2")
	if js := ok("tree", "--json"); !strings.Contains(js, `"closed": [`) || !strings.Contains(js, `"id": "P2"`) {
		t.Fatalf("set stage: %s", js)
	}
	ok("move", "T2", "-")
}

// The companion files by rule only; the rest waits for the filing pane, which
// answers with place; what it isn't sure of waits for the user.
func TestTrackFileQueueAndPlace(t *testing.T) {
	file := filepath.Join(t.TempDir(), "track.json")
	hc, herdr := fakeLists(t,
		`{"terminals":[{"address":"t2","name":"api","state":"live","status":"idle","turn":3,"cwd":"/tmp/p/acme-api","agent":"claude-code"}]}`,
		`{"result":{"agents":[]}}`)
	ok := trackOK(t, []string{"--file", file, "--hc", hc, "--herdr", herdr})
	ok("add", "P1", "Acme 1.0 发布")

	if out := ok("file", "--pane", "hc:api", "发版前把 README 的安装命令更新成 1.0"); !strings.Contains(out, "Q1 queued for filing") {
		t.Fatalf("file: %s", out)
	}
	if out := ok("tree"); !strings.Contains(out, "1 still to file") {
		t.Fatalf("tree text lacks the queue:\n%s", out)
	}
	var v track.QueueView
	if err := json.Unmarshal([]byte(ok("queue", "--json")), &v); err != nil || len(v.Items) != 1 || len(v.Tasks) != 1 {
		t.Fatalf("queue: %v %+v", err, v)
	}
	tid := strings.TrimSpace(ok("place", "Q1", "--task", "new", "--under", "P1", "--title", "README 安装命令", "--sure"))
	// The pane now has a task, so 「好」 goes there by rule.
	if out := ok("file", "--json", "--pane", "hc:api", "好"); !strings.Contains(out, `"id": "`+tid+`"`) || !strings.Contains(out, `"by": "rule"`) {
		t.Fatalf("好: %s", out)
	}
	// Not sure is filed where it guessed: version one asks the user nothing.
	ok("file", "--pane", "hc:api", "顺便给公司账号写一条招聘帖")
	if out := ok("place", "Q1", "--task", "new", "--title", "写招聘帖", "--why", "像是一件新的事"); !strings.HasPrefix(out, "T") {
		t.Fatalf("place: %s", out)
	}
	if out := ok("tree"); !strings.Contains(out, "写招聘帖 (filed by rule)") || strings.Contains(out, "待确认") {
		t.Fatalf("tree:\n%s", out)
	}
	if js := ok("tree", "--json"); !strings.Contains(js, "README 安装命令") || strings.Contains(js, `"confirm"`) {
		t.Fatalf("tree: %s", js)
	}
	if _, _, c := runTrack(t, "confirm", "ls", "--file", file); c == 0 {
		t.Fatal("confirm still exists")
	}
	if _, _, c := runTrack(t, "file", "--file", file, "--hc", hc, "--herdr", herdr, "--pane", "hc:nope", "x"); c == 0 {
		t.Fatal("filed from a pane that doesn't exist")
	}
}

// claudeHome makes a fake ~/.claude with one session for pid, holding lines.
func claudeHome(t *testing.T, home string, pid, sid string, lines ...string) string {
	t.Helper()
	os.MkdirAll(filepath.Join(home, ".claude", "sessions"), 0o700)
	os.WriteFile(filepath.Join(home, ".claude", "sessions", pid+".json"), []byte(`{"pid":`+pid+`,"sessionId":"`+sid+`"}`), 0o600)
	proj := filepath.Join(home, ".claude", "projects", "-p-acme-api")
	os.MkdirAll(proj, 0o700)
	p := filepath.Join(proj, sid+".jsonl")
	os.WriteFile(p, []byte(strings.Join(lines, "")), 0o600)
	return p
}

func typedLine(text string) string {
	b, _ := json.Marshal(map[string]any{"type": "user", "origin": map[string]string{"kind": "human"}, "promptSource": "typed", "message": map[string]any{"role": "user", "content": text}})
	return string(b) + "\n"
}

func replyLine(text string) string {
	b, _ := json.Marshal(map[string]any{"type": "assistant", "message": map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "text", "text": text}}, "stop_reason": "end_turn"}})
	return string(b) + "\n"
}

// hc with Claude pane t2 (pid 4242), Herdr's filing pane t8 and a shell.
func scanHC(t *testing.T) (string, string) {
	return fakeHCFiles(t, `{"terminals":[
{"address":"t2","name":"api","state":"live","status":"done","turn":3,"cwd":"/p/acme-api","agent":"claude-code"},
{"address":"t8","name":"herdr-organizer","state":"live","status":"idle","turn":1,"agent":"claude-code"},
{"address":"t5","state":"live","turn":1,"agent":"zsh"}]}`, map[string]string{
		"t2": `{"address":"t2","state":"live","status":{"state":"done","msg":"端口改好了"},"status_ms":1,"foreground":{"pid":4242}}`,
		"t8": `{"address":"t8","state":"live","labels":{"herdr.role":"organizer"},"status":{"state":"idle"},"foreground":{"pid":4343}}`,
		"t5": `{"address":"t5","state":"live"}`,
	})
}

// A scan queues each Claude pane's current session once (never Herdr's own
// panes).
func TestTrackScanQueuesEachPanesSessionOnce(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)
	file := filepath.Join(t.TempDir(), "track.json")
	hc, _ := scanHC(t)
	claudeHome(t, home, "4242", "s1", typedLine("修 total_items"), replyLine("改好了"), typedLine("README 端口改成 8080"), replyLine("端口改好了"))
	claudeHome(t, home, "4343", "s9", typedLine("整理队列"))
	ok := trackOK(t, []string{"--file", file, "--hc", hc, "--herdr", "-"})
	if out := ok("scan"); !strings.Contains(out, "1 panes") {
		t.Fatalf("scan: %s", out)
	}
	var v track.QueueView
	json.Unmarshal([]byte(ok("queue", "--json")), &v)
	if len(v.Items) != 1 || v.Items[0].Kind != "session" || !strings.Contains(v.Items[0].Text, "README 端口改成 8080") || !strings.Contains(v.Items[0].Text, "端口改好了") {
		t.Fatalf("queued: %+v", v.Items)
	}
	// Each thing the user asked for comes with what the agent answered to it,
	// so the filing pane can tell which are finished.
	if want := "- User: 修 total_items\n  Agent: 改好了\n- User: README 端口改成 8080\n  Agent: 端口改好了\n"; !strings.Contains(v.Items[0].Text, want) {
		t.Fatalf("session text:\n%s\nwant:\n%s", v.Items[0].Text, want)
	}
	if strings.Contains(v.Items[0].Text, "整理队列") {
		t.Fatal("scanned Herdr's own filing pane")
	}
	ok("scan")
	json.Unmarshal([]byte(ok("queue", "--json")), &v)
	if len(v.Items) != 1 {
		t.Fatalf("queued twice: %+v", v.Items)
	}
	// The filing pane turns the session into tasks the way its rules say: --stage
	// for one that looks done, --more while more tasks are to come from it.
	q := v.Items[0].ID
	ok("place", q, "--task", "new", "--title", "修 total_items", "--stage", "review", "--more")
	ok("place", q, "--task", "new", "--title", "README 端口改成 8080")
	tree := ok("tree")
	if !strings.Contains(tree, "review  T1 修 total_items (filed by rule)") || !strings.Contains(tree, "T2 README 端口改成 8080") || strings.Contains(tree, "still to file") {
		t.Fatalf("tree:\n%s", tree)
	}
}

// Turning the progress page on is the user's yes to sorting what the open
// panes already hold: the first pass scans them once, nobody is asked.
func TestTurningProgressOnScansTheOpenPanesOnce(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)
	state := t.TempDir()
	t.Setenv("HERDR_ANYWHERE_STATE", state)
	hc, _ := scanHC(t)
	claudeHome(t, home, "4242", "s1", typedLine("修 total_items"), replyLine("改好了"))
	st := track.Open(filepath.Join(state, "track.json"))
	w := &watch.Watcher{Home: home, State: filepath.Join(state, "watch.json"), List: func() ([]watch.Term, error) { return watchTerms(hc) }}
	var log strings.Builder
	progressTick(w, hc, state, st, &log)
	if f, _ := st.Load(); len(f.Queue) != 0 {
		t.Fatalf("scanned while off: %+v", f.Queue)
	}
	progressCommand([]string{"on"}, &log, &log)
	progressTick(w, hc, state, st, &log)
	f, _ := st.Load()
	if len(f.Queue) != 1 || f.Queue[0].Kind != "session" || !strings.Contains(f.Queue[0].Text, "修 total_items") {
		t.Fatalf("queue after on: %+v\n%s", f.Queue, log.String())
	}
	progressTick(w, hc, state, st, &log)
	if f, _ := st.Load(); len(f.Queue) != 1 {
		t.Fatalf("scanned twice: %+v", f.Queue)
	}
	// The tree offers nothing to answer.
	if js := trackOK(t, []string{"--file", filepath.Join(state, "track.json"), "--hc", hc, "--herdr", "-"})("tree", "--json"); strings.Contains(js, `"scan"`) {
		t.Fatalf("tree still offers a scan: %s", js)
	}
}

func TestOrganizerDueOnABatchAnOldItemAScannedSessionOrAStop(t *testing.T) {
	cfg := organizerConfig{Batch: 3, Wait: 3 * time.Minute}
	now := time.Date(2026, 11, 7, 21, 0, 0, 0, time.UTC)
	item := func(ago time.Duration, kind string) track.Pending {
		return track.Pending{At: now.Add(-ago).UnixMilli(), Kind: kind}
	}
	for _, c := range []struct {
		q    []track.Pending
		want bool
	}{
		{nil, false},
		{[]track.Pending{item(time.Minute, "")}, false},
		{[]track.Pending{item(4*time.Minute, "")}, true},
		{[]track.Pending{item(0, ""), item(0, ""), item(0, "")}, true},
		{[]track.Pending{item(0, "session")}, true},
		{[]track.Pending{item(0, "stop")}, true}, // the user wants to see it finished, or what it asks
	} {
		if got := organizerDue(c.q, cfg, now); got != c.want {
			t.Fatalf("%+v: %v", c.q, got)
		}
	}
}

// The filing pane gets the batch prompt only while it is idle or done, and is
// started when there is none.
func TestOrganizerKickTypesTheBatchOnlyIntoAnIdleFilingPane(t *testing.T) {
	state := t.TempDir()
	st := track.Open(filepath.Join(state, "track.json"))
	st.Update(func(f *track.File) error {
		f.Queue = []track.Pending{{ID: "Q1", Kind: "session", Text: "x", At: 1}}
		return nil
	})
	hc, sent := fakeHCFiles(t, `{"terminals":[{"address":"t8","name":"herdr-organizer","state":"live","status":"working","turn":1,"agent":"claude-code"}]}`,
		map[string]string{"t8": `{"address":"t8","state":"live","labels":{"herdr.role":"organizer"},"status":{"state":"working"}}`})
	if did, err := organizerKick(hc, state, st, time.Now()); err != nil || did != "" {
		t.Fatalf("busy: %q %v", did, err)
	}
	if b, _ := os.ReadFile(sent); len(b) != 0 {
		t.Fatalf("typed into a working pane: %s", b)
	}
	os.WriteFile(filepath.Join(filepath.Dir(hc), "get-t8.json"), []byte(`{"address":"t8","state":"live","labels":{"herdr.role":"organizer"},"status":{"state":"idle"}}`), 0o600)
	if did, err := organizerKick(hc, state, st, time.Now()); err != nil || !strings.Contains(did, "t8") {
		t.Fatalf("idle: %q %v", did, err)
	}
	if b, _ := os.ReadFile(sent); !strings.Contains(string(b), "send t8 Filing queue") {
		t.Fatalf("sent: %s", b)
	}
	// A same-named pane without the label is someone else's: not used.
	os.WriteFile(filepath.Join(filepath.Dir(hc), "get-t8.json"), []byte(`{"address":"t8","state":"live","status":{"state":"idle"}}`), 0o600)
	if _, err := organizerKick(hc, state, st, time.Now()); err == nil {
		t.Fatal("used an unlabelled pane")
	}
}

// `progress on|off` is the switch the phone's 进度页 toggle runs: while off, the
// serve loop neither reads sessions nor types into the filing pane.
func TestProgressSwitchTurnsTheFilingLoopOnAndOff(t *testing.T) {
	state := t.TempDir()
	t.Setenv("HERDR_ANYWHERE_STATE", state)
	st := track.Open(filepath.Join(state, "track.json"))
	st.Update(func(f *track.File) error {
		f.Queue = []track.Pending{{ID: "Q1", Kind: "session", Text: "x", At: 1}}
		return nil
	})
	hc, sent := fakeHCFiles(t, `{"terminals":[{"address":"t8","name":"herdr-organizer","state":"live","status":"idle","turn":1,"agent":"claude-code"}]}`,
		map[string]string{"t8": `{"address":"t8","state":"live","labels":{"herdr.role":"organizer"},"status":{"state":"idle"}}`})
	w := &watch.Watcher{Home: t.TempDir(), State: filepath.Join(state, "watch.json"), List: func() ([]watch.Term, error) { return nil, nil }}
	var log strings.Builder
	progressTick(w, hc, state, st, &log)
	if b, _ := os.ReadFile(sent); len(b) != 0 {
		t.Fatalf("typed while off: %s", b)
	}
	for _, c := range []struct {
		arg  string
		want bool
	}{{"on", true}, {"off", false}, {"on", true}} {
		if err := progressCommand([]string{c.arg}, &log, &log); err != nil || progressOn(state) != c.want {
			t.Fatalf("progress %s: %v %v", c.arg, err, progressOn(state))
		}
	}
	progressTick(w, hc, state, st, &log)
	if b, _ := os.ReadFile(sent); !strings.Contains(string(b), "send t8") {
		t.Fatalf("not kicked while on: %q %s", b, log.String())
	}
	// `progress once --hc` runs the same pass from the command line.
	os.Remove(sent)
	var out strings.Builder
	if err := progressCommand([]string{"once", "--hc", hc}, &out, &out); err != nil {
		t.Fatalf("once: %v %s", err, out.String())
	}
	if b, _ := os.ReadFile(sent); !strings.Contains(string(b), "send t8") || !strings.Contains(out.String(), "t8") {
		t.Fatalf("once did not kick: %q %s", b, out.String())
	}
}

// The filing pane runs `herdr-anywhere track …` in its own shell: it must read
// the same progress file as the companion that started it, herdr-core only.
func TestTheFilingPaneUsesTheCompanionsProgressFile(t *testing.T) {
	state := t.TempDir()
	st := track.Open(filepath.Join(state, "track.json"))
	st.Update(func(f *track.File) error {
		f.Queue = []track.Pending{{ID: "Q1", Kind: "session", Text: "x", At: 1}}
		return nil
	})
	d := t.TempDir()
	ran := filepath.Join(d, "ran")
	hc := testfake.HC(t, d, "hc", testfake.Spec{Version: "hc 0.1.18", On: map[string][]testfake.Reply{
		"ls": {{If: ran, Print: `{"terminals":[{"address":"t9","name":"herdr-organizer","state":"live","status":"working","turn":0,"agent":"claude-code"}]}`},
			{Print: `{"terminals":[]}`}},
		"get":     {{Print: `{"address":"t9","state":"live","labels":{"herdr.role":"organizer"},"status":{"state":"working"}}`}},
		"connect": {{Stdin: ran, Print: `{"jsonrpc":"2.0","id":1,"result":{"address":"t9"}}`}},
	}})
	if _, err := organizerKick(hc, state, st, time.Now()); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(d, "ran"))
	var req struct {
		Method string
		Params struct {
			Env    map[string]string
			Labels map[string]string
		}
	}
	if err := json.Unmarshal(b, &req); err != nil || req.Method != "terminal.run" || req.Params.Env["HERDR_ANYWHERE_STATE"] != state ||
		req.Params.Env["HERDR_TRACK_HERDR"] != "-" || req.Params.Labels["herdr.role"] != "organizer" {
		t.Fatalf("the filing pane was started with %s", b)
	}
	// Version one asks the user nothing: the rules never send anything to be ticked.
	rules, _ := os.ReadFile(filepath.Join(state, "organizer", "CLAUDE.md"))
	for _, want := range []string{"kind stop", `"stage": "done"`, `"stage": "waiting", "waiting":`, "language"} {
		if !strings.Contains(string(rules), want) {
			t.Fatalf("rules lack %q:\n%s", want, rules)
		}
	}
	for _, not := range []string{"confirm", "tick", "--sure", "review"} {
		if len(rules) == 0 || strings.Contains(string(rules), not) {
			t.Fatalf("rules have %q (or are missing):\n%s", not, rules)
		}
	}
}

func TestTrackTakesHerdrFromTheEnvironment(t *testing.T) {
	file := filepath.Join(t.TempDir(), "track.json")
	hc, _ := fakeLists(t, `{"terminals":[]}`, `{"result":{"agents":[]}}`)
	// The herdr on PATH notes that it ran; with HERDR_TRACK_HERDR=- it never does.
	dir := t.TempDir()
	mark := filepath.Join(dir, "ran")
	testfake.HC(t, dir, "herdr", testfake.Spec{On: map[string][]testfake.Reply{"agent": {{Append: mark, Print: `{"result":{"agents":[]}}`}}}})
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	runTrack(t, "tree", "--file", file, "--hc", hc)
	if _, err := os.Stat(mark); err != nil {
		t.Fatal("the check is broken: herdr on PATH did not run without the variable")
	}
	os.Remove(mark)
	t.Setenv("HERDR_TRACK_HERDR", "-")
	if _, e, c := runTrack(t, "tree", "--file", file, "--hc", hc); c != 0 {
		t.Fatalf("%d %s", c, e)
	}
	if _, err := os.Stat(mark); err == nil {
		t.Fatal("ran herdr with HERDR_TRACK_HERDR=-")
	}
}

// The filing pane's first start stops on Claude's folder-trust dialog (seen in companion/eval/filing),
// where it reports no status. While something waits for it, the tree says so,
// with the pane to open; once it reports a status, it doesn't.
func TestTheTreeSaysWhenTheFilingPaneHasNotStarted(t *testing.T) {
	state := t.TempDir()
	file := filepath.Join(state, "track.json")
	hc, _ := fakeHCFiles(t, `{"terminals":[{"address":"t8","name":"herdr-organizer","state":"live","turn":0,"agent":"claude"}]}`,
		map[string]string{"t8": `{"address":"t8","state":"live","labels":{"herdr.role":"organizer"},"status":null}`})
	ok := trackOK(t, []string{"--file", file, "--hc", hc, "--herdr", "-"})
	if js := ok("tree", "--json"); strings.Contains(js, "organizer_waits") {
		t.Fatalf("nothing queued, yet: %s", js)
	}
	ok("file", "--pane", "hc:t8", "x") // a pane whose message waits for the filing pane
	if js := ok("tree", "--json"); !strings.Contains(js, `"organizer_waits": {`) || !strings.Contains(js, `"addr": "t8"`) {
		t.Fatalf("no note: %s", js)
	}
	if out := ok("tree"); !strings.Contains(out, "has not started") {
		t.Fatalf("text: %s", out)
	}
	os.WriteFile(filepath.Join(filepath.Dir(hc), "get-t8.json"), []byte(`{"address":"t8","state":"live","labels":{"herdr.role":"organizer"},"status":{"state":"working"}}`), 0o600)
	if js := ok("tree", "--json"); strings.Contains(js, "organizer_waits") {
		t.Fatalf("still noted while working: %s", js)
	}
}

// A pane that stops on a task is queued for the filing pane on the next pass;
// the filing pane answers with place --stage, and a finished task shows in 刚做完.
func TestAStoppedPaneIsJudgedByTheFilingPane(t *testing.T) {
	state := t.TempDir()
	t.Setenv("HERDR_ANYWHERE_STATE", state)
	file := filepath.Join(state, "track.json")
	hc, _ := scanHC(t) // t2 api: done, "端口改好了"
	ok := trackOK(t, []string{"--file", file, "--hc", hc, "--herdr", "-"})
	ok("add", "P1", "Acme 1.0 发布")
	ok("add", "--under", "P1", "T1", "README 端口改成 8080")
	handedTo(t, []string{"--file", file, "--hc", hc, "--herdr", "-"}, "T1", "hc:t2", "README 端口改成 8080")
	st := track.Open(file)
	st.Update(func(f *track.File) error {
		f.ScanAnswer = "yes"
		t1, _ := f.Get("T1")
		t1.Panes[0].At = 0 // handed over before the pane's status changed
		return nil
	})
	progressCommand([]string{"on"}, io.Discard, io.Discard)
	w := &watch.Watcher{Home: t.TempDir(), State: filepath.Join(state, "watch.json"), List: func() ([]watch.Term, error) { return nil, nil }}
	var log strings.Builder
	progressTick(w, hc, state, st, &log)
	var v track.QueueView
	json.Unmarshal([]byte(ok("queue", "--json")), &v)
	if len(v.Items) != 1 || v.Items[0].Kind != "stop" || v.Items[0].Task != "T1" || v.Items[0].Text != "端口改好了" {
		t.Fatalf("queue: %+v\n%s", v.Items, log.String())
	}
	if _, e, c := runTrack(t, "place", v.Items[0].ID, "--stage", "waiting", "--file", file, "--hc", hc, "--herdr", "-"); c == 0 {
		t.Fatal("waiting without the question")
	} else if !strings.Contains(e, "--waiting") {
		t.Fatalf("err: %s", e)
	}
	ok("place", v.Items[0].ID, "--stage", "done")
	if out := ok("tree"); !strings.Contains(out, "Just done\n  T1 README 端口改成 8080  Acme 1.0 发布") {
		t.Fatalf("tree:\n%s", out)
	}
	progressTick(w, hc, state, st, &log)
	if json.Unmarshal([]byte(ok("queue", "--json")), &v); len(v.Items) != 0 {
		t.Fatalf("judged twice: %+v", v.Items)
	}
}

// The filing pane's rules and settings belong to the companion: after an update
// the pane gets the new ones, even one already running, or a fix to the rules
// never reaches anyone who turned the progress page on before it.
func TestTheFilingPaneGetsTheRulesOfThisCompanion(t *testing.T) {
	state := t.TempDir()
	st := track.Open(filepath.Join(state, "track.json"))
	st.Update(func(f *track.File) error {
		f.Queue = []track.Pending{{ID: "Q1", Kind: "session", Text: "x", At: 1}}
		return nil
	})
	dir := filepath.Join(state, "organizer")
	os.MkdirAll(filepath.Join(dir, ".claude"), 0o700)
	for _, n := range []string{"CLAUDE.md", "AGENTS.md", filepath.Join(".claude", "settings.json")} {
		os.WriteFile(filepath.Join(dir, n), []byte("an older companion's rules"), 0o600)
	}
	d := t.TempDir()
	live := `{"terminals":[{"address":"t9","name":"herdr-organizer","state":"live","status":"working","turn":0,"agent":"claude-code"}]}`
	hc := testfake.HC(t, d, "hc", testfake.Spec{Version: "hc 0.1.18", On: map[string][]testfake.Reply{
		"ls":  {{Print: live}},
		"get": {{Print: `{"address":"t9","state":"live","labels":{"herdr.role":"organizer"},"status":{"state":"working"}}`}},
	}})
	if _, err := organizerKick(hc, state, st, time.Now()); err != nil {
		t.Fatal(err)
	}
	for n, want := range map[string]string{"CLAUDE.md": organizerRulesText(), "AGENTS.md": organizerRulesText(), filepath.Join(".claude", "settings.json"): organizerSettings} {
		if b, _ := os.ReadFile(filepath.Join(dir, n)); string(b) != want {
			t.Errorf("%s is still %q", n, b)
		}
	}
}

// A progress file scanned by an older companion, whose scan gave the filing
// pane less to go on, is scanned once more after the update: the open panes'
// sessions go back to the filing pane, which puts their tasks right.
func TestAfterAnUpdateTheOpenPanesAreScannedOnceMore(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)
	state := t.TempDir()
	t.Setenv("HERDR_ANYWHERE_STATE", state)
	hc, _ := scanHC(t)
	claudeHome(t, home, "4242", "s1", typedLine("修 total_items"), replyLine("改好了"))
	st := track.Open(filepath.Join(state, "track.json"))
	st.Update(func(f *track.File) error {
		f.ScanAnswer, f.Scanned = "yes", []string{"s1"} // scanned before, by a companion with no scan format
		f.Things = []track.Thing{{ID: "T1", Outcome: "修 total_items", Stage: "doing"}}
		return nil
	})
	w := &watch.Watcher{Home: home, State: filepath.Join(state, "watch.json"), List: func() ([]watch.Term, error) { return watchTerms(hc) }}
	var log strings.Builder
	progressCommand([]string{"on"}, &log, &log)
	progressTick(w, hc, state, st, &log)
	f, _ := st.Load()
	if len(f.Queue) != 1 || f.Queue[0].Kind != "session" || !strings.Contains(f.Queue[0].Text, "Agent: 改好了") {
		t.Fatalf("not scanned again: %+v\n%s", f.Queue, log.String())
	}
	if f.ScanFormat != track.ScanFormat {
		t.Fatalf("scan format %d, want %d", f.ScanFormat, track.ScanFormat)
	}
	progressTick(w, hc, state, st, &log)
	if f, _ := st.Load(); len(f.Queue) != 1 {
		t.Fatalf("scanned again on the next pass: %+v", f.Queue)
	}
}
