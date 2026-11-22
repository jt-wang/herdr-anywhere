package antigravity

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"herdr-anywhere/companion/internal/chat"
)

func checkWant(t *testing.T, path, want string) {
	t.Helper()
	b, err := os.ReadFile(want)
	if err != nil {
		t.Fatalf("no %s: %v", want, err)
	}
	var w []chat.Turn
	if err := json.Unmarshal(b, &w); err != nil {
		t.Fatalf("%s: %v", want, err)
	}
	got, err := (&Reader{}).Turns(path)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	if !reflect.DeepEqual(norm(got), norm(w)) {
		g, _ := json.MarshalIndent(norm(got), "", "  ")
		t.Fatalf("%s: turns differ from %s; got\n%s", path, want, g)
	}
}

// norm drops what changes from run to run: durations and times (ids are step numbers, the same every run).
func norm(ts []chat.Turn) []chat.Turn {
	out := []chat.Turn{}
	for _, t := range ts {
		t.Secs, t.At = 0, ""
		if t.Steps == nil {
			t.Steps = []chat.Step{}
		}
		out = append(out, t)
	}
	return out
}

// Every fixture written by an installed agy reads as its want.json says.
func TestFixtures(t *testing.T) {
	files, _ := filepath.Glob("testdata/antigravity/*/*/transcript.jsonl")
	if len(files) == 0 {
		t.Fatal("no fixtures")
	}
	for _, f := range files {
		t.Run(f, func(t *testing.T) { checkWant(t, f, filepath.Join(filepath.Dir(f), "want.json")) })
	}
}

// --- hand-made steps, in the shapes agy 1.5.14 writes (fixtures above) -------------------------------------------

func at(i int) string { return fmt.Sprintf("2026-01-01T12:00:%02dZ", i%60) }

func user(i int, text string) string {
	c := "<USER_REQUEST>\n" + text + "\n</USER_REQUEST>\n<ADDITIONAL_METADATA>\nThe current local time is: 2026-01-01T12:00:00Z.\n</ADDITIONAL_METADATA>"
	return fmt.Sprintf(`{"step_index":%d,"source":"USER_EXPLICIT","type":"USER_INPUT","status":"DONE","created_at":%q,"content":%q}`, i, at(i), c)
}

// call is a tool call as agy records it: each argument's value JSON-encoded as a string.
func tc(name string, args map[string]any) map[string]any {
	enc := map[string]string{}
	for k, v := range args {
		b, _ := json.Marshal(v)
		enc[k] = string(b)
	}
	return map[string]any{"name": name, "args": enc}
}

func resp(i int, content string, calls ...map[string]any) string {
	m := map[string]any{"step_index": i, "source": "MODEL", "type": "PLANNER_RESPONSE", "status": "DONE", "created_at": at(i)}
	if content != "" {
		m["content"] = content
	}
	if len(calls) > 0 {
		m["tool_calls"] = calls
	}
	b, _ := json.Marshal(m)
	return string(b)
}

func think(i int, thinking, content string) string {
	b, _ := json.Marshal(map[string]any{"step_index": i, "source": "MODEL", "type": "PLANNER_RESPONSE", "status": "DONE", "created_at": at(i), "thinking": thinking, "content": content})
	return string(b)
}

// result is the step after a tool call: what it did, under agy's time lines.
func result(i int, status, out string) string {
	c := "Created At: " + at(i) + "\nCompleted At: " + at(i) + "\n" + out
	b, _ := json.Marshal(map[string]any{"step_index": i, "source": "MODEL", "type": "GENERIC", "status": status, "created_at": at(i), "content": c})
	return string(b)
}

func system(i int, text string) string {
	c := "The following is a <SYSTEM_MESSAGE> not actually sent by the user. It is provided by the system as important information to pay attention to.\n\n<SYSTEM_MESSAGE>\n" + text + "\n</SYSTEM_MESSAGE>"
	b, _ := json.Marshal(map[string]any{"step_index": i, "source": "SYSTEM", "type": "SYSTEM_MESSAGE", "status": "DONE", "created_at": at(i), "content": c})
	return string(b)
}

// record writes a conversation's folder as agy keeps it: brain/<id>/.system_generated/logs/transcript.jsonl, and
// transcript_full.jsonl beside it (the same lines unless full says otherwise, by step number).
func record(t *testing.T, lines []string, full map[int]string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "brain", "0b7f3c2e-9d41-4a5e-8c6f-2a1b3c4d5e6f", ".system_generated", "logs")
	os.MkdirAll(dir, 0o755)
	var long []string
	for i, l := range lines {
		var s struct {
			Step int `json:"step_index"`
		}
		json.Unmarshal([]byte(l), &s)
		if f, ok := full[s.Step]; ok {
			long = append(long, f)
		} else {
			long = append(long, lines[i])
		}
	}
	p := filepath.Join(dir, "transcript.jsonl")
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "transcript_full.jsonl"), []byte(strings.Join(long, "\n")+"\n"), 0o644)
	return p
}

func turns(t *testing.T, lines ...string) []chat.Turn {
	t.Helper()
	ts, err := (&Reader{}).Turns(record(t, lines, nil))
	if err != nil {
		t.Fatal(err)
	}
	return ts
}

func detail(t *testing.T, path, id string) chat.Detail {
	t.Helper()
	d, err := (&Reader{}).Detail(path, id)
	if err != nil {
		t.Fatalf("Detail(%s): %v", id, err)
	}
	return d
}

