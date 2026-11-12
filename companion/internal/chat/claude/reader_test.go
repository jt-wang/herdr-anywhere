package claude

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"herdr-anywhere/companion/internal/chat"
)

// checkWant reads path and compares its turns with the JSON in want.
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

// sessionInPath: the session id in where Claude Code keeps a pasted image (/tmp/claude/<project>/<session>/images/).
var sessionInPath = regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)

// norm drops what changes from run to run: ids (an image keeps its place, ".1"), durations, and the session id in an
// image's source.
func norm(ts []chat.Turn) []chat.Turn {
	out := []chat.Turn{}
	for _, t := range ts {
		t.ID, t.Secs, t.At = "", 0, ""
		var images []chat.Image
		for _, im := range t.Images {
			im.ID = im.ID[strings.LastIndexByte(im.ID, '.'):]
			im.Source = sessionInPath.ReplaceAllString(im.Source, "<session>")
			images = append(images, im)
		}
		t.Images = images
		steps := []chat.Step{}
		for _, s := range t.Steps {
			s.ID = ""
			steps = append(steps, s)
		}
		t.Steps = steps
		out = append(out, t)
	}
	return out
}

// Every fixture written by an installed Claude Code reads as its .want.json says.
func TestFixtures(t *testing.T) {
	files, _ := filepath.Glob("testdata/claude-code/*/*.jsonl")
	if len(files) == 0 {
		t.Fatal("no fixtures")
	}
	for _, f := range files {
		t.Run(f, func(t *testing.T) { checkWant(t, f, strings.TrimSuffix(f, ".jsonl")+".want.json") })
	}
}

// --- hand-made lines, in the shapes Claude Code 2.1.319 writes (fixtures above, and a survey of
// running sessions) --------------------------------------------------------------------------------------------------

func prompt(id, text string) string {
	return fmt.Sprintf(`{"type":"user","uuid":%q,"timestamp":"2026-01-01T12:00:00.000Z","cwd":"/work/acme","turnOrigin":"human","origin":{"kind":"human"},"promptSource":"typed","turnPosition":{"promptIndex":0},"message":{"role":"user","content":%q}}`, id, text)
}
func notice(id, text string) string {
	return fmt.Sprintf(`{"type":"user","uuid":%q,"timestamp":"2026-01-01T12:00:00.000Z","turnOrigin":"task_notification","origin":{"kind":"task-notification"},"promptSource":"system","turnPosition":{"promptIndex":1},"message":{"role":"user","content":%q}}`, id, text)
}
func say(id, msg, text, stop string) string {
	return fmt.Sprintf(`{"type":"assistant","uuid":%q,"timestamp":"2026-01-01T12:00:42.000Z","cwd":"/work/acme","message":{"id":%q,"role":"assistant","content":[{"type":"text","text":%q}],"stop_reason":%q}}`, id, msg, text, stop)
}
func use(id, msg, tool, name, input string) string {
	return fmt.Sprintf(`{"type":"assistant","uuid":%q,"timestamp":"2026-01-01T12:00:10.000Z","cwd":"/work/acme","message":{"id":%q,"role":"assistant","content":[{"type":"tool_use","id":%q,"name":%q,"input":%s}],"stop_reason":"tool_use"}}`, id, msg, tool, name, input)
}
func result(id, tool, out string) string {
	return fmt.Sprintf(`{"type":"user","uuid":%q,"timestamp":"2026-01-01T12:00:20.000Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":%q,"content":%q,"is_error":false}]}}`, id, tool, out)
}

const ask = `{"questions":[{"question":"Which color should the button be?","header":"Color","multiSelect":false,"options":[{"label":"Red","description":"Warm"},{"label":"Blue","description":"Cool"}]}]}`

func write(t *testing.T, lines ...string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "s.jsonl")
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func turns(t *testing.T, lines ...string) []chat.Turn {
	t.Helper()
	ts, err := (&Reader{}).Turns(write(t, lines...))
	if err != nil {
		t.Fatal(err)
	}
	return ts
}

func formatError(t *testing.T, lines ...string) string {
	t.Helper()
	_, err := (&Reader{}).Turns(write(t, lines...))
	var fe *chat.FormatError
	if !errors.As(err, &fe) {
		t.Fatalf("want a format error, got %v", err)
	}
	return fe.Field
}

func TestOpenQuestion(t *testing.T) {
	ts := turns(t, prompt("u1", "pick a color"), use("a1", "m1", "toolu_1", "AskUserQuestion", ask))
	if len(ts) != 1 || ts[0].End != "open" || ts[0].Ask == nil {
		t.Fatalf("%+v", ts)
	}
	if ts[0].Ask.Text != "Which color should the button be?" || !reflect.DeepEqual(ts[0].Ask.Options, []string{"Red", "Blue"}) {
		t.Fatalf("%+v", ts[0].Ask)
	}
	if len(ts[0].Steps) != 0 {
		t.Fatalf("an open question is not a step: %+v", ts[0].Steps)
	}
}

