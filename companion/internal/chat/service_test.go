package chat

import (
	"bytes"
	"errors"
	"io"
	"log"
	"reflect"
	"strings"
	"testing"
	"time"

	"herdr-anywhere/companion/internal/testfake"
)

type fakeReader struct {
	path, version string
	findErr       error
	turns         []Turn
	turnsErr      error
	detail        Detail
	finds         []int
}

func (f *fakeReader) Find(pid int) (string, string, error) {
	f.finds = append(f.finds, pid)
	return f.path, f.version, f.findErr
}
func (f *fakeReader) Turns(string) ([]Turn, error) { return f.turns, f.turnsErr }
func (f *fakeReader) Detail(_, id string) (Detail, error) {
	if id == "toolu_1" {
		return f.detail, nil
	}
	return Detail{}, ErrNoStep
}
func (f *fakeReader) Image(_, id string) (string, []byte, error) {
	if id == "u1.1" {
		return "image/png", []byte("\x89PNG"), nil
	}
	return "", nil, ErrNoStep
}

func service(get string, r Reader) (*Service, *bytes.Buffer, *[]string) {
	var logs bytes.Buffer
	var asked []string
	s := &Service{Readers: map[string]Reader{"claude-code": r}, Log: log.New(&logs, "", 0),
		get: func(addr string) ([]byte, error) {
			asked = append(asked, addr)
			if get == "" {
				return nil, errors.New("not_found")
			}
			return []byte(get), nil
		}}
	return s, &logs, &asked
}

const working = `{"address":"t1","state":"live","status":{"app":"claude-code","state":"working"},"foreground":{"pid":4242}}`
const done = `{"address":"t1","state":"live","status":{"app":"claude-code","state":"done"},"foreground":{"pid":4242}}`

var openTurn = []Turn{{ID: "u1", Said: "go", End: "open", Steps: []Step{}}}
var doneTurn = []Turn{{ID: "u1", Said: "go", End: "done", Reply: "ok", Steps: []Step{}}}

func TestChat(t *testing.T) {
	cases := []struct {
		name string
		get  string
		r    *fakeReader
		want Answer
	}{
		{"working and the record's last turn open", working, &fakeReader{path: "/s", turns: openTurn}, Answer{Chat: true, Turns: openTurn}},
		{"done and the last turn done", done, &fakeReader{path: "/s", turns: doneTurn}, Answer{Chat: true, Turns: doneTurn}},
		{"blocked on a question", strings.Replace(working, `"working"`, `"blocked"`, 1), &fakeReader{path: "/s", turns: openTurn}, Answer{Chat: true, Turns: openTurn}},
		{"idle with no turns yet", strings.Replace(done, `"done"`, `"idle"`, 1), &fakeReader{path: "/s", turns: []Turn{}}, Answer{Chat: true, Turns: []Turn{}}},
		// Behind: the screen and the record disagree; the phone still shows what the record has, marked.
		{"done but the record has not caught up", done, &fakeReader{path: "/s", turns: openTurn}, Answer{Chat: true, Unknown: Behind, Turns: openTurn}},
		{"working on a prompt the record doesn't have yet", working, &fakeReader{path: "/s", turns: doneTurn}, Answer{Chat: true, Unknown: Behind, Turns: doneTurn}},
		{"working and no turns in the record", working, &fakeReader{path: "/s", turns: []Turn{}}, Answer{Chat: true, Unknown: Behind, Turns: []Turn{}}},
		{"no sure match", working, &fakeReader{findErr: &Miss{Reason: NoMatch}}, Answer{Chat: true, Unknown: NoMatch}},
		{"a format the reader doesn't know", working, &fakeReader{path: "/s", version: "9.9.9", turnsErr: Formatf("content block %q", "hologram")}, Answer{Chat: true, Unknown: Format}},
		{"another agent", `{"status":{"app":"pi","state":"done"},"foreground":{"pid":1}}`, &fakeReader{}, Answer{}},
		{"a shell", `{"status":null,"foreground":{"pid":1}}`, &fakeReader{}, Answer{}},
		{"hc doesn't know the terminal", "", &fakeReader{}, Answer{}},
		{"no foreground program", `{"status":{"app":"claude-code","state":"done"},"foreground":null}`, &fakeReader{}, Answer{Chat: true, Unknown: NoTerminal}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, _, _ := service(c.get, c.r)
			if got := s.Chat("t1"); !reflect.DeepEqual(got, c.want) {
				t.Fatalf("got %+v\nwant %+v", got, c.want)
			}
		})
	}
}

