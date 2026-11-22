package pi

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// OMP_LIVE=/path/to/omp go test ./internal/chat/pi -run OMPLive -update
func TestOMPLive(t *testing.T) {
	bin := os.Getenv("OMP_LIVE")
	if bin == "" {
		t.Skip("OMP_LIVE runs real Oh My Pi against the local scripted provider")
	}
	r := liveRun(t, bin)
	agent := filepath.Join(r.root, "home", ".pi", "agent")
	config := "setupVersion: 999\nstartup:\n  checkUpdate: false\n  showSplash: false\n  setupWizard: false\nupdate:\n  auto: false\nmarketplace:\n  autoUpdate: off\n"
	if err := os.WriteFile(filepath.Join(agent, "config.yml"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	v := strings.TrimPrefix(strings.TrimSpace(pi(t, bin, r.cwd, r.env, "--version")), "omp/")
	if v != "19.0.0" {
		t.Fatalf("fixtures require OMP 19.0.0, got %q", v)
	}
	f := r.session(t, []string{"HELLO sample", "LIST the files here", "BREAK the build", "LOOK at shot.png"})
	b, err := os.ReadFile(f)
	if err != nil {
		t.Fatal(err)
	}
	if *update {
		dir := filepath.Join("testdata", "omp", v)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "turns.jsonl"), scrub(t, b, r.root), 0o644); err != nil {
			t.Fatal(err)
		}
		cli, err := filepath.EvalSymlinks(bin)
		if err != nil {
			t.Fatal(err)
		}
		module := filepath.Join(filepath.Dir(filepath.Dir(cli)), "src", "session", "session-manager.ts")
		script := fmt.Sprintf(`import { SessionManager } from %q;
const s = await SessionManager.open(process.argv[2]);
const entries = s.getEntries();
const first = entries.find(e => e.type === "message" && e.message.role === "assistant");
const user = entries.find(e => e.type === "message" && e.message.role === "user");
s.branch(first.id);
s.appendMessage({...user.message, content: [{type: "text", text: "HELLO alternate branch"}]});
s.appendMessage(first.message);
`, filepath.ToSlash(module))
		js := filepath.Join(r.root, "branch.ts")
		if err := os.WriteFile(js, []byte(script), 0o644); err != nil {
			t.Fatal(err)
		}
		pi(t, "bun", r.cwd, r.env, js, f)
		b, err = os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "branch.jsonl"), scrub(t, b, r.root), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	checkOMPTurns(t, f)
}