func TestAnsweredQuestionIsAStep(t *testing.T) {
	ts := turns(t, prompt("u1", "pick a color"), use("a1", "m1", "toolu_1", "AskUserQuestion", ask),
		result("u2", "toolu_1", "User has answered: Red"), say("a2", "m2", "Red it is.", "end_turn"))
	if ts[0].Ask != nil || ts[0].End != "done" || ts[0].Reply != "Red it is." {
		t.Fatalf("%+v", ts[0])
	}
	if !reflect.DeepEqual(norm(ts)[0].Steps, []chat.Step{{Kind: "other", Title: "Which color should the button be?"}}) {
		t.Fatalf("%+v", ts[0].Steps)
	}
}

func TestInterrupted(t *testing.T) {
	ts := turns(t, prompt("u1", "go"), use("a1", "m1", "toolu_1", "Bash", `{"command":"make"}`),
		result("u2", "toolu_1", "Interrupted"),
		`{"type":"user","uuid":"u3","interruptedMessageId":"m1","message":{"role":"user","content":[{"type":"text","text":"[Request interrupted by user for tool use]"}]}}`)
	if ts[0].End != "stopped" || ts[0].Reply != "" {
		t.Fatalf("%+v", ts[0])
	}
}

func TestNotesRepliesAndJoinedText(t *testing.T) {
	ts := turns(t, prompt("u1", "fix it"),
		say("a1", "m1", "Looking at the test.\nThen the code.", "tool_use"),
		use("a2", "m1", "toolu_1", "Read", `{"file_path":"/work/acme/src/app.py"}`),
		result("u2", "toolu_1", "code"),
		use("a3", "m2", "toolu_2", "Edit", `{"file_path":"/elsewhere/lib.py","old_string":"a","new_string":"b"}`),
		result("u3", "toolu_2", "ok"),
		use("a4", "m3", "toolu_3", "Grep", `{"pattern":"total_items"}`),
		result("u4", "toolu_3", "ok"),
		use("a5", "m4", "toolu_4", "mcp__notes__search_pages", `{"q":"x"}`),
		result("u5", "toolu_4", "ok"),
		say("a6", "m5", "Fixed.", "tool_use"),
		say("a7", "m5", "All tests pass.", "end_turn"))
	want := []chat.Step{{Kind: "note", Title: "Looking at the test."}, {Kind: "read", Title: "src/app.py"}, {Kind: "edit", Title: "/elsewhere/lib.py"},
		{Kind: "search", Title: "total_items"}, {Kind: "other", Title: "search_pages"}}
	if got := norm(ts)[0].Steps; !reflect.DeepEqual(got, want) {
		t.Fatalf("%+v", got)
	}
	if ts[0].Reply != "Fixed.\n\nAll tests pass." || ts[0].End != "done" || ts[0].Secs != 42 {
		t.Fatalf("%+v", ts[0])
	}
}

func TestSkipsMetaCompactSidechainAndQuietLines(t *testing.T) {
	ts := turns(t, prompt("u1", "hi"),
		`{"type":"user","uuid":"m","isMeta":true,"message":{"role":"user","content":"<system-reminder>x</system-reminder>"}}`,
		`{"type":"system","subtype":"compact_boundary","uuid":"s1"}`,
		`{"type":"user","uuid":"c","isCompactSummary":true,"isVisibleInTranscriptOnly":true,"message":{"role":"user","content":"summary"}}`,
		`{"type":"assistant","uuid":"side","isSidechain":true,"message":{"id":"x","role":"assistant","content":[{"type":"text","text":"sub-agent"}],"stop_reason":"end_turn"}}`,
		`{"type":"api-request-blob","uuid":"b","message":{"role":"system","content":[{"type":"text","text":"(removed)"}]}}`,
		`{"type":"some-new-kind-of-line","uuid":"n"}`,
		say("a1", "m1", "hello", "end_turn"))
	if len(ts) != 1 || ts[0].Reply != "hello" || len(ts[0].Steps) != 0 {
		t.Fatalf("%+v", ts)
	}
}

func TestOtherSenders(t *testing.T) {
	ts := turns(t, prompt("u1", "hi"), say("a1", "m1", "hello", "end_turn"), notice("u2", "<task-notification>build done</task-notification>"), say("a2", "m2", "Build passed.", "end_turn"))
	if len(ts) != 2 || ts[0].From != "you" || ts[1].From != "other" || ts[1].Said != "<task-notification>build done</task-notification>" {
		t.Fatalf("%+v", ts)
	}
	// A background task's notice says what happened in its summary; the rest is for the model.
	ts = turns(t, prompt("u1", "hi"), say("a1", "m1", "hello", "end_turn"),
		notice("u2", "<task-notification>\n<task-id>a1</task-id>\n<status>completed</status>\n<summary>Agent \"Ask a helper\" finished</summary>\n<result>Hi.</result>\n</task-notification>"), say("a2", "m2", "Done.", "end_turn"))
	if ts[1].Said != `Agent "Ask a helper" finished` {
		t.Fatalf("%q", ts[1].Said)
	}
}

