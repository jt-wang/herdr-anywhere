package main

import (
	"testing"

	"herdr-anywhere/companion/internal/chat/antigravity"
	"herdr-anywhere/companion/internal/chat/claude"
	"herdr-anywhere/companion/internal/chat/codex"
	"herdr-anywhere/companion/internal/chat/cursor"
	"herdr-anywhere/companion/internal/chat/opencode"
	"herdr-anywhere/companion/internal/chat/pi"
)

// The chat view reads an agent's own record when herdr-core's status.app names it: "claude-code", "codex", "pi", "opencode" and
// "antigravity" are what Claude Code, the Codex TUI, Pi, OpenCode 2 and the Antigravity CLI report in OSC 7501.
// Oh My Pi reports "omp".
func TestChatReadersByApp(t *testing.T) {
	setups := chatSetups("/hc")
	r := chatReaders(setups)
	if _, ok := r["claude-code"].(*claude.Reader); !ok {
		t.Errorf("claude-code: %T", r["claude-code"])
	}
	if _, ok := r["codex"].(*codex.Reader); !ok {
		t.Errorf("codex: %T", r["codex"])
	}
	if _, ok := r["pi"].(*pi.Reader); !ok {
		t.Errorf("pi: %T", r["pi"])
	}
	if _, ok := r["opencode"].(*opencode.Reader); !ok {
		t.Errorf("opencode: %T", r["opencode"])
	}
	if a, ok := r["antigravity"].(*antigravity.Reader); !ok || a.Dir != antigravity.Dir() {
		t.Errorf("antigravity: %T %+v", r["antigravity"], r["antigravity"])
	}
	if _, ok := r["cursor"].(*cursor.Reader); !ok {
		t.Fatalf("cursor: %T", r["cursor"])
	}
	if _, ok := chatSetups("/hc")["cursor"].(*cursor.Setup); !ok {
		t.Fatal("missing cursor setup")
	}
	if _, ok := r["omp"].(*pi.Reader); !ok {
		t.Errorf("omp: %T", r["omp"])
	}
	if len(r) != 7 {
		t.Errorf("readers %v", r)
	}
	if len(setups) != 3 {
		t.Errorf("setups %v", setups)
	}
	for _, agent := range []string{"pi", "omp"} {
		setup, ok := setups[agent].(*pi.Setup)
		if !ok || setup.Agent() != agent {
			t.Fatalf("%s setup: %v", agent, setups[agent])
		}
		reader, ok := r[agent].(*pi.Reader)
		if !ok || reader.Setup != setup {
			t.Fatalf("%s reader lost its setup", agent)
		}
	}
	if setups["omp"].Reload() != "/restart" {
		t.Fatal("OMP lost its restart command")
	}
	if r["cursor"].(*cursor.Reader).Setup != setups["cursor"] {
		t.Fatal("Cursor reader lost its setup")
	}
}
