package codex

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"herdr-anywhere/companion/internal/chat"
)

// Made-up record lines in the shapes of the recorded ones (testdata/codex/0.167.1).
func ev(turn string, payload map[string]any) string {
	payload["turn_id"] = turn
	b, _ := json.Marshal(map[string]any{"timestamp": "2026-01-01T12:00:00.000Z", "type": "event_msg", "payload": payload})
	return string(b)
}
func started(turn string) string { return ev(turn, map[string]any{"type": "task_started"}) }
func completed(turn string, item map[string]any) string {
	return ev(turn, map[string]any{"type": "item_completed", "item": item})
}
func said(turn, id, text string) string {
	return completed(turn, map[string]any{"type": "UserMessage", "id": id, "content": []any{map[string]any{"type": "text", "text": text}}})
}
func ran(turn, id, command, out string) string {
	return completed(turn, map[string]any{"type": "CommandExecution", "id": id, "command": []string{"/bin/zsh", "-lc", command}, "aggregated_output": out, "exit_code": 0, "status": "completed"})
}
func answered(turn, id, text string) string {
	return completed(turn, map[string]any{"type": "AgentMessage", "id": id, "content": []any{map[string]any{"type": "Text", "text": text}}})
}
func ended(turn string) string {
	return ev(turn, map[string]any{"type": "task_complete", "duration_ms": 1000})
}

func writeLines(t *testing.T, ls ...string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "rollout.jsonl")
	if err := os.WriteFile(p, []byte(strings.Join(ls, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// A turn running commands that print about 1 MB each, n of them; still running. The outputs quote a turn start, as
// a command reading a record would print it.
func longTurn(n int) []string {
	out := strings.Repeat(`{"type":"task_started"} `, 1<<20/24)
	ls := []string{started("t0"), said("t0", "u0", "an earlier question"), answered("t0", "a0", "an earlier answer"), ended("t0"),
		started("t1"), said("t1", "u1", "build the docs site")}
	for i := 0; i < n; i++ {
		ls = append(ls, ran("t1", fmt.Sprintf("call_%d", i), fmt.Sprintf("make page-%d", i), out))
		if i == 1 { // the words of a turn start, said: not where the turn began
			ls = append(ls, answered("t1", "a-word", "task_started"))
		}
	}
	return append(ls, ran("t1", "call_last", "make index", "ok"))
}

func read(t *testing.T, p string) []chat.Turn {
	t.Helper()
	ts, err := (&Reader{}).Turns(p)
	if err != nil {
		t.Fatal(err)
	}
	return ts
}

// The turn started further back than MaxTail: read from its start, open, with its steps.
func TestTurnLongerThanTheTail(t *testing.T) {
	p := writeLines(t, longTurn(6)...)
	ts := read(t, p)
	if len(ts) == 0 {
		t.Fatal("no turns")
	}
	last := ts[len(ts)-1]
	if last.Said != "build the docs site" || last.End != "open" || len(last.Steps) != 8 { // 7 commands and the note
		t.Fatalf("last turn %q, %s, %d steps", last.Said, last.End, len(last.Steps))
	}
	if d, err := (&Reader{}).Detail(p, "call_0"); err != nil || !strings.Contains(d.Input, "make page-0") {
		t.Fatalf("detail of the first step: %+v %v", d, err)
	}
}

// One command output over 4 MB: read whole, no format error.
func TestALineOverMaxTail(t *testing.T) {
	ts := read(t, writeLines(t, started("t1"), said("t1", "u1", "print the log"), ran("t1", "call_1", "cat big.log", strings.Repeat("y", 5<<20)),
		answered("t1", "a1", "It is long."), ended("t1")))
	if len(ts) != 1 || ts[0].Said != "print the log" || ts[0].End != "done" || ts[0].Reply != "It is long." {
		t.Fatalf("%+v", ts)
	}
}

// The turn started further back than MaxBack: it shows from where the reading starts, open, without what was said.
func TestStartBeyondMaxBack(t *testing.T) {
	p := writeLines(t, longTurn(chat.MaxBack>>20+2)...)
	ts := read(t, p)
	if len(ts) != 1 || ts[0].ID != "t1" || ts[0].Said != "" || ts[0].End != "open" || len(ts[0].Steps) == 0 {
		t.Fatalf("%d turns: %+v", len(ts), ts)
	}
	f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString(answered("t1", "a-end", "The site is built.") + "\n" + ended("t1") + "\n")
	f.Close()
	if ts = read(t, p); len(ts) != 1 || ts[0].End != "done" || ts[0].Reply != "The site is built." {
		t.Fatalf("%+v", ts)
	}
	f, _ = os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString(started("t9") + "\n" + said("t9", "u9", "now the index") + "\n")
	f.Close()
	if ts = read(t, p); len(ts) != 1 || ts[0].Said != "now the index" || ts[0].End != "open" {
		t.Fatalf("%+v", ts)
	}
}

// A turn the window starts in, never ended, and a new one started: only the new one.
func TestNewTurnAfterAnUnendedOne(t *testing.T) {
	ls := append(longTurn(6), started("t2"), said("t2", "u2", "and the index"))
	if ts := read(t, writeLines(t, ls...)); len(ts) != 1 || ts[0].Said != "and the index" {
		t.Fatalf("%+v", ts)
	}
}