// A command typed after "!" in Claude Code runs in a shell: its line has no turn of its own, the next one holds its
// output and the turn mark, and no reply follows. The chat shows the command as said by you; the output stays in the
// terminal. (Shape of the lines as Claude Code 2.1.319 and 2.1.322 write them.)
func TestShellCommandAfterBang(t *testing.T) {
	ts := turns(t, prompt("u1", "hi"), say("a1", "m1", "hello", "end_turn"),
		`{"type":"user","uuid":"u2","timestamp":"2026-01-01T12:01:00.000Z","cwd":"/work/acme","entrypoint":"cli","userType":"external","promptId":"p2","message":{"role":"user","content":"<bash-input>ls -la</bash-input>"}}`,
		`{"type":"user","uuid":"u3","timestamp":"2026-01-01T12:01:01.000Z","cwd":"/work/acme","turnOrigin":"human","turnPosition":{"promptIndex":2,"turnIndex":3},"message":{"role":"user","content":"<bash-stdout>a.txt\nb.txt</bash-stdout><bash-stderr></bash-stderr>"}}`,
		prompt("u4", "what is in a.txt?"), say("a4", "m4", "Two lines.", "end_turn"))
	if len(ts) != 3 || ts[1].From != "you" || ts[1].Said != "! ls -la" || ts[1].End != "done" || ts[1].Reply != "" || len(ts[1].Steps) != 0 {
		t.Fatalf("%+v", ts)
	}
	if ts[2].Said != "what is in a.txt?" || ts[2].Reply != "Two lines." {
		t.Fatalf("%+v", ts[2])
	}
	for _, x := range ts {
		if strings.Contains(x.Said, "b.txt") || strings.Contains(x.Reply, "b.txt") {
			t.Fatalf("the command's output is in the chat: %+v", x)
		}
	}
	// The command is the last thing so far: it is done, not working.
	ts = turns(t, prompt("u1", "hi"), say("a1", "m1", "hello", "end_turn"),
		`{"type":"user","uuid":"u2","timestamp":"2026-01-01T12:01:00.000Z","message":{"role":"user","content":"<bash-input>make</bash-input>"}}`)
	if len(ts) != 2 || ts[1].Said != "! make" || ts[1].End != "done" {
		t.Fatalf("%+v", ts)
	}
}

// A background task's notice that came while the user typed is written only to the record (queueTranscriptOnly), with
// no turn of its own, before the user's prompt. It shows as from another program, by its summary.
func TestNoticeWrittenOnlyToTheRecord(t *testing.T) {
	ts := turns(t, prompt("u1", "hi"), say("a1", "m1", "hello", "end_turn"),
		`{"type":"user","uuid":"u2","timestamp":"2026-01-01T12:02:00.000Z","origin":{"kind":"task-notification"},"promptSource":"system","queueTranscriptOnly":true,"queueSkipAttachments":true,"message":{"role":"user","content":"<task-notification>\n<task-id>b7</task-id>\n<tool-use-id>toolu_9</tool-use-id>\n<status>completed</status>\n<summary>Background command \"make test\" completed</summary>\n<note>see the output file</note>\n</task-notification>"}}`,
		prompt("u3", "and now?"), say("a3", "m3", "All green.", "end_turn"))
	if len(ts) != 3 || ts[1].From != "other" || ts[1].Said != `Background command "make test" completed` || ts[1].End != "done" || ts[1].Reply != "" {
		t.Fatalf("%+v", ts)
	}
	if ts[2].From != "you" || ts[2].Reply != "All green." {
		t.Fatalf("%+v", ts[2])
	}
}

func think(id, msg, text string) string {
	return fmt.Sprintf(`{"type":"assistant","uuid":%q,"timestamp":"2026-01-01T12:00:05.000Z","cwd":"/work/acme","message":{"id":%q,"role":"assistant","content":[{"type":"thinking","thinking":%q,"signature":"c2ln"}],"stop_reason":null}}`, id, msg, text)
}

const png1 = "iVBORw0KGgo=" // the first bytes of a PNG, enough to tell a decoded image

