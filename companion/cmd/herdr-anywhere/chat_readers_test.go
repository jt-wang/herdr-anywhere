package main

import (
	"testing"

	"herdr-anywhere/companion/internal/chat/claude"
	"herdr-anywhere/companion/internal/chat/codex"
	"herdr-anywhere/companion/internal/chat/opencode"
	"herdr-anywhere/companion/internal/chat/pi"
)

// The chat view reads an agent's own record when herdr-core's status.app names it: "claude-code", "codex", "pi" and "opencode" are what
// Claude Code, the Codex TUI, Pi and OpenCode 2 report in OSC 7501.
func TestChatReadersByApp(t *testing.T) {
	r := chatReaders(chatSetups("/hc"))
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
	if len(r) != 4 {
		t.Errorf("readers %v", r)
	}
}