func TestPidComesFromHC(t *testing.T) {
	r := &fakeReader{path: "/s", turns: openTurn}
	s, _, _ := service(working, r)
	s.Chat("t1")
	if !reflect.DeepEqual(r.finds, []int{4242}) {
		t.Fatalf("%v", r.finds)
	}
}

// A terminal on another machine is never looked up here: its pid means nothing on this computer.
func TestRemoteAndOddAddresses(t *testing.T) {
	for _, a := range []string{"box/t7", "-rf", "", "t1 t2", "t1\n"} {
		s, _, asked := service(working, &fakeReader{path: "/s", turns: openTurn})
		if got := s.Chat(a); got.Chat || len(*asked) != 0 {
			t.Fatalf("%q: %+v, asked hc %v", a, got, *asked)
		}
	}
}

func TestFormatIsLoggedOnceWithAgentVersionAndField(t *testing.T) {
	s, logs, _ := service(working, &fakeReader{path: "/s", version: "9.9.9", turnsErr: Formatf("content block %q", "hologram")})
	s.Chat("t1")
	s.Chat("t1")
	if n := strings.Count(logs.String(), "\n"); n != 1 {
		t.Fatalf("%d log lines: %s", n, logs)
	}
	for _, w := range []string{"claude-code", "9.9.9", `content block "hologram"`} {
		if !strings.Contains(logs.String(), w) {
			t.Fatalf("log %q lacks %q", logs, w)
		}
	}
}

func TestStep(t *testing.T) {
	r := &fakeReader{path: "/s", turns: openTurn, detail: Detail{Input: "ls", Output: "a"}}
	s, _, _ := service(working, r)
	d, err := s.Step("t1", "toolu_1")
	if err != nil || d.Input != "ls" {
		t.Fatalf("%+v %v", d, err)
	}
	if _, err := s.Step("t1", "toolu_2"); !errors.Is(err, ErrNoStep) {
		t.Fatalf("want ErrNoStep, got %v", err)
	}
	if _, err := s.Step("box/t7", "toolu_1"); !errors.Is(err, ErrNoStep) {
		t.Fatalf("remote: %v", err)
	}
}

func TestImage(t *testing.T) {
	s, _, _ := service(working, &fakeReader{path: "/s", turns: openTurn})
	media, data, err := s.Image("t1", "u1.1")
	if err != nil || media != "image/png" || string(data) != "\x89PNG" {
		t.Fatalf("%q %q %v", media, data, err)
	}
	if _, _, err := s.Image("t1", "u1.2"); !errors.Is(err, ErrNoStep) {
		t.Fatalf("want ErrNoStep, got %v", err)
	}
	if _, _, err := s.Image("box/t7", "u1.1"); !errors.Is(err, ErrNoStep) {
		t.Fatalf("remote: %v", err)
	}
}

// A permission prompt is only on the screen (7501's msg), not in the record (seen with Claude Code 2.1.319): the open turn
// shows it as its question. A question the agent asked itself (AskUserQuestion) stays as the record has it.
func TestBlockedOnAPermissionPrompt(t *testing.T) {
	blocked := `{"address":"t1","state":"live","status":{"app":"claude-code","state":"blocked","kind":"permission","msg":"approve Bash: date > stamp.txt"},"foreground":{"pid":4242}}`
	open := []Turn{{ID: "u1", Said: "stamp it", End: "open", Steps: []Step{{ID: "toolu_1", Kind: "run", Title: "date > stamp.txt"}}}}
	s, _, _ := service(blocked, &fakeReader{path: "/s", turns: open})
	a := s.Chat("t1")
	if len(a.Turns) != 1 || a.Turns[0].Ask == nil || a.Turns[0].Ask.Text != "approve Bash: date > stamp.txt" {
		t.Fatalf("%+v", a)
	}
	own := []Turn{{ID: "u1", Said: "pick", End: "open", Steps: []Step{}, Ask: &Ask{Text: "Which color?", Options: []string{"Red"}}}}
	s, _, _ = service(blocked, &fakeReader{path: "/s", turns: own})
	if a := s.Chat("t1"); a.Turns[0].Ask.Text != "Which color?" {
		t.Fatalf("%+v", a.Turns[0].Ask)
	}
	if open[0].Ask != nil {
		t.Fatal("the reader's turns were changed")
	}
}