func TestImages(t *testing.T) {
	p := write(t, `{"type":"user","uuid":"u1","turnOrigin":"human","turnPosition":{"promptIndex":0},"message":{"role":"user","content":[{"type":"text","text":"what is this"},{"type":"image","source":{"type":"base64","media_type":"image/png","data":"`+png1+`"}}]}}`,
		use("a1", "m1", "toolu_1", "Read", `{"file_path":"/work/acme/shot.jpg"}`),
		`{"type":"user","uuid":"u2","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":[{"type":"image","source":{"type":"base64","media_type":"image/jpeg","data":"/9j/4AA="}}]}]}}`,
		say("a2", "m2", "A cat.", "end_turn"))
	r := &Reader{}
	ts, err := r.Turns(p)
	if err != nil {
		t.Fatal(err)
	}
	if ts[0].Said != "what is this" || len(ts[0].Images) != 1 {
		t.Fatalf("said %q images %v", ts[0].Said, ts[0].Images)
	}
	media, data, err := r.Image(p, ts[0].Images[0].ID)
	if err != nil || media != "image/png" || string(data[1:4]) != "PNG" {
		t.Fatalf("prompt image: %q %q %v", media, data, err)
	}
	d, err := r.Detail(p, "toolu_1")
	if err != nil || len(d.Images) != 1 {
		t.Fatalf("detail %+v %v", d, err)
	}
	media, data, err = r.Image(p, d.Images[0].ID)
	if err != nil || media != "image/jpeg" || data[0] != 0xff {
		t.Fatalf("result image: %q %v", media, err)
	}
	for _, id := range []string{"u1.0", "u1.9", "nope", "toolu_1.5", ""} {
		if _, _, err := r.Image(p, id); !errors.Is(err, chat.ErrNoStep) {
			t.Fatalf("%q: want ErrNoStep, got %v", id, err)
		}
	}
}

// Each image comes with where it came from, when the record says: a prompt's from the "[Image: source: …]" line Claude
// Code writes after it, a tool result's from the file_path the tool read. The phone shows it above the image.
func TestImagesSayWhereTheyCameFrom(t *testing.T) {
	img := `{"type":"image","source":{"type":"base64","media_type":"image/png","data":"` + png1 + `"}}`
	p := write(t, `{"type":"user","uuid":"u1","turnOrigin":"human","turnPosition":{"promptIndex":0},"message":{"role":"user","content":[{"type":"text","text":"compare"},`+img+`,`+img+`]}}`,
		`{"type":"user","uuid":"m1","isMeta":true,"message":{"role":"user","content":[{"type":"text","text":"[Image: source: /tmp/claude/x/images/1.png]"},{"type":"text","text":"[Image: source: /tmp/claude/x/images/2.png]"}]}}`,
		use("a1", "m1", "toolu_1", "Read", `{"file_path":"/work/acme/shot.png"}`),
		`{"type":"user","uuid":"u2","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":[`+img+`]}]}}`,
		use("a2", "m2", "toolu_2", "mcp__browser__screenshot", `{}`),
		`{"type":"user","uuid":"u3","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_2","content":[`+img+`]}]}}`,
		`{"type":"user","uuid":"u4","turnOrigin":"human","turnPosition":{"promptIndex":1},"message":{"role":"user","content":[`+img+`]}}`,
		say("a3", "m3", "Done.", "end_turn"))
	r := &Reader{}
	ts, err := r.Turns(p)
	if err != nil {
		t.Fatal(err)
	}
	want := []chat.Image{{ID: "u1.1", Source: "/tmp/claude/x/images/1.png"}, {ID: "u1.2", Source: "/tmp/claude/x/images/2.png"}}
	if !reflect.DeepEqual(ts[0].Images, want) {
		t.Fatalf("prompt images %+v", ts[0].Images)
	}
	if !reflect.DeepEqual(ts[1].Images, []chat.Image{{ID: "u4.0"}}) {
		t.Fatalf("an image the record gives no source for: %+v", ts[1].Images)
	}
	d, _ := r.Detail(p, "toolu_1")
	if !reflect.DeepEqual(d.Images, []chat.Image{{ID: "toolu_1.0", Source: "/work/acme/shot.png"}}) {
		t.Fatalf("Read result %+v", d.Images)
	}
	d, _ = r.Detail(p, "toolu_2")
	if !reflect.DeepEqual(d.Images, []chat.Image{{ID: "toolu_2.0"}}) {
		t.Fatalf("a tool that read no file: %+v", d.Images)
	}
}

func TestImageOfAnUnknownTypeIsNotServed(t *testing.T) {
	p := write(t, `{"type":"user","uuid":"u1","turnOrigin":"human","turnPosition":{"promptIndex":0},"message":{"role":"user","content":[{"type":"image","source":{"type":"base64","media_type":"image/svg+xml","data":"PHN2Zz4="}}]}}`,
		say("a1", "m1", "A drawing.", "end_turn"))
	r := &Reader{}
	ts, _ := r.Turns(p)
	if len(ts[0].Images) != 0 || ts[0].Said != "[image]" {
		t.Fatalf("%+v", ts[0])
	}
	if _, _, err := r.Image(p, "u1.0"); !errors.Is(err, chat.ErrNoStep) {
		t.Fatalf("want ErrNoStep, got %v", err)
	}
}