// What the person typed, without agy's wrappers: its metadata, a settings change, the plan-mode note.
func TestSaid(t *testing.T) {
	plan := "<USER_REQUEST>\n/plan merge the notes\n</USER_REQUEST>\n<ADDITIONAL_METADATA>\nThe current local time is: 2026-01-01T12:00:00Z.\n\n/plan is a [Slash Command]:\n<PLAN>The user is requesting…</PLAN>\n</ADDITIONAL_METADATA>\n<USER_SETTINGS_CHANGE>\nThe user changed setting `Model Selection` from None to Fake Model.\n</USER_SETTINGS_CHANGE>"
	bare := "fix the <b>bold</b> bug\n<ADDITIONAL_METADATA>\nnoise\n</ADDITIONAL_METADATA>"
	ts := turns(t,
		fmt.Sprintf(`{"step_index":0,"source":"USER_EXPLICIT","type":"USER_INPUT","status":"DONE","created_at":%q,"content":%q}`, at(0), plan),
		resp(1, "Planned."),
		fmt.Sprintf(`{"step_index":2,"source":"USER_EXPLICIT","type":"USER_INPUT","status":"DONE","created_at":%q,"content":%q}`, at(2), bare),
		resp(3, "Fixed."),
	)
	if len(ts) != 2 || ts[0].Said != "/plan merge the notes" || ts[1].Said != "fix the <b>bold</b> bug" {
		t.Fatalf("said: %+v", ts)
	}
	if ts[0].From != "you" || ts[0].End != "done" || ts[0].Reply != "Planned." || ts[0].ID != "0" || ts[0].At != at(0) {
		t.Fatalf("turn: %+v", ts[0])
	}
}

// agy's own messages to the model (reminders, a background task's end, a subagent's report, the plan-mode handoff)
// are not turns of the person's, and don't end the turn they come in.
func TestSystemIsNotATurn(t *testing.T) {
	ts := turns(t,
		user(0, "WORK through it"),
		resp(1, "Step one.", tc("run_command", map[string]any{"CommandLine": "make", "toolSummary": "Build", "WaitMsBeforeAsync": 5000})),
		result(2, "DONE", "Command exited with code 0.\n\nCommand output:\nok\r\n"),
		system(3, "Reminder: work efficiently."),
		user(4, "[plan review] Plan mode is on: this turn runs on the planner."),
		resp(5, "All done."),
	)
	if len(ts) != 1 || ts[0].End != "done" || ts[0].Reply != "All done." || len(ts[0].Steps) != 2 {
		t.Fatalf("turns: %+v", ts)
	}
	if s := ts[0].Steps; s[0].Kind != "note" || s[0].Title != "Step one." || s[1].Kind != "run" || s[1].Title != "make" || s[1].Failed {
		t.Fatalf("steps: %+v", s)
	}
}

func TestToolKinds(t *testing.T) {
	h, _ := os.UserHomeDir()
	in := filepath.Join(h, "proj", "main.go")
	cases := []struct {
		call        map[string]any
		kind, title string
	}{
		{tc("read_file", map[string]any{"Path": in}), "read", filepath.Join("~", "proj", "main.go")},
		{tc("edit_file", map[string]any{"Path": "/srv/a.go", "Target": "x", "Replacement": "y"}), "edit", "/srv/a.go"},
		{tc("write_file", map[string]any{"Path": "/srv/b.go", "Content": "z"}), "edit", "/srv/b.go"},
		{tc("run_command", map[string]any{"CommandLine": "go test ./...\necho done", "toolSummary": "Tests"}), "run", "go test ./..."},
		{tc("run_command", map[string]any{"CommandLine": "", "toolSummary": "Tests"}), "run", "Tests"},
		{tc("grep_search", map[string]any{"Query": "TODO", "SearchPath": "/srv"}), "search", "TODO"},
		{tc("find_by_name", map[string]any{"Pattern": "*.go", "SearchDirectory": "/srv"}), "search", "*.go"},
		{tc("list_dir", map[string]any{"DirectoryPath": "/srv"}), "read", "/srv"},
		{tc("search_web", map[string]any{"query": "go generics"}), "web", "go generics"},
		{tc("read_url_content", map[string]any{"Url": "https://example.com/a"}), "web", "https://example.com/a"},
		{tc("invoke_subagent", map[string]any{"TypeName": "research", "Role": "Code Reader", "Prompt": "read"}), "agent", "Code Reader"},
		{tc("swarm", map[string]any{"action": "start", "title": "Port the tests"}), "agent", "Port the tests"},
		{tc("run_workflow", map[string]any{"Description": "Nightly checks"}), "agent", "Nightly checks"},
		{tc("send_message", map[string]any{"Recipient": "c-1", "Message": "Done?\nreply soon"}), "agent", "Done?"},
		{tc("manage_task", map[string]any{"Action": "list"}), "other", "manage_task"},
		{tc("mcp_tool_from_somewhere", map[string]any{}), "other", "mcp_tool_from_somewhere"},
	}
	for _, c := range cases {
		ts := turns(t, user(0, "go"), resp(1, "", c.call), result(2, "DONE", "ok"), resp(3, "Done."))
		if s := ts[0].Steps; len(s) != 1 || s[0].Kind != c.kind || s[0].Title != c.title || s[0].ID != "1.0" {
			t.Errorf("%v: %+v, want %s %q", c.call["name"], s, c.kind, c.title)
		}
	}
}

