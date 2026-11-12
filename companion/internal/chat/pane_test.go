package chat

import (
	"context"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type paneReader struct {
	fakeReader
	panes []Pane
}

func (p *paneReader) FindPane(pane Pane) (string, string, error) {
	p.panes = append(p.panes, pane)
	return p.path, p.version, p.findErr
}

func (p *paneReader) ReportsSession() {}

// labelReader finds its session from the pane's labels (Pi): no reports to follow.
type labelReader struct{ fakeReader }

func (l *labelReader) FindPane(Pane) (string, string, error) { return l.path, l.version, l.findErr }

const ocWorking = `{"address":"t1","cwd":"/work/acme","title":"OC | Hello","status":{"app":"opencode","state":"working"},` +
	`"records":{"ses_486922732ffe1Oe1UHic1xuAFb":{"app":"opencode","state":"working"}},"foreground":{"pid":4242,"cwd":"/work/acme"}}`
const ocIdle = `{"address":"t1","cwd":"/work/acme","title":"OC | Hello","status":{"app":"opencode","state":"idle"},` +
	`"records":{},"foreground":{"pid":4242,"cwd":"/work/acme"}}`

// The status events OpenCode 2.2.0's reports made over one turn, as hc events sent them (replayed in an isolated
// herdr-core): the session id is in records only while it works.
var ocTurn = strings.Join([]string{
	`{"channel":null,"seq":0,"terminals":[]}`,
	`{"jsonrpc":"2.0","method":"event","params":{"data":{"records":{},"reported":true,"status":{"app":"opencode","state":"working","title":"Hello"}},"seq":4,"terminal":"t1","type":"status"}}`,
	`{"jsonrpc":"2.0","method":"event","params":{"data":{"records":{"ses_486922732ffe1Oe1UHic1xuAFb":{"app":"opencode","state":"working","title":"Hello"}},"reported":true,"status":{"app":"opencode","state":"working","title":"Hello"}},"seq":5,"terminal":"t1","type":"status"}}`,
	`{"jsonrpc":"2.0","method":"event","params":{"data":{"records":{},"reported":true,"status":{"app":"opencode","state":"idle"}},"seq":7,"terminal":"t1","type":"status"}}`,
	`{"jsonrpc":"2.0","method":"event","params":{"data":{"state":"idle","turn":1},"seq":8,"terminal":"t1","type":"turn"}}`,
}, "\n")

func paneService(get string, r Reader) *Service {
	s, _, _ := service(get, r)
	s.Readers = map[string]Reader{"opencode": r}
	return s
}

func TestPaneReaderGetsWhatHerdrCoreSays(t *testing.T) {
	r := &paneReader{fakeReader: fakeReader{path: "/db#ses", turns: openTurn}}
	paneService(ocWorking, r).Chat("t1")
	want := []Pane{{Pid: 4242, Cwd: "/work/acme", Title: "OC | Hello", Sessions: []string{"ses_486922732ffe1Oe1UHic1xuAFb"}}}
	if !reflect.DeepEqual(r.panes, want) || len(r.finds) != 0 {
		t.Fatalf("panes %+v, finds %v", r.panes, r.finds)
	}
}

// Once the turn is over herdr-core has no session id; the one seen in the turn is kept for the same process.
func TestSessionRememberedAfterTheTurn(t *testing.T) {
	r := &paneReader{fakeReader: fakeReader{path: "/db#ses", turns: doneTurn}}
	s := paneService(ocIdle, r)
	s.Chat("t1")
	if got := r.panes[0].Sessions; len(got) != 0 {
		t.Fatalf("before any event: %v", got)
	}
	s.follow(strings.NewReader(ocTurn))
	s.Chat("t1")
	if got := r.panes[1].Sessions; !reflect.DeepEqual(got, []string{"ses_486922732ffe1Oe1UHic1xuAFb"}) {
		t.Fatalf("after the turn: %v", got)
	}
	// OpenCode started again in the same pane: another process, so not its session.
	s.get = func(string) ([]byte, error) {
		return []byte(strings.Replace(ocIdle, `"pid":4242`, `"pid":5151`, 1)), nil
	}
	s.Chat("t1")
	if got := r.panes[2].Sessions; len(got) != 0 {
		t.Fatalf("another process: %v", got)
	}
}

// Following `hc events` starts with the first chat of a pane whose reader needs it, not with serve: a Companion
// whose phone never opens such a chat asks herdr-core nothing more than before.
func TestFollowStartsWithTheFirstPaneChat(t *testing.T) {
	r := &paneReader{fakeReader: fakeReader{path: "/db#ses", turns: openTurn}}
	cc := &fakeReader{path: "/s.jsonl", version: "2.1.0", turns: openTurn}
	s := paneService(ocWorking, r)
	s.Readers["claude-code"] = cc
	var started atomic.Int32
	s.Ctx, s.followFn = context.Background(), func(context.Context) { started.Add(1) }
	s.get = func(string) ([]byte, error) {
		return []byte(`{"address":"t2","status":{"app":"claude-code","state":"idle"},"foreground":{"pid":7}}`), nil
	}
	s.Chat("t2")
	s.Readers["pi"] = &labelReader{fakeReader: fakeReader{path: "/s.jsonl", turns: openTurn}}
	s.get = func(string) ([]byte, error) {
		return []byte(`{"address":"t3","status":{"app":"pi","state":"idle"},"foreground":{"pid":8}}`), nil
	}
	s.Chat("t3")
	time.Sleep(50 * time.Millisecond)
	if started.Load() != 0 {
		t.Fatalf("followed after a Claude Code or Pi chat")
	}
	s.get = func(string) ([]byte, error) { return []byte(ocWorking), nil }
	s.Chat("t1")
	s.Chat("t1")
	time.Sleep(50 * time.Millisecond)
	if n := started.Load(); n != 1 {
		t.Fatalf("started %d times after two OpenCode chats", n)
	}
}

// An answer that a Setup's file is missing names the agent, so the phone can offer that agent's setup.
func TestAnswerNamesTheSetup(t *testing.T) {
	r := &labelReader{fakeReader: fakeReader{findErr: &Miss{Reason: SetupNeeded}}}
	s := paneService(`{"address":"t3","status":{"app":"pi","state":"idle"},"foreground":{"pid":8}}`, r)
	s.Readers = map[string]Reader{"pi": r}
	if a := s.Chat("t3"); a.Unknown != SetupNeeded || a.Setup != "pi" {
		t.Fatalf("%+v", a)
	}
	r.findErr = &Miss{Reason: NoRecordYet}
	if a := s.Chat("t3"); a.Setup != "" {
		t.Fatalf("named a setup for another reason: %+v", a)
	}
}

// A database the reader doesn't know (another OpenCode's tables) is a format it can't read yet, not a missing session.
func TestFindPaneFormatIsFormat(t *testing.T) {
	r := &labelReader{fakeReader: fakeReader{findErr: Formatf("session: no such table")}}
	s := paneService(`{"address":"t3","status":{"app":"opencode","state":"idle"},"foreground":{"pid":8}}`, r)
	s.Readers = map[string]Reader{"opencode": r}
	if a := s.Chat("t3"); a.Unknown != Format {
		t.Fatalf("%+v", a)
	}
}

func TestLabeledOnlyFromTheProcessRunning(t *testing.T) {
	p := Pane{Pid: 7, Labels: map[string]string{LabelPid: "7", LabelSession: "/s.jsonl"}}
	if f, ok := p.Labeled(); !ok || f != "/s.jsonl" {
		t.Fatalf("%q %v", f, ok)
	}
	p.Pid = 8
	if _, ok := p.Labeled(); ok {
		t.Fatalf("another process")
	}
}