func TestThinkingWithTextIsAStep(t *testing.T) {
	p := write(t, prompt("u1", "fix it"), think("a1", "m1", "The total counts twice.\nCheck orders.py first."), think("a2", "m1", ""),
		say("a3", "m1", "Fixed.", "end_turn"))
	r := &Reader{}
	ts, err := r.Turns(p)
	if err != nil {
		t.Fatal(err)
	}
	want := []chat.Step{{ID: "a1", Kind: "think", Title: "The total counts twice."}}
	if !reflect.DeepEqual(ts[0].Steps, want) || ts[0].Reply != "Fixed." {
		t.Fatalf("%+v", ts[0])
	}
	d, err := r.Detail(p, "a1")
	if err != nil || d.Input != "The total counts twice.\nCheck orders.py first." {
		t.Fatalf("%+v %v", d, err)
	}
}

func TestAPIErrorStopsTheTurn(t *testing.T) {
	ts := turns(t, prompt("u1", "hi"), `{"type":"assistant","uuid":"a1","isApiErrorMessage":true,"message":{"id":"e","role":"assistant","content":[{"type":"text","text":"API Error: 529 overloaded"}],"stop_reason":"stop_sequence"}}`)
	if ts[0].End != "stopped" || ts[0].Reply != "API Error: 529 overloaded" {
		t.Fatalf("%+v", ts[0])
	}
}

func TestUnknownFormats(t *testing.T) {
	cases := map[string][]string{
		"block": {prompt("u1", "hi"), `{"type":"assistant","uuid":"a1","message":{"id":"m","role":"assistant","content":[{"type":"hologram","x":1}],"stop_reason":"end_turn"}}`},
		"type":  {prompt("u1", "hi"), `{"type":"assistant_v2","uuid":"a1","message":{"role":"assistant","content":"hello"}}`},
		"json":  {prompt("u1", "hi"), `{"type":"assistant",`, say("a1", "m1", "hello", "end_turn")},
		"text":  {prompt("u1", "hi"), `{"type":"user","uuid":"u2","message":{"role":"user","content":"<voice-input>ls</voice-input>"}}`},
		"tool":  {prompt("u1", "hi"), `{"type":"assistant","uuid":"a1","message":{"id":"m","role":"assistant","content":[{"type":"tool_use","input":{}}],"stop_reason":"tool_use"}}`},
		"stop":  {prompt("u1", "hi"), say("a1", "m1", "hello", "pause_turn")},
	}
	for name, lines := range cases {
		t.Run(name, func(t *testing.T) {
			if f := formatError(t, lines...); f == "" {
				t.Fatal("no field named")
			}
		})
	}
}

// A line being written (no newline yet) is not read; a broken line before the last is a format error (above).
func TestHalfWrittenLastLine(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.jsonl")
	os.WriteFile(p, []byte(prompt("u1", "hi")+"\n"+say("a1", "m1", "hello", "end_turn")+"\n"+`{"type":"user","uuid":"u2","mess`), 0o644)
	ts, err := (&Reader{}).Turns(p)
	if err != nil || len(ts) != 1 || ts[0].End != "done" {
		t.Fatalf("%+v %v", ts, err)
	}
}

// Only the file's end is read; a turn cut by that window is dropped, not shown in part.
func TestReadsOnlyTheEnd(t *testing.T) {
	big := strings.Repeat("x", 200<<10)
	var lines []string
	for i := 0; i < 40; i++ {
		lines = append(lines, prompt(fmt.Sprintf("u%d", i), fmt.Sprintf("prompt %d", i)), result(fmt.Sprintf("r%d", i), "toolu_none", big), say(fmt.Sprintf("a%d", i), fmt.Sprintf("m%d", i), fmt.Sprintf("reply %d", i), "end_turn"))
	}
	ts := turns(t, lines...)
	if len(ts) == 0 || len(ts) > chat.MaxTurns {
		t.Fatalf("%d turns", len(ts))
	}
	if ts[len(ts)-1].Reply != "reply 39" {
		t.Fatalf("last: %+v", ts[len(ts)-1])
	}
	first := ts[0]
	if !strings.HasPrefix(first.Said, "prompt ") || first.Reply != strings.Replace(first.Said, "prompt", "reply", 1) {
		t.Fatalf("first turn is partial: %+v", first)
	}
}