// A command: failed by its exit code, with what it printed (its end) and how many lines.
func TestRun(t *testing.T) {
	p := record(t, []string{
		user(0, "BREAK it"),
		resp(1, "", tc("run_command", map[string]any{"CommandLine": "echo checking; exit 3", "toolSummary": "Check run", "WaitMsBeforeAsync": 5000})),
		result(2, "DONE", "Command exited with code 3.\n\nCommand output:\nchecking\r\none test failed\r\n"),
		resp(3, "", tc("run_command", map[string]any{"CommandLine": "sleep 60", "toolSummary": "Wait", "WaitMsBeforeAsync": 500})),
		result(4, "RUNNING", "Tool is running as a background task with task id: c/task-4\nTask Description: sleep 60\nTask logs are available at: file:///work/task-4.log"),
		resp(5, "It failed."),
	}, nil)
	ts, _ := (&Reader{}).Turns(p)
	s := ts[0].Steps
	if len(s) != 2 || !s[0].Failed || s[1].Failed {
		t.Fatalf("steps: %+v", s)
	}
	d := detail(t, p, "1.0")
	if d.Run == nil || d.Run.Command != "echo checking; exit 3" || !d.Run.Failed || d.Run.Code != 3 || d.Run.Output != "checking\none test failed" || d.Run.Lines != 2 {
		t.Fatalf("run: %+v", d.Run)
	}
	if strings.Contains(d.Output, "Created At") || !strings.Contains(d.Output, "Command exited with code 3.") {
		t.Fatalf("output: %q", d.Output)
	}
	var in map[string]any
	if json.Unmarshal([]byte(d.Input), &in) != nil || in["CommandLine"] != "echo checking; exit 3" || in["WaitMsBeforeAsync"] != float64(5000) {
		t.Fatalf("input: %s", d.Input)
	}
	bg := detail(t, p, "3.0")
	if bg.Run == nil || bg.Run.Failed || !strings.Contains(bg.Run.Output, "background task") {
		t.Fatalf("background: %+v", bg.Run)
	}
}

// An edit's lines as a diff, from its Target and Replacement and where agy says it changed the file.
func TestEdit(t *testing.T) {
	p := record(t, []string{
		user(0, "CHANGE it"),
		resp(1, "", tc("edit_file", map[string]any{"Path": "/work/acme/notes.txt", "Target": "a\nold\nz\n", "Replacement": "a\nnew\nnewer\nz\n"})),
		result(2, "DONE", "Made 1 replacement in /work/acme/notes.txt starting at line 4 (+2/-1 lines)."),
		resp(3, "", tc("write_file", map[string]any{"Path": "/work/acme/hello.txt", "Content": "hello\nworld\n"})),
		result(4, "DONE", "Created file."),
		resp(5, "", tc("edit_file", map[string]any{"Path": "/work/acme/gone.txt", "Target": "x", "Replacement": "y"})),
		result(6, "ERROR", "Encountered error in step execution: target not found"),
		resp(7, "Done."),
	}, nil)
	ts, _ := (&Reader{}).Turns(p)
	s := ts[0].Steps
	if len(s) != 3 || s[0].Added != 2 || s[0].Deleted != 1 || s[1].Added != 2 || s[1].Deleted != 0 || !s[2].Failed || s[2].Added != 0 {
		t.Fatalf("steps: %+v", s)
	}
	c := detail(t, p, "1.0").Change
	want := &chat.Change{File: "/work/acme/notes.txt", Added: 2, Deleted: 1, Hunks: []chat.Hunk{{OldStart: 4, NewStart: 4, Lines: []string{" a", "-old", "+new", "+newer", " z"}}}}
	if !reflect.DeepEqual(c, want) {
		t.Fatalf("change: %+v", c)
	}
	w := detail(t, p, "3.0").Change
	if w == nil || w.Added != 2 || !reflect.DeepEqual(w.Hunks, []chat.Hunk{{OldStart: 0, NewStart: 1, Lines: []string{"+hello", "+world"}}}) {
		t.Fatalf("write: %+v", w)
	}
	if c := detail(t, p, "5.0").Change; c != nil {
		t.Fatalf("a failed edit has no change: %+v", c)
	}
}

