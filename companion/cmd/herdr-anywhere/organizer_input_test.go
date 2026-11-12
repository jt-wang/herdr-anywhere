package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"herdr-anywhere/companion/internal/track"
)

// Only the line at the very bottom that starts with ❯ decides: it is an empty
// input box when the lines around it are its two rules. A suggestion, a
// half-typed draft or a choice dialog is not.
func TestInputBoxEmpty(t *testing.T) {
	const rule = "──────────────────────────"
	for _, c := range []struct {
		name, screen string
		want         bool
	}{
		{"empty", rule + "\n❯ \n" + rule + "\n  ⏵⏵ auto mode on (shift+tab to cycle)\n", true},
		{"empty without a space", rule + "\n❯\n" + rule, true},
		{"empty with a non-breaking space", rule + "\n❯ \u00a0\n" + rule, true},
		{"empty with carriage returns", rule + "\r\n❯ \r\n" + rule + "\r\n", true},
		{"suggestion command", rule + "\n❯ /made-up-command\n" + rule + "\n", false},
		{"half-typed words", rule + "\n❯ the build is\n" + rule + "\n", false},
		{"choice dialog", "  Pick what to keep\n\n  ❯ 1. Keep the old entries\n    2. Start over\n    3. Cancel\n\n  Enter to continue · Esc to cancel\n", false},
		{"choice dialog under an old input box", rule + "\n❯ \n" + rule + "\n\n  ❯ 1. Keep the old entries\n  Enter to continue · Esc to cancel\n", false},
		{"no input box", "✻ Baked for 31s · done 1:23 PM\n\n  everything filed\n", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := inputBoxEmpty(c.screen); got != c.want {
				t.Errorf("inputBoxEmpty() = %v, want %v for:\n%s", got, c.want, c.screen)
			}
		})
	}
}

// A suggestion in the input box is not typed over: the batch waits for the next
// pass. An empty input box gets it. The pane's draft is never cleared with keys.
func TestOrganizerKickOnlyTypesIntoAnEmptyInputBox(t *testing.T) {
	state := t.TempDir()
	st := track.Open(filepath.Join(state, "track.json"))
	st.Update(func(f *track.File) error {
		f.Queue = []track.Pending{{ID: "Q1", Kind: "session", Text: "x", At: 1}}
		return nil
	})
	hc, sent := fakeHCFiles(t, `{"terminals":[{"address":"t8","name":"herdr-organizer","state":"live","status":"idle","turn":1,"agent":"claude-code"}]}`,
		map[string]string{"t8": `{"address":"t8","state":"live","labels":{"herdr.role":"organizer"},"status":{"state":"idle"}}`})
	read := filepath.Join(filepath.Dir(hc), "read-t8.json")
	os.WriteFile(read, []byte(`{"complete":true,"how":"screen","text":"──────────────────────────\n❯ /made-up-command\n──────────────────────────\n"}`), 0o600)
	if did, err := organizerKick(hc, state, st, time.Now()); err != nil || did != "" {
		t.Fatalf("suggestion: %q %v", did, err)
	}
	if b, _ := os.ReadFile(sent); len(b) != 0 {
		t.Fatalf("typed into a pane showing a suggestion: %s", b)
	}
	os.WriteFile(read, []byte(fakeEmptyInputScreenJSON), 0o600)
	if did, err := organizerKick(hc, state, st, time.Now()); err != nil || !strings.Contains(did, "t8") {
		t.Fatalf("empty: %q %v", did, err)
	}
	b, _ := os.ReadFile(sent)
	if !strings.Contains(string(b), "send t8 Filing queue") {
		t.Fatalf("not sent into an empty input box: %s", b)
	}
	if strings.Contains(string(b), "keys") {
		t.Fatalf("pressed keys in the pane: %s", b)
	}
}

// The input-box check reads Claude Code's screen. A filing pane running another
// agent (Pi, Kimi Code, Codex) draws its input differently, so it still gets the
// batch when it is idle, as before.
func TestOrganizerKickStillTypesIntoOtherAgents(t *testing.T) {
	for _, a := range []struct{ id, cmd string }{{"pi", "pi"}, {"kimi-code", "kimi --yolo"}, {"codex", "codex --no-daemon"}} {
		t.Run(a.id, func(t *testing.T) {
			state := t.TempDir()
			cfg := defaultOrganizer(state)
			cfg.Agent, cfg.Cmd = a.id, a.cmd
			if err := saveOrganizer(state, cfg); err != nil {
				t.Fatal(err)
			}
			st := track.Open(filepath.Join(state, "track.json"))
			st.Update(func(f *track.File) error {
				f.Queue = []track.Pending{{ID: "Q1", Kind: "session", Text: "x", At: 1}}
				return nil
			})
			hc, sent := fakeHCFiles(t, `{"terminals":[{"address":"t8","name":"herdr-organizer","state":"live","status":"idle","turn":1,"agent":"`+a.id+`"}]}`,
				map[string]string{"t8": `{"address":"t8","state":"live","labels":{"herdr.role":"organizer"},"status":{"state":"idle"}}`})
			os.WriteFile(filepath.Join(filepath.Dir(hc), "read-t8.json"), []byte(`{"complete":true,"how":"screen","text":"Ready when you are.\n\n> \n  made-up-model · ~/notes\n"}`), 0o600)
			if did, err := organizerKick(hc, state, st, time.Now()); err != nil || !strings.Contains(did, "t8") {
				t.Fatalf("%s: %q %v", a.id, did, err)
			}
			if b, _ := os.ReadFile(sent); !strings.Contains(string(b), "send t8 Filing queue") {
				t.Fatalf("%s not sent: %s", a.id, b)
			}
		})
	}
}