func TestLastThirtyTurns(t *testing.T) {
	var lines []string
	for i := 0; i < 45; i++ {
		lines = append(lines, prompt(fmt.Sprintf("u%d", i), fmt.Sprintf("p%d", i)), say(fmt.Sprintf("a%d", i), fmt.Sprintf("m%d", i), "r", "end_turn"))
	}
	ts := turns(t, lines...)
	if len(ts) != chat.MaxTurns || ts[0].Said != "p15" {
		t.Fatalf("%d turns, first %q", len(ts), ts[0].Said)
	}
}

func TestDetail(t *testing.T) {
	p := write(t, prompt("u1", "go"), say("a0", "m0", "First I look.", "tool_use"), use("a1", "m1", "toolu_1", "Bash", `{"command":"go test ./...","description":"Run the tests"}`),
		result("u2", "toolu_1", strings.Repeat("ok\n", 3000)), say("a2", "m2", "Done.", "end_turn"))
	r := &Reader{}
	d, err := r.Detail(p, "toolu_1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(d.Input, "go test ./...") || len(d.Output) > chat.MaxDetail+len("…") || !strings.HasPrefix(d.Output, "ok\nok") {
		t.Fatalf("%+v", d)
	}
	ts, _ := r.Turns(p)
	n, err := r.Detail(p, ts[0].Steps[0].ID)
	if err != nil || n.Input != "First I look." {
		t.Fatalf("note detail %+v %v", n, err)
	}
	if _, err := r.Detail(p, "toolu_nope"); !errors.Is(err, chat.ErrNoStep) {
		t.Fatalf("want ErrNoStep, got %v", err)
	}
}

// --- which file belongs to a process ------------------------------------------------------------------------

func psStart(t *testing.T, pid int) string {
	cmd := exec.Command("ps", "-o", "lstart=", "-p", fmt.Sprint(pid))
	cmd.Env = append(os.Environ(), "TZ=UTC")
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

func pidFile(t *testing.T, dir string, pid int, fields map[string]any) {
	os.MkdirAll(filepath.Join(dir, "sessions"), 0o755)
	b, _ := json.Marshal(fields)
	os.WriteFile(filepath.Join(dir, "sessions", fmt.Sprintf("%d.json", pid)), b, 0o644)
}

func transcript(t *testing.T, dir, project, id string) string {
	p := filepath.Join(dir, "projects", project, id+".jsonl")
	os.MkdirAll(filepath.Dir(p), 0o755)
	os.WriteFile(p, []byte(prompt("u1", "hi")+"\n"), 0o644)
	return p
}

const sid = "0ff888dd-6fdf-48a6-a0c3-f255c5328133"

func miss(t *testing.T, err error) string {
	t.Helper()
	var m *chat.Miss
	if !errors.As(err, &m) {
		t.Fatalf("want a miss, got %v", err)
	}
	return m.Reason
}

// proc starts a process with only these environment variables, as a terminal of a herdr-core started with them. It
// is this test binary (TestHelperProcess): ps shows no environment of a system program such as /bin/sleep.
func proc(t *testing.T, env ...string) int {
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperProcess$")
	cmd.Env = append([]string{"PATH=/usr/bin:/bin", "CHAT_HELPER=1"}, env...)
	out, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	bufio.NewReader(out).ReadString('\n') // running
	return cmd.Process.Pid
}

func TestHelperProcess(t *testing.T) {
	if os.Getenv("CHAT_HELPER") != "1" {
		t.Skip("a process for TestFind")
	}
	fmt.Println("running")
	time.Sleep(60 * time.Second)
}

func TestFind(t *testing.T) {
	if runtime.GOOS == "windows" {
		// Find checks the pid file's procStart against ps's start time; Windows has no ps, so there Find finds no
		// session (how Claude Code writes procStart on Windows is not measured).
		t.Skip("the start time comes from ps, which Windows lacks")
	}
	pid := proc(t)
	good := map[string]any{"pid": pid, "sessionId": sid, "procStart": psStart(t, pid), "version": "2.1.319", "kind": "interactive"}
	with := func(k string, v any) map[string]any {
		m := map[string]any{}
		for a, b := range good {
			m[a] = b
		}
		m[k] = v
		return m
	}
	t.Run("sure", func(t *testing.T) {
		dir := t.TempDir()
		pidFile(t, dir, pid, good)
		want := transcript(t, dir, "-work-acme", sid)
		got, v, err := (&Reader{Dir: dir}).Find(pid)
		if err != nil || got != want || v != "2.1.319" {
			t.Fatalf("%q %q %v", got, v, err)
		}
	})
	// Claude Code rewrites its pid file in place: a shorter write leaves the old end after it (a stray "}", seen on
	// 3 of 27 running sessions of 2.1.319 and 2.1.322). The first object is the file; pid and start time still decide.
	t.Run("old end after the object", func(t *testing.T) {
		dir := t.TempDir()
		pidFile(t, dir, pid, good)
		f := filepath.Join(dir, "sessions", fmt.Sprintf("%d.json", pid))
		b, _ := os.ReadFile(f)
		os.WriteFile(f, append(b, '}'), 0o644)
		want := transcript(t, dir, "-work-acme", sid)
		if got, _, err := (&Reader{Dir: dir}).Find(pid); err != nil || got != want {
			t.Fatalf("%q %v", got, err)
		}
		// Still another process's file if its pid or start time says so.
		b, _ = json.Marshal(with("procStart", "Mon Jan  1 00:00:00 2024"))
		os.WriteFile(f, append(b, '}'), 0o644)
		if _, _, err := (&Reader{Dir: dir}).Find(pid); miss(t, err) != chat.NoMatch {
			t.Fatalf("%v", err)
		}
	})
	cases := []struct {
		name   string
		fields map[string]any
		files  []string
		reason string
	}{
		{"no pid file", nil, []string{"-work-acme"}, chat.NoMatch},
		{"another pid inside", with("pid", pid+1), []string{"-work-acme"}, chat.NoMatch},
		{"pid used again by another process", with("procStart", "Mon Jan  1 00:00:00 2024"), []string{"-work-acme"}, chat.NoMatch},
		{"no transcript yet", good, nil, chat.NoRecordYet},
		{"two transcripts", good, []string{"-work-acme", "-work-other"}, chat.Ambiguous},
		{"not a session id", with("sessionId", "../*"), []string{"-work-acme"}, chat.NoMatch},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			if c.fields != nil {
				pidFile(t, dir, pid, c.fields)
			}
			for _, p := range c.files {
				transcript(t, dir, p, sid)
			}
			_, _, err := (&Reader{Dir: dir}).Find(pid)
			if got := miss(t, err); got != c.reason {
				t.Fatalf("reason %q, want %q", got, c.reason)
			}
		})
	}
	// A Claude Code started where another Claude Code runs (a herdr-core first started from inside one passes its
	// environment to every terminal) saves no transcript (seen with 2.1.319): none will come.
	child := proc(t, "CLAUDE_CODE_CHILD_SESSION=1")
	childFile := with("pid", child)
	childFile["procStart"] = psStart(t, child)
	for _, c := range []struct {
		name   string
		fields map[string]any
	}{{"record off, no pid file", nil}, {"record off, a pid file", childFile}} {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			if c.fields != nil {
				pidFile(t, dir, child, c.fields)
			}
			_, _, err := (&Reader{Dir: dir}).Find(child)
			if got := miss(t, err); got != chat.NoRecord {
				t.Fatalf("reason %q, want %q", got, chat.NoRecord)
			}
		})
	}
	t.Run("a process that has gone", func(t *testing.T) {
		dir := t.TempDir()
		cmd := exec.Command("true")
		cmd.Run()
		gone := cmd.Process.Pid
		pidFile(t, dir, gone, with("pid", gone))
		transcript(t, dir, "-work-acme", sid)
		_, _, err := (&Reader{Dir: dir}).Find(gone)
		if got := miss(t, err); got != chat.NoMatch {
			t.Fatalf("reason %q", got)
		}
	})
}