// A long write is cut to MaxChangeLines lines.
func TestWriteCut(t *testing.T) {
	var b strings.Builder
	for i := 0; i < chat.MaxChangeLines+50; i++ {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	p := record(t, []string{user(0, "go"), resp(1, "", tc("write_file", map[string]any{"Path": "/w/big.txt", "Content": b.String()})), result(2, "DONE", "Created file."), resp(3, "ok")}, nil)
	c := detail(t, p, "1.0").Change
	if c == nil || !c.Cut || c.Added != chat.MaxChangeLines+50 || len(c.Hunks[0].Lines) != chat.MaxChangeLines {
		t.Fatalf("change: cut=%v added=%d", c.Cut, c.Added)
	}
}

func TestRead(t *testing.T) {
	p := record(t, []string{user(0, "go"), resp(1, "", tc("read_file", map[string]any{"Path": "/w/a.go", "StartLine": 10})), result(2, "DONE", "Showing lines 10-29 of 120\n10: package a"), resp(3, "ok")}, nil)
	r := detail(t, p, "1.0").Read
	if r == nil || *r != (chat.Read{File: "/w/a.go", From: 10, Count: 20, Total: 120}) {
		t.Fatalf("read: %+v", r)
	}
}

func TestThinkAndNotes(t *testing.T) {
	p := record(t, []string{user(0, "THINK"), think(1, "The notes may disagree.\nRead both.", "Plan: read both."), resp(2, "And then merge.")}, nil)
	ts, _ := (&Reader{}).Turns(p)
	if s := ts[0].Steps; len(s) != 1 || s[0].Kind != "think" || s[0].Title != "The notes may disagree." || s[0].ID != "1.t" {
		t.Fatalf("steps: %+v", s)
	}
	if ts[0].Reply != "Plan: read both.\n\nAnd then merge." {
		t.Fatalf("reply: %q", ts[0].Reply)
	}
	if d := detail(t, p, "1.t"); d.Input != "The notes may disagree.\nRead both." {
		t.Fatalf("think: %+v", d)
	}
}

// ask_question: open until its answer comes, then a step; options in agy's shape (strings, "(Recommended)" first).
func TestAsk(t *testing.T) {
	q := tc("ask_question", map[string]any{"questions": []any{
		map[string]any{"question": "Which color?", "options": []any{"(Recommended) Blue", "Red"}},
		map[string]any{"question": "Which sizes?", "is_multi_select": true, "options": []any{"Small", "Large (推荐)"}},
	}})
	open := turns(t, user(0, "ASK"), resp(1, "", q))
	a := open[0].Ask
	if open[0].End != "open" || a == nil || a.Text != "Which color?" || !reflect.DeepEqual(a.Options, []string{"(Recommended) Blue", "Red"}) {
		t.Fatalf("open: %+v", open[0])
	}
	want := []chat.Question{
		{Text: "Which color?", Options: []chat.Choice{{Label: "Blue", Recommended: true}, {Label: "Red"}}},
		{Text: "Which sizes?", Multi: true, Options: []chat.Choice{{Label: "Small"}, {Label: "Large", Recommended: true}}},
	}
	if !reflect.DeepEqual(a.Questions, want) {
		t.Fatalf("questions: %+v", a.Questions)
	}
	done := turns(t, user(0, "ASK"), resp(1, "", q), result(2, "DONE", "A1: Red\nA2: Small, Large"), resp(3, "Noted."))
	if done[0].Ask != nil || len(done[0].Steps) != 1 || done[0].Steps[0].Title != "Which color?" || done[0].Steps[0].Kind != "other" {
		t.Fatalf("answered: %+v", done[0])
	}
}

// The plan agy writes in plan mode, for review (ArtifactMetadata.RequestFeedback), is a plan step with its words.
func TestPlan(t *testing.T) {
	plan := "# Merge the notes\n\n1. Read both.\n2. Merge."
	p := record(t, []string{
		user(0, "/plan merge"),
		resp(1, "", tc("write_file", map[string]any{"Path": "/work/home/.gemini/antigravity-cli/brain/c/implementation_plan.md", "Content": plan,
			"ArtifactMetadata": map[string]any{"Summary": "Merge.", "UserFacing": true, "RequestFeedback": true}})),
		result(2, "DONE", "Created file."),
		resp(3, "The plan is ready for your review."),
		user(4, "[Approved] implementation_plan.md\n"),
		resp(5, "", tc("exit_plan_mode", map[string]any{"plan": "## Second plan\nsteps"})),
		result(6, "DONE", "ok"),
		resp(7, "Merged."),
	}, nil)
	// Plan, review and the work after it are one turn: the approval is the person's answer, a step of theirs.
	ts, _ := (&Reader{}).Turns(p)
	if len(ts) != 1 || ts[0].Said != "/plan merge" || ts[0].End != "done" || ts[0].Reply != "Merged." {
		t.Fatalf("turns: %+v", ts)
	}
	want := []chat.Step{{ID: "1.0", Kind: "plan", Title: "Merge the notes"}, {ID: "3.n", Kind: "note", Title: "The plan is ready for your review."},
		{ID: "4", Kind: chat.StepYou, Title: "[Approved] implementation_plan.md"}, {ID: "5.0", Kind: "plan", Title: "Second plan"}}
	if !reflect.DeepEqual(ts[0].Steps, want) {
		t.Fatalf("steps: %+v", ts[0].Steps)
	}
	if d := detail(t, p, "1.0"); d.Input != plan || d.Change != nil {
		t.Fatalf("plan detail: %+v", d)
	}
}

// A turn cut off (Esc while a command runs: no result is written) is stopped when the next prompt comes.
func TestStopped(t *testing.T) {
	ts := turns(t, user(0, "SLOW"), resp(1, "", tc("run_command", map[string]any{"CommandLine": "sleep 60"})), user(3, "HELLO"), resp(4, "Hi."))
	if len(ts) != 2 || ts[0].End != "stopped" || ts[1].End != "done" {
		t.Fatalf("turns: %+v", ts)
	}
	// Still running, it is open.
	if ts := turns(t, user(0, "SLOW"), resp(1, "", tc("run_command", map[string]any{"CommandLine": "sleep 60"}))); ts[0].End != "open" {
		t.Fatalf("open: %+v", ts[0])
	}
	// A prompt with nothing after it yet is open.
	if ts := turns(t, user(0, "HELLO")); ts[0].End != "open" || len(ts[0].Steps) != 0 {
		t.Fatalf("just sent: %+v", ts[0])
	}
}

// A field agy cut in transcript.jsonl (truncated_fields) is read whole from transcript_full.jsonl.
func TestTruncated(t *testing.T) {
	long := strings.Repeat("- a row of the long reply\n", 400)
	cut := long[:2048] + "\n<truncated 7000 bytes>\n" + long[len(long)-300:]
	short := func(i int, c string) string {
		b, _ := json.Marshal(map[string]any{"step_index": i, "source": "MODEL", "type": "PLANNER_RESPONSE", "status": "DONE", "created_at": at(i), "content": c, "truncated_fields": []string{"content"}})
		return string(b)
	}
	big := strings.Repeat("x\n", 3000)
	wcall := map[string]any{"name": "write_file", "args": map[string]any{"Path": "/w/big.txt", "Content": big}}
	wcut := map[string]any{"name": "write_file", "args": map[string]string{"Path": `"/w/big.txt"`, "Content": `"x\nx\n<truncated>`}}
	wshort, _ := json.Marshal(map[string]any{"step_index": 3, "source": "MODEL", "type": "PLANNER_RESPONSE", "status": "DONE", "created_at": at(3), "tool_calls": []any{wcut}, "truncated_fields": []string{"tool_calls"}})
	p := record(t, []string{user(0, "LONG"), short(1, cut), user(2, "WRITE"), string(wshort), result(4, "DONE", "Created file."), resp(5, "ok")},
		map[int]string{1: resp(1, long), 3: resp(3, "", wcall)})
	ts, err := (&Reader{}).Turns(p)
	if err != nil {
		t.Fatal(err)
	}
	if ts[0].Reply != long {
		t.Fatalf("reply cut: %d bytes", len(ts[0].Reply))
	}
	if c := detail(t, p, "3.0").Change; c == nil || c.Added != 3000 {
		t.Fatalf("write from full: %+v", c)
	}
	// Without the full file, the cut words are what there is.
	os.Remove(filepath.Join(filepath.Dir(p), "transcript_full.jsonl"))
	ts, _ = (&Reader{}).Turns(p)
	if ts[0].Reply != cut {
		t.Fatalf("no full file: %q", ts[0].Reply[:40])
	}
}

func TestRestoredJSONWrite(t *testing.T) {
	content := "{\n  \"ready\": true\n}\n"
	short := resp(1, "", tc("write_file", map[string]any{"Path": "/w/settings.json", "Content": "{<truncated>"}))
	short = strings.TrimSuffix(short, "}") + `,"truncated_fields":["tool_calls"]}`
	whole := resp(1, "", map[string]any{"name": "write_file", "args": map[string]any{"Path": "/w/settings.json", "Content": content}})
	p := record(t, []string{user(0, "WRITE"), short, result(2, "DONE", "Created file.")}, map[int]string{1: whole})
	ts, err := (&Reader{}).Turns(p)
	if err != nil {
		t.Fatal(err)
	}
	if got := ts[0].Steps[0].Added; got != 3 {
		t.Fatalf("added lines: %d", got)
	}
	d := detail(t, p, "1.0")
	want := []string{"+{", "+  \"ready\": true", "+}"}
	if d.Change == nil || len(d.Change.Hunks) != 1 || !reflect.DeepEqual(d.Change.Hunks[0].Lines, want) {
		t.Fatalf("diff: %+v", d.Change)
	}
	var input map[string]string
	if json.Unmarshal([]byte(d.Input), &input) != nil || input["Content"] != content {
		t.Fatalf("input: %s", d.Input)
	}
}

func TestFormat(t *testing.T) {
	_, err := (&Reader{}).Turns(record(t, []string{user(0, "go"), "not json"}, nil))
	var fe *chat.FormatError
	if !errors.As(err, &fe) {
		t.Fatalf("err %v", err)
	}
	_, err = (&Reader{}).Turns(record(t, []string{`{"source":"MODEL"}`}, nil))
	if !errors.As(err, &fe) {
		t.Fatalf("no step_index: %v", err)
	}
	// A step type it doesn't know is passed over.
	ts := turns(t, user(0, "go"), `{"step_index":1,"source":"SYSTEM","type":"CHECKPOINT","status":"DONE"}`, resp(2, "ok"))
	if len(ts) != 1 || ts[0].Reply != "ok" {
		t.Fatalf("turns %+v", ts)
	}
}

func TestDetailUnknown(t *testing.T) {
	p := record(t, []string{user(0, "go"), resp(1, "ok")}, nil)
	for _, id := range []string{"9.0", "1.5", "x", "", "1.n.2"} {
		if _, err := (&Reader{}).Detail(p, id); err != chat.ErrNoStep {
			t.Errorf("Detail(%q): %v", id, err)
		}
	}
	if d := detail(t, p, "1.n"); d.Input != "ok" {
		t.Fatalf("note: %+v", d)
	}
}

// --- images ----------------------------------------------------------------------------------------------------------

func png1() []byte { return shot() }

// withMedia writes a conversation whose prompt carries images by those URIs.
func withMedia(t *testing.T, uris ...string) (path, brain string) {
	t.Helper()
	p := record(t, nil, nil)
	brain = filepath.Dir(filepath.Dir(filepath.Dir(p)))
	var ms []map[string]string
	for _, u := range uris {
		u = strings.ReplaceAll(u, "$BRAIN", brain)
		ms = append(ms, map[string]string{"mime_type": "image/png", "uri": u})
	}
	b, _ := json.Marshal(map[string]any{"step_index": 0, "source": "USER_EXPLICIT", "type": "USER_INPUT", "status": "DONE", "created_at": at(0),
		"content": "<USER_REQUEST>\nlook\n</USER_REQUEST>", "media": ms})
	os.WriteFile(p, append(b, '\n'), 0o644)
	return p, brain
}

func TestImage(t *testing.T) {
	outside := filepath.Join(t.TempDir(), "secret.png")
	os.WriteFile(outside, png1(), 0o644)
	p, brain := withMedia(t, "$BRAIN/.user_uploaded/uploaded_media_1.png", "$BRAIN/.user_uploaded/link.png", outside,
		"$BRAIN/.user_uploaded/notes.txt", "$BRAIN/.user_uploaded/../../escape.png", "$BRAIN/.user_uploaded/missing.png")
	up := filepath.Join(brain, ".user_uploaded")
	os.MkdirAll(up, 0o755)
	os.WriteFile(filepath.Join(up, "uploaded_media_1.png"), png1(), 0o644)
	os.WriteFile(filepath.Join(up, "notes.txt"), []byte("not an image"), 0o644)
	os.WriteFile(filepath.Join(filepath.Dir(brain), "escape.png"), png1(), 0o644)
	symlinked := runtime.GOOS != "windows" && os.Symlink(outside, filepath.Join(up, "link.png")) == nil
	ts, err := (&Reader{}).Turns(p)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(ts[0].Images); n != 6 || ts[0].Images[0].ID != "0.m0" {
		t.Fatalf("images: %+v", ts[0].Images)
	}
	r := &Reader{}
	if m, b, err := r.Image(p, "0.m0"); err != nil || m != "image/png" || len(b) != len(png1()) {
		t.Fatalf("the uploaded image: %v %s", err, m)
	}
	// Only a file inside the conversation's own folder, of an image type, that the record names.
	for _, id := range []string{"0.m1", "0.m2", "0.m3", "0.m4", "0.m5", "0.m9", "1.m0", "0.0", "../x"} {
		if id == "0.m1" && !symlinked {
			continue
		}
		if _, _, err := r.Image(p, id); err == nil {
			t.Errorf("Image(%s) served a file", id)
		}
	}
}

// A media URI written as a file: URL or with Windows separators still names the file it names, and nothing else.
func TestImageURIs(t *testing.T) {
	// An image file the record calls something else than an image is not sent.
	{
		p, brain := withMedia(t, "$BRAIN/.user_uploaded/c.png")
		b, _ := os.ReadFile(p)
		os.WriteFile(p, []byte(strings.Replace(string(b), "image/png", "text/plain", 1)), 0o644)
		os.MkdirAll(filepath.Join(brain, ".user_uploaded"), 0o755)
		os.WriteFile(filepath.Join(brain, ".user_uploaded", "c.png"), png1(), 0o644)
		if _, _, err := (&Reader{}).Image(p, "0.m0"); err == nil {
			t.Fatal("a text/plain medium served")
		}
	}
	p, brain := withMedia(t, "file://$BRAIN/.user_uploaded/a.png", `$BRAIN\.user_uploaded\..\..\b.png`)
	up := filepath.Join(brain, ".user_uploaded")
	os.MkdirAll(up, 0o755)
	os.WriteFile(filepath.Join(up, "a.png"), png1(), 0o644)
	os.WriteFile(filepath.Join(filepath.Dir(brain), "b.png"), png1(), 0o644)
	r := &Reader{}
	if runtime.GOOS != "windows" {
		if _, _, err := r.Image(p, "0.m0"); err != nil {
			t.Fatalf("file: URL: %v", err)
		}
	}
	if _, _, err := r.Image(p, "0.m1"); err == nil {
		t.Fatal(`..\ out of the folder served a file`)
	}
}

// --- finding the conversation -----------------------------------------------------------------------------------

func TestFind(t *testing.T) {
	dir := t.TempDir()
	const a, b = "0b7f3c2e-9d41-4a5e-8c6f-2a1b3c4d5e6f", "7c1d2e3f-4a5b-4c6d-8e9f-0a1b2c3d4e5f"
	logs := func(id string) string {
		return filepath.Join(dir, "brain", id, ".system_generated", "logs", "transcript.jsonl")
	}
	for _, id := range []string{a, b} {
		os.MkdirAll(filepath.Dir(logs(id)), 0o755)
		os.WriteFile(logs(id), []byte(user(0, "hi")+"\n"), 0o644)
	}
	lock := func(id string) string { return filepath.Join(dir, "presence", id+".lock") }
	brainDir := func(id string) string { return filepath.Join(dir, "brain", id) }
	cases := []struct {
		name  string
		open  []string
		path  string
		miss  string
		check func(*testing.T, []int)
	}{
		{"one lock", []string{brainDir(a), brainDir(b), lock(b)}, logs(b), "", nil},
		{"none yet", []string{filepath.Join(dir, "settings.json")}, "", chat.NoRecordYet, nil},
		{"two locks", []string{lock(a), lock(b)}, "", chat.Ambiguous, nil},
		{"the same lock twice", []string{lock(a), lock(a)}, logs(a), "", nil},
		{"elsewhere", []string{filepath.Join(t.TempDir(), "presence", a+".lock")}, "", chat.NoRecordYet, nil},
		{"not an id", []string{filepath.Join(dir, "presence", "..lock"), filepath.Join(dir, "presence", "x.lock")}, "", chat.NoRecordYet, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var asked []int
			r := &Reader{Dir: dir, openFiles: func(pids []int) ([]string, error) { asked = pids; return c.open, nil }}
			path, _, err := r.Find(42)
			if c.miss != "" {
				var m *chat.Miss
				if !errors.As(err, &m) || m.Reason != c.miss {
					t.Fatalf("Find: %q %v, want %s", path, err, c.miss)
				}
				return
			}
			if err != nil || path != c.path {
				t.Fatalf("Find: %q %v, want %s", path, err, c.path)
			}
			if len(asked) == 0 || asked[0] != 42 {
				t.Fatalf("asked %v", asked)
			}
		})
	}
	// The lock's conversation without a record yet: written at its first step.
	os.Remove(logs(a))
	r := &Reader{Dir: dir, openFiles: func([]int) ([]string, error) { return []string{lock(a)}, nil }}
	if _, _, err := r.Find(42); err == nil || err.(*chat.Miss).Reason != chat.NoRecordYet {
		t.Fatalf("no record: %v", err)
	}
	// The process's open files can't be read: no sure match.
	r = &Reader{Dir: dir, openFiles: func([]int) ([]string, error) { return nil, errors.New("no lsof") }}
	if _, _, err := r.Find(42); err == nil || err.(*chat.Miss).Reason != chat.NoMatch {
		t.Fatalf("no lsof: %v", err)
	}
}

