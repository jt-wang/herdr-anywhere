package chat_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"herdr-anywhere/companion/internal/chat"
	"herdr-anywhere/companion/internal/chat/claude"
	"herdr-anywhere/companion/internal/chat/codex"
	"herdr-anywhere/companion/internal/chat/opencode"
	"herdr-anywhere/companion/internal/chat/pi"
	"herdr-anywhere/companion/internal/testfake"
	"herdr-anywhere/companion/internal/testfake/replay"
)

// Only discovery is fixed here. Turns, caching, completion semantics and pagination are the real readers'.
type locatedReader struct {
	chat.Reader
	chat.HistoryReader
	path string
}

func (r *locatedReader) Find(int) (string, string, error) { return r.path, "", nil }

func TestRecordProgressIsIndependentOfTerminalStatus(t *testing.T) {
	for _, c := range []struct {
		agent, fixture string
		reader         chat.Reader
	}{
		{"claude-code", "claude/testdata/claude-code/2.1.325/steps.jsonl", &claude.Reader{}},
		{"codex", "codex/testdata/codex/0.167.1/steps.jsonl", &codex.Reader{}},
		{"opencode", "opencode/testdata/opencode/2.2.0/steps.json", &opencode.Reader{}},
		{"pi", "pi/testdata/pi/1.4.0/steps.jsonl", &pi.Reader{}},
	} {
		t.Run(c.agent, func(t *testing.T) {
			script, err := replay.Load(c.agent, c.fixture)
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			player, err := script.Start(replay.Options{Dir: dir, Cwd: "/work/grove", Pid: 4242})
			if err != nil {
				t.Fatal(err)
			}
			defer player.Close()
			if err := player.Play("Inspect the grove", 0); err != nil {
				t.Fatal(err)
			}
			path := player.Path()
			complete, err := c.reader.Turns(path)
			if err != nil || len(complete) != 1 || complete[0].End != "done" || complete[0].Reply == "" {
				t.Fatalf("recorded completion: %+v %v", complete, err)
			}
			r := &locatedReader{Reader: c.reader, HistoryReader: c.reader.(chat.HistoryReader), path: path}
			core := filepath.Join(dir, "terminal.json")
			hc := testfake.HC(t, dir, "hc", testfake.Spec{On: map[string][]testfake.Reply{"get": {{Cat: core}}}})
			s := &chat.Service{HC: hc, Readers: map[string]chat.Reader{c.agent: r}}
			setStatus := func(state string) {
				t.Helper()
				b, _ := json.Marshal(map[string]any{"status": map[string]string{"app": c.agent, "state": state}, "foreground": map[string]int{"pid": 4242}})
				if err := os.WriteFile(core, b, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			check := func(want []chat.Turn) chat.Answer {
				t.Helper()
				a, err := s.ChatPage("t1", "")
				if err != nil || !a.Chat || a.Unknown != "" || a.Session == "" || !reflect.DeepEqual(a.Turns, want) {
					t.Fatalf("successfully read record must not wait for terminal agreement: %+v err=%v", a, err)
				}
				return a
			}
			for _, state := range []string{"working", "blocked", "done", "idle", "error", ""} {
				setStatus(state)
				for range 3 {
					check(complete)
				}
			}

			// A terminal may finish before its last record write, or be killed without that write ever happening.
			var restore func()
			if c.agent == "opencode" {
				p, id, _ := strings.Cut(path, "#")
				db, err := sql.Open("sqlite", p)
				if err != nil {
					t.Fatal(err)
				}
				defer db.Close()
				if _, err := db.Exec("CREATE TEMP TABLE completion AS SELECT * FROM session_message WHERE session_id = ? AND type = 'idle';", id); err != nil {
					t.Fatal(err)
				}
				if _, err := db.Exec("DELETE FROM session_message WHERE session_id = ? AND type = 'idle'", id); err != nil {
					t.Fatal(err)
				}
				restore = func() {
					if _, err := db.Exec("INSERT INTO session_message SELECT * FROM completion"); err != nil {
						t.Fatal(err)
					}
				}
			} else {
				b, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				lines := bytes.Split(bytes.TrimSuffix(b, []byte{'\n'}), []byte{'\n'})
				cut := len(lines) - 1
				for i, raw := range lines {
					var l struct {
						Message struct {
							Stop string `json:"stop_reason"`
						} `json:"message"`
						Payload struct {
							Type string `json:"type"`
						} `json:"payload"`
					}
					if err := json.Unmarshal(raw, &l); err != nil {
						t.Fatal(err)
					}
					if (c.agent == "claude-code" && l.Message.Stop == "end_turn") || (c.agent == "codex" && l.Payload.Type == "task_complete") {
						cut = i
						break
					}
				}
				if err := os.WriteFile(path, append(bytes.Join(lines[:cut], []byte{'\n'}), '\n'), 0o600); err != nil {
					t.Fatal(err)
				}
				restore = func() {
					if err := os.WriteFile(path, b, 0o600); err != nil {
						t.Fatal(err)
					}
				}
			}
			open, err := c.reader.Turns(path)
			if err != nil || len(open) != 1 || open[0].End != "open" {
				t.Fatalf("record before completion: %+v %v", open, err)
			}
			setStatus("done")
			for range 3 {
				check(open)
			}
			restore()
			first := check(complete)
			if err := player.Play("Inspect the orchard", 0); err != nil {
				t.Fatal(err)
			}
			latest, err := c.reader.Turns(path)
			if err != nil || len(latest) != 2 || latest[1].Said != "Inspect the orchard" || latest[1].End != "done" {
				t.Fatalf("next recorded turn: %+v %v", latest, err)
			}
			if a := check(latest); a.Session != first.Session {
				t.Fatal("an append changed session identity")
			}
		})
	}
}