// The view asks again every 3 s while the agent works: a file that has not changed is not read again.
func TestUnchangedFileIsNotReadAgain(t *testing.T) {
	p := write(t, prompt("u1", "go"), say("a1", "m1", "Done.", "end_turn"))
	r := &Reader{}
	for i := 0; i < 3; i++ {
		if _, err := r.Turns(p); err != nil {
			t.Fatal(err)
		}
	}
	if r.parses != 1 {
		t.Fatalf("parsed %d times", r.parses)
	}
	f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString(prompt("u2", "again") + "\n")
	f.Close()
	ts, _ := r.Turns(p)
	if r.parses != 2 || len(ts) != 2 {
		t.Fatalf("after growing: parsed %d times, %d turns", r.parses, len(ts))
	}
	if runtime.GOOS == "windows" {
		return // Windows' FileInfo has no file number (chat.Inode): a file there is told by its size and time alone.
	}
	// Another file in its place (a new inode), even of the same size and time, is read again.
	st, _ := os.Stat(p)
	other := filepath.Join(filepath.Dir(p), "other")
	b, _ := os.ReadFile(p)
	os.WriteFile(other, []byte(strings.Replace(string(b), "again", "AGAIN", 1)), 0o644)
	os.Chtimes(other, st.ModTime(), st.ModTime())
	os.Rename(other, p)
	ts, _ = r.Turns(p)
	if r.parses != 3 || ts[1].Said != "AGAIN" {
		t.Fatalf("after replacing: parsed %d times, %q", r.parses, ts[1].Said)
	}
}