// --- history ----------------------------------------------------------------------------------------------------

func TestHistory(t *testing.T) {
	var lines []string
	for i := 0; i < chat.MaxTurns+12; i++ {
		lines = append(lines, user(2*i, fmt.Sprintf("prompt %d", i)), resp(2*i+1, fmt.Sprintf("reply %d", i)))
	}
	p := record(t, lines, nil)
	r := &Reader{}
	ts, err := r.Turns(p)
	if err != nil || len(ts) != chat.MaxTurns || ts[0].Said != "prompt 12" {
		t.Fatalf("tail: %d %v", len(ts), err)
	}
	before, err := r.Before(p, ts[0].ID)
	if err != nil || before == "" {
		t.Fatalf("Before: %q %v", before, err)
	}
	page, err := r.History(p, before)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Turns) != 12 || page.Turns[0].Said != "prompt 0" || page.Turns[11].Said != "prompt 11" {
		t.Fatalf("page: %d %+v", len(page.Turns), page.Turns)
	}
	// Appends don't move the page.
	f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString(user(999, "later") + "\n")
	f.Close()
	again, err := r.History(p, before)
	if err != nil || !reflect.DeepEqual(again.Turns, page.Turns) {
		t.Fatalf("after an append: %v", err)
	}
}

func TestHistoryDetailsOutsideTail(t *testing.T) {
	p, brain := withMedia(t, "$BRAIN/shot.png")
	if err := os.WriteFile(filepath.Join(brain, "shot.png"), png1(), 0o644); err != nil {
		t.Fatal(err)
	}
	lines := []string{
		resp(1, "", tc("run_command", map[string]any{"CommandLine": "echo old"})),
		result(2, "DONE", "Command exited with code 0.\nCommand output:\nold"),
	}
	padding := strings.Repeat("a", chat.MaxTail/8)
	for i := 3; i < 29; i += 2 {
		lines = append(lines, user(i, fmt.Sprintf("new prompt %d", i)), resp(i+1, padding))
	}
	f, err := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := f.WriteString(strings.Join(lines, "\n") + "\n")
	if err := f.Close(); err != nil || writeErr != nil {
		t.Fatalf("append: %v, %v", writeErr, err)
	}
	r := &Reader{}
	ts, err := r.Turns(p)
	if err != nil || len(ts) == 0 || ts[0].ID == "0" {
		t.Fatalf("expected original turn outside tail: %d turns, %v", len(ts), err)
	}
	before, err := r.Before(p, ts[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	for before != "" {
		page, err := r.History(p, before)
		if err != nil {
			t.Fatal(err)
		}
		for _, turn := range page.Turns {
			if turn.ID != "0" {
				continue
			}
			d, err := r.Detail(p, turn.Steps[0].ID)
			if err != nil || d.Run == nil || d.Run.Output != "old" {
				t.Fatalf("older command: %+v, %v", d.Run, err)
			}
			mime, b, err := r.Image(p, turn.Images[0].ID)
			if err != nil || mime != "image/png" || !reflect.DeepEqual(b, png1()) {
				t.Fatalf("older image: %s, %d bytes, %v", mime, len(b), err)
			}
			return
		}
		before = page.Before
	}
	t.Fatal("original turn missing from history")
}

// The tail is read again only when the file changed.
func TestCache(t *testing.T) {
	p := record(t, []string{user(0, "go"), resp(1, "ok")}, nil)
	r := &Reader{}
	r.Turns(p)
	r.Turns(p)
	if r.parses != 1 {
		t.Fatalf("parses %d", r.parses)
	}
	f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString(user(2, "more") + "\n")
	f.Close()
	if ts, _ := r.Turns(p); len(ts) != 2 || r.parses != 2 {
		t.Fatalf("after an append: %d turns, %d parses", len(ts), r.parses)
	}
}

// A subagent's report comes after the turn's reply, as a message from agy, and the model answers it: the answer is the
// reply, the words before it a note.
func TestAfterSystem(t *testing.T) {
	ts := turns(t,
		user(0, "HELP"),
		resp(1, "", tc("invoke_subagent", map[string]any{"Role": "Helper"})),
		result(2, "DONE", "Created the following subagent:"),
		resp(3, "The helper is on it."),
		system(4, "[Message] content=Subagent has gone idle. This was the subagent's most recent response: OK."),
		resp(5, "The helper said hello."),
		user(6, "FAIL"),
		system(7, "Reminder."),
		user(8, "HELLO"),
		resp(9, "Hi."),
	)
	if len(ts) != 3 || ts[0].Reply != "The helper said hello." || ts[0].End != "done" {
		t.Fatalf("turns: %+v", ts)
	}
	if s := ts[0].Steps; len(s) != 2 || s[1].Kind != "note" || s[1].Title != "The helper is on it." || s[1].ID != "3.n" {
		t.Fatalf("steps: %+v", s)
	}
	// A turn with nothing after its prompt (the model's API failed) is stopped by the next one.
	if ts[1].End != "stopped" {
		t.Fatalf("failed turn: %+v", ts[1])
	}
}

// Details of what agy 1.5.14 really wrote (the fixtures).
func TestFixtureDetails(t *testing.T) {
	fx := func(name string) string {
		return filepath.Join("testdata", "antigravity", "1.5.14", name, "transcript.jsonl")
	}
	c := detail(t, fx("turns"), "11.0").Change
	if c == nil || c.File != "/work/acme/notes.txt" || c.Added != 2 || c.Deleted != 0 ||
		!reflect.DeepEqual(c.Hunks, []chat.Hunk{{OldStart: 1, NewStart: 1, Lines: []string{" a", "+b", "+c"}}}) {
		t.Fatalf("edit: %+v", c)
	}
	r := detail(t, fx("turns"), "22.0").Run
	if r == nil || !r.Failed || r.Code != 3 || r.Output != "checking\none test failed" || r.Lines != 2 {
		t.Fatalf("run: %+v", r)
	}
	if rd := detail(t, fx("turns"), "9.0").Read; rd == nil || rd.File != "/work/acme/notes.txt" || rd.Total != 1 || rd.Count != 1 {
		t.Fatalf("read: %+v", rd)
	}
	p := detail(t, fx("plan"), "3.0")
	if !strings.HasPrefix(p.Input, "# Merge the notes") || p.Change != nil {
		t.Fatalf("plan: %+v", p)
	}
	if a := detail(t, fx("ask-answered"), "1.0"); !strings.Contains(a.Output, "A1: Red") || !strings.Contains(a.Output, "A2: Small, Large") {
		t.Fatalf("answers: %q", a.Output)
	}
	if th := detail(t, fx("gallery"), "1.t"); !strings.HasPrefix(th.Input, "The notes and the todo list may disagree.") {
		t.Fatalf("think: %+v", th)
	}
	// The long reply, cut in transcript.jsonl, read whole from transcript_full.jsonl.
	ts, _ := (&Reader{}).Turns(fx("gallery"))
	for _, tn := range ts {
		if strings.HasPrefix(tn.Said, "LONG") && (strings.Contains(tn.Reply, "<truncated") || strings.Count(tn.Reply, "\n") < 120) {
			t.Fatalf("long reply: %d bytes", len(tn.Reply))
		}
	}
	// A pasted image's file is not in a fixture: its record names it, inside the conversation's folder.
	if _, _, err := (&Reader{}).Image(fx("image"), "0.m0"); err != chat.ErrNoStep {
		t.Fatalf("image without its file: %v", err)
	}
}

// A plan written for review is waited on: its turn stays open until the person approves or rejects it (a new prompt).
func TestPlanAwaitsReview(t *testing.T) {
	plan := tc("write_file", map[string]any{"Path": "/w/implementation_plan.md", "Content": "# Merge", "ArtifactMetadata": map[string]any{"RequestFeedback": true}})
	ts := turns(t, user(0, "/plan merge"), resp(1, "", plan), result(2, "DONE", "Created file."), resp(3, "The plan is ready for your review."))
	if ts[0].End != "open" || ts[0].Reply != "The plan is ready for your review." {
		t.Fatalf("waiting: %+v", ts[0])
	}
	ts = turns(t, user(0, "/plan merge"), resp(1, "", plan), result(2, "DONE", "Created file."), resp(3, "Ready."), user(4, "[Rejected] implementation_plan.md"), resp(5, "Ok."))
	if len(ts) != 1 || ts[0].End != "done" || ts[0].Steps[2].Title != "[Rejected] implementation_plan.md" || ts[0].Reply != "Ok." {
		t.Fatalf("reviewed: %+v", ts)
	}
	// Without a plan waiting, words like those are a prompt like any other.
	if ts := turns(t, user(0, "[Approved] looks fine"), resp(1, "Ok.")); len(ts) != 1 || ts[0].Said != "[Approved] looks fine" {
		t.Fatalf("no plan: %+v", ts)
	}
	// A file written without asking for review is not waited on.
	w := tc("write_file", map[string]any{"Path": "/w/notes.md", "Content": "x", "ArtifactMetadata": map[string]any{"RequestFeedback": false}})
	if ts := turns(t, user(0, "go"), resp(1, "", w), result(2, "DONE", "Created file."), resp(3, "Done.")); ts[0].End != "done" {
		t.Fatalf("plain write: %+v", ts[0])
	}
}

// Declining an approval ends the turn: agy writes the call as failed and waits at its prompt.
func TestDeclined(t *testing.T) {
	denied, _ := json.Marshal(map[string]any{"step_index": 2, "source": "MODEL", "type": "GENERIC", "status": "ERROR", "created_at": at(2),
		"content": "The user denied permission for this action. Proceed without performing this action.",
		"error":   `permission check failed for write_file "/w/notes.txt": user denied permission`})
	ts := turns(t, user(0, "CHANGE"), resp(1, "", tc("edit_file", map[string]any{"Path": "/w/notes.txt", "Target": "a", "Replacement": "b"})), string(denied))
	if ts[0].End != "stopped" || !ts[0].Steps[0].Failed {
		t.Fatalf("declined: %+v", ts[0])
	}
}
