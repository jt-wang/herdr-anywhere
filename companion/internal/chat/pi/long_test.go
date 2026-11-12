package pi

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"herdr-anywhere/companion/internal/chat"
)

// A made-up conversation file in the shapes of the recorded ones (testdata/pi/1.4.0): each entry's parent is the one
// before it.
type conv struct {
	ls   []string
	last string
}

func (c *conv) add(id string, m map[string]any) {
	var parent any
	if c.last != "" {
		parent = c.last
	}
	b, _ := json.Marshal(map[string]any{"type": "message", "id": id, "parentId": parent, "timestamp": "2026-01-01T12:00:00.000Z", "message": m})
	c.ls = append(c.ls, string(b))
	c.last = id
}
func (c *conv) user(id, text string) {
	c.add(id, map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": text}}, "timestamp": 1767268800000})
}
func (c *conv) call(id, tool, command string) {
	c.add(id, map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "toolCall", "id": tool, "name": "bash", "arguments": map[string]any{"command": command}}}, "stopReason": "toolUse", "timestamp": 1767268801000})
}
func (c *conv) result(id, tool, out string) {
	c.add(id, map[string]any{"role": "toolResult", "toolCallId": tool, "toolName": "bash", "content": []any{map[string]any{"type": "text", "text": out}}, "isError": false, "timestamp": 1767268802000})
}
func (c *conv) answer(id, text string) {
	c.add(id, map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "text", "text": text}}, "stopReason": "stop", "timestamp": 1767268803000})
}
func (c *conv) write(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "s.jsonl")
	ls := append([]string{`{"type":"session","version":3,"id":"s1","timestamp":"2026-01-01T12:00:00.000Z","cwd":"/work/acme"}`}, c.ls...)
	if err := os.WriteFile(p, []byte(strings.Join(ls, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// A turn running commands that print about 1 MB each, n of them; still running. The outputs quote a person's message,
// as a command reading a conversation file would print it.
func longTurn(n int) *conv {
	out := strings.Repeat(`{"role":"user"} `, 1<<20/16)
	c := &conv{}
	c.user("u0", "an earlier question")
	c.answer("a0", "an earlier answer")
	c.user("u1", "build the docs site")
	for i := 0; i < n; i++ {
		c.call(fmt.Sprintf("c%d", i), fmt.Sprintf("call_%d", i), fmt.Sprintf("make page-%d", i))
		c.result(fmt.Sprintf("r%d", i), fmt.Sprintf("call_%d", i), out)
		if i == 1 { // a tool's details naming a person's message: not where the turn began
			c.add("d1", map[string]any{"role": "toolResult", "toolCallId": "call_1", "toolName": "bash", "content": []any{}, "details": map[string]any{"role": "user"}, "timestamp": 1767268802000})
		}
	}
	c.call("c-last", "call_last", "make index")
	return c
}

func read(t *testing.T, p string) []chat.Turn {
	t.Helper()
	ts, err := (&Reader{}).Turns(p)
	if err != nil {
		t.Fatal(err)
	}
	return ts
}

// The person's message is further back than MaxTail: the turn is read from it, open, with its steps.
func TestTurnLongerThanTheTail(t *testing.T) {
	p := longTurn(6).write(t)
	ts := read(t, p)
	if len(ts) == 0 {
		t.Fatal("no turns")
	}
	last := ts[len(ts)-1]
	if last.Said != "build the docs site" || last.End != "open" || len(last.Steps) != 7 {
		t.Fatalf("last turn %q, %s, %d steps", last.Said, last.End, len(last.Steps))
	}
	if d, err := (&Reader{}).Detail(p, "call_0"); err != nil || !strings.Contains(d.Input, "make page-0") {
		t.Fatalf("detail of the first step: %+v %v", d, err)
	}
}

// One command output over 4 MB: read whole, no format error.
func TestALineOverMaxTail(t *testing.T) {
	c := &conv{}
	c.user("u1", "print the log")
	c.call("c1", "call_1", "cat big.log")
	c.result("r1", "call_1", strings.Repeat("y", 5<<20))
	c.answer("a1", "It is long.")
	if ts := read(t, c.write(t)); len(ts) != 1 || ts[0].Said != "print the log" || ts[0].End != "done" || ts[0].Reply != "It is long." {
		t.Fatalf("%+v", ts)
	}
}

// The person's message is further back than MaxBack: the turn shows from where the reading starts, open, without it.
func TestMessageBeyondMaxBack(t *testing.T) {
	c := longTurn(chat.MaxBack>>20 + 2)
	ts := read(t, c.write(t))
	if len(ts) != 1 || ts[0].Said != "" || ts[0].End != "open" || len(ts[0].Steps) == 0 {
		t.Fatalf("%d turns: %+v", len(ts), ts)
	}
	c.answer("a-end", "The site is built.")
	if ts = read(t, c.write(t)); len(ts) != 1 || ts[0].End != "done" || ts[0].Reply != "The site is built." {
		t.Fatalf("%+v", ts)
	}
	c.user("u9", "now the index")
	if ts = read(t, c.write(t)); len(ts) != 1 || ts[0].Said != "now the index" || ts[0].End != "open" {
		t.Fatalf("%+v", ts)
	}
}