// A step's title is one short line: a command, not its output; nothing of a tool's result is in the turns.
func TestStepTitlesAreOneShortLine(t *testing.T) {
	long := strings.Repeat("x", 200)
	ts := turns(t, prompt("u1", "go"),
		use("a1", "m1", "toolu_1", "Bash", `{"command":"go test ./...\necho done","description":"Run the tests"}`),
		result("u2", "toolu_1", "SECRET OUTPUT"),
		use("a2", "m2", "toolu_2", "Grep", `{"pattern":"`+long+`"}`),
		say("a3", "m3", long+"\nmore", "tool_use"), use("a4", "m4", "toolu_3", "Bash", `{"command":"ls"}`),
		say("a5", "m5", "Done.", "end_turn"))
	var titles []string
	for _, s := range ts[0].Steps {
		titles = append(titles, s.Title)
		if len(s.Title) > 80+len("…") || strings.Contains(s.Title, "\n") {
			t.Fatalf("title %q", s.Title)
		}
	}
	if titles[0] != "go test ./..." {
		t.Fatalf("run title %q", titles[0])
	}
	b, _ := json.Marshal(ts)
	if strings.Contains(string(b), "SECRET OUTPUT") {
		t.Fatal("a tool result is in the turns")
	}
	if ts[0].At != "2026-01-01T12:00:00.000Z" {
		t.Fatalf("at %q", ts[0].At)
	}
}

// Each step of a fixture with a .details.want.json, as the phone shows it: what a read read, what an edit changed,
// what a command printed and whether it failed. Only those parts are compared; the raw input and output are the
// fallback and stay as they are.
func TestFixtureDetails(t *testing.T) {
	wants, _ := filepath.Glob(filepath.Join("testdata", "claude-code", "*", "*.details.want.json"))
	if len(wants) == 0 {
		t.Fatal("no .details.want.json fixtures")
	}
	for _, want := range wants {
		t.Run(want, func(t *testing.T) {
			p := strings.TrimSuffix(want, ".details.want.json") + ".jsonl"
			b, err := os.ReadFile(want)
			if err != nil {
				t.Fatal(err)
			}
			var w []chat.Detail
			if err := json.Unmarshal(b, &w); err != nil {
				t.Fatalf("%s: %v", want, err)
			}
			r := &Reader{}
			ts, err := r.Turns(p)
			if err != nil {
				t.Fatal(err)
			}
			var got []chat.Detail
			for _, tn := range ts {
				for _, s := range tn.Steps {
					d, err := r.Detail(p, s.ID)
					if err != nil {
						t.Fatalf("step %s: %v", s.Title, err)
					}
					got = append(got, chat.Detail{Run: d.Run, Change: d.Change, Read: d.Read})
				}
			}
			if !reflect.DeepEqual(got, w) {
				g, _ := json.MarshalIndent(got, "", "  ")
				t.Fatalf("details differ from %s; got\n%s", want, g)
			}
		})
	}
}

func TestRunOutputKeepsItsEnd(t *testing.T) {
	long := strings.Repeat("line\n", 2000) + "the last line"
	r, _, _ := shaped("Bash", json.RawMessage(`{"command":"make"}`), nil, true, "Exit code 2\n"+long)
	if !r.Failed || r.Code != 2 || r.Lines != 2001 || !strings.HasSuffix(r.Output, "the last line") || !strings.HasPrefix(r.Output, "…") || len(r.Output) > chat.MaxDetail+len("…") {
		t.Fatalf("got failed %v code %d lines %d, output %d bytes ending %q", r.Failed, r.Code, r.Lines, len(r.Output), r.Output[len(r.Output)-20:])
	}
	// Refused before it ran: failed, no code, the reason as its output.
	r, _, _ = shaped("Bash", json.RawMessage(`{"command":"rm -rf x"}`), nil, true, "Permission to use Bash has been denied.")
	if !r.Failed || r.Code != 0 || r.Output != "Permission to use Bash has been denied." {
		t.Fatalf("got %+v", r)
	}
}

func TestChangeStopsAtMaxLines(t *testing.T) {
	var lines []string
	for i := 0; i < chat.MaxChangeLines+50; i++ {
		lines = append(lines, "+x")
	}
	res, _ := json.Marshal(map[string]any{"filePath": "/w/a.txt", "structuredPatch": []any{map[string]any{"oldStart": 1, "newStart": 1, "lines": lines}}})
	_, c, _ := shaped("Edit", nil, res, false, "")
	if !c.Cut || c.Added != chat.MaxChangeLines+50 || len(c.Hunks[0].Lines) != chat.MaxChangeLines {
		t.Fatalf("cut %v added %d shown %d", c.Cut, c.Added, len(c.Hunks[0].Lines))
	}
	// A failed edit changed nothing: no change shown.
	if _, c, _ := shaped("Edit", nil, res, true, "String not found"); c != nil {
		t.Fatalf("a failed edit shows a change: %+v", c)
	}
}