// fakeNamed: a reader of a program that reports no status to herdr-core (as OpenCode 1): known by its process name.
type fakeNamed struct{ fakeReader }

func (*fakeNamed) ProgramName() string { return "opencode" }

// A program that reports no status (OpenCode 1, measured with 1.19.0: hc get has status null, foreground name
// "opencode") is known by its process name, and its record alone says whether its turn is open.
func TestChatWithoutStatus(t *testing.T) {
	quiet := `{"address":"t1","state":"live","status":null,"foreground":{"pid":4242,"name":"opencode"}}`
	for name, c := range map[string]struct {
		get  string
		r    *fakeNamed
		want Answer
	}{
		"done":       {quiet, &fakeNamed{fakeReader{path: "/s", turns: doneTurn}}, Answer{Chat: true, Turns: doneTurn}},
		"working":    {quiet, &fakeNamed{fakeReader{path: "/s", turns: openTurn}}, Answer{Chat: true, Turns: openTurn}},
		"not yet":    {quiet, &fakeNamed{fakeReader{findErr: &Miss{Reason: NotReported}}}, Answer{Chat: true, Unknown: NotReported}},
		"a shell":    {strings.Replace(quiet, `"opencode"`, `"zsh"`, 1), &fakeNamed{fakeReader{path: "/s", turns: doneTurn}}, Answer{}},
		"on Windows": {strings.Replace(quiet, `"opencode"`, `"opencode.exe"`, 1), &fakeNamed{fakeReader{path: "/s", turns: doneTurn}}, Answer{Chat: true, Turns: doneTurn}},
	} {
		t.Run(name, func(t *testing.T) {
			s, _, _ := service(c.get, &fakeReader{})
			s.Readers["opencode"] = c.r
			if got := s.Chat("t1"); !reflect.DeepEqual(got, c.want) {
				t.Fatalf("got %+v\nwant %+v", got, c.want)
			}
		})
	}
	// A reader that doesn't say it goes by the name: a pane with no status is still a shell.
	s, _, _ := service(quiet, &fakeReader{})
	s.Readers["opencode"] = &fakeReader{path: "/s", turns: doneTurn}
	if got := s.Chat("t1"); !reflect.DeepEqual(got, Answer{}) {
		t.Fatalf("unnamed reader: %+v", got)
	}
}

// An hc get that takes longer than the Service waits: hc can't say now what runs there, which is not "no chat here".
// The phone closes an open chat view on chat:false; on CI's iOS 27 simulator a busy computer did that (hc get over
// 5 s), so a slow hc is said as unsure, and the view stays.
func TestChatWhenHCIsSlow(t *testing.T) {
	dir := t.TempDir()
	hc := testfake.HC(t, dir, "hc", testfake.Spec{On: map[string][]testfake.Reply{"get": {{Sleep: "3s", Print: working}}}})
	s := &Service{HC: hc, hcTimeout: 300 * time.Millisecond, Readers: map[string]Reader{"claude-code": &fakeReader{path: "/s", turns: openTurn}}, Log: log.New(io.Discard, "", 0)}
	start := time.Now()
	got := s.Chat("t1")
	if took := time.Since(start); took > 2*time.Second {
		t.Fatalf("Chat waited %v for a slow hc get, not its 300 ms", took)
	}
	if want := (Answer{Chat: true, Unknown: NoTerminal}); !reflect.DeepEqual(got, want) {
		t.Fatalf("a slow hc get: got %+v, want %+v", got, want)
	}
	// Control: the same hc in time is a chat.
	s.hcTimeout = 10 * time.Second
	if got := s.Chat("t1"); !got.Chat || got.Unknown != "" || len(got.Turns) != 1 {
		t.Fatalf("hc get in time: got %+v", got)
	}
	// A terminal hc doesn't know stays "no chat here".
	gone := testfake.HC(t, dir, "hc2", testfake.Spec{On: map[string][]testfake.Reply{"get": {{Fail: "/nonexistent"}}}})
	s = &Service{HC: gone, Readers: map[string]Reader{"claude-code": &fakeReader{path: "/s", turns: openTurn}}, Log: log.New(io.Discard, "", 0)}
	if got := s.Chat("t1"); got.Chat {
		t.Fatalf("a terminal hc doesn't know: got %+v, want no chat", got)
	}
}
