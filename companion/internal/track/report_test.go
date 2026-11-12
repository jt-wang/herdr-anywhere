package track

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// A synthetic Claude Code transcript in 2.1.319's layout: an old turn, then the
// last real prompt, a tool_use opener, a tool result, and an end_turn reply
// written as two lines with the same message id.
const claudeTranscript = `{"type":"user","message":{"role":"user","content":"old task"}}
{"type":"assistant","message":{"id":"m0","role":"assistant","stop_reason":"end_turn","content":[{"type":"text","text":"old answer"}]}}
{"type":"user","isMeta":true,"message":{"role":"user","content":"<meta>"}}
{"type":"user","message":{"role":"user","content":"fix total_items"}}
{"type":"assistant","message":{"id":"m1","role":"assistant","stop_reason":"tool_use","content":[{"type":"text","text":"Let me look."},{"type":"tool_use","name":"Edit"}]}}
{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"ok"}]}}
{"type":"assistant","message":{"id":"m2","role":"assistant","stop_reason":"end_turn","content":[{"type":"thinking","thinking":"hmm"}]}}
{"type":"assistant","message":{"id":"m2","role":"assistant","stop_reason":"end_turn","content":[{"type":"text","text":"Fixed total_items to sum qty; tests pass."}]}}
`

const piSession = `{"type":"session","version":3,"id":"s1","cwd":"/tmp/p/acme-launch"}
{"type":"message","message":{"role":"user","content":[{"type":"text","text":"write the X post"}]}}
{"type":"message","message":{"role":"assistant","stopReason":"toolUse","content":[{"type":"text","text":"Writing."}]}}
{"type":"message","message":{"role":"assistant","stopReason":"stop","content":[{"type":"text","text":"Wrote posts/x.md: Acme 1.0 is out."}]}}
`

func TestClaudeLastReplySkipsOpenersAndJoinsSplitLines(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.jsonl")
	write(t, p, claudeTranscript)
	got, err := claudeLastReply(p)
	if err != nil || got != "Fixed total_items to sum qty; tests pass." {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestClaudeInterruptedTurnHasNoReply(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.jsonl")
	write(t, p, `{"type":"user","message":{"role":"user","content":"a"}}
{"type":"assistant","message":{"id":"m0","role":"assistant","stop_reason":"end_turn","content":[{"type":"text","text":"A done"}]}}
{"type":"user","message":{"role":"user","content":"b"}}
{"type":"assistant","message":{"id":"m1","role":"assistant","stop_reason":"tool_use","content":[{"type":"tool_use","name":"Bash"}]}}
`)
	got, err := claudeLastReply(p)
	if err != nil || got != "" {
		t.Fatalf("an unfinished turn must not fall back to the previous reply: %q, %v", got, err)
	}
}

func TestPiLastReply(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.jsonl")
	write(t, p, piSession)
	got, err := piLastReply(p)
	if err != nil || got != "Wrote posts/x.md: Acme 1.0 is out." {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestPiSessionDirFromCwd(t *testing.T) {
	if got := piDirName("/private/tmp/agentlab/projects/acme-launch"); got != "--private-tmp-agentlab-projects-acme-launch--" {
		t.Fatalf("got %q", got)
	}
}

func TestReportFindsRepliesCommitsAndMsg(t *testing.T) {
	d := t.TempDir()
	claudeHome := filepath.Join(d, "claude")
	piHome := filepath.Join(d, "pi")
	write(t, filepath.Join(claudeHome, "sessions", "101.json"), `{"pid":101,"sessionId":"sid-a","cwd":"/tmp/p/acme-api"}`)
	write(t, filepath.Join(claudeHome, "projects", "-tmp-p-acme-api", "sid-a.jsonl"), claudeTranscript)
	write(t, filepath.Join(claudeHome, "projects", "-tmp-p-acme-web", "sid-w.jsonl"), claudeTranscript)
	write(t, filepath.Join(piHome, "sessions", piDirName("/tmp/p/acme-launch"), "2026-11-07T05-00-00_s1.jsonl"), piSession)

	f := File{Things: []Thing{
		{ID: "A1", Outcome: "fix", Stage: Doing, Panes: []PaneRef{{Side: "hc", Addr: "api", TurnAt: ip(0), At: ms(t0)}}},
		{ID: "A2", Outcome: "footer", Stage: Doing, Panes: []PaneRef{{Side: "herdr", PaneID: "w1:p1", TurnAt: ip(2), At: ms(t0)}}},
		{ID: "A3", Outcome: "post", Stage: Doing, Panes: []PaneRef{{Side: "hc", Addr: "launch", At: ms(t0)}}},
		{ID: "A9", Outcome: "nothing", Stage: Doing, Panes: []PaneRef{{Side: "hc", Addr: "gone"}}},
	}}
	live := Live{HCOK: true, HerdrOK: true,
		HC: []HCTerm{
			{Address: "t2", Name: "api", State: "live", Status: "done", Turn: 1, Cwd: "/tmp/p/acme-api"},
			{Address: "t3", Name: "launch", State: "live", Status: "done", Turn: 1, Cwd: "/tmp/p/acme-launch"},
		},
		Herdr: []HerdrAgent{{PaneID: "w1:p1", Name: "web", Status: "done", Seq: 4, Cwd: "/tmp/p/acme-web", Agent: "claude", Session: "sid-w"}},
	}
	var sinces []time.Time
	src := ReportSources{
		ClaudeHome: claudeHome, PiHome: piHome,
		HCGet: func(addr string) (HCDetail, error) {
			switch addr {
			case "t2":
				return HCDetail{Pid: 101, App: "claude-code", Msg: "fixed total_items"}, nil
			case "t3":
				return HCDetail{Pid: 202, App: "pi"}, nil
			}
			return HCDetail{}, os.ErrNotExist
		},
		Git: func(cwd string, since time.Time) ([]string, int, error) {
			sinces = append(sinces, since)
			if cwd == "/tmp/p/acme-api" {
				return []string{"abc123 Fix total_items"}, 0, nil
			}
			return nil, 1, nil
		},
	}
	reps := Report(f, live, src, t0.Add(time.Hour), 24*time.Hour, "")
	byThing := map[string]PaneReport{}
	for _, r := range reps {
		byThing[r.Thing] = r
	}
	a1 := byThing["A1"]
	if a1.Reply != "Fixed total_items to sum qty; tests pass." || a1.Msg != "fixed total_items" || len(a1.Commits) != 1 || a1.Turns != 1 {
		t.Fatalf("A1 = %+v", a1)
	}
	if a2 := byThing["A2"]; a2.Reply == "" || a2.Agent != "claude" || a2.Dirty != 1 {
		t.Fatalf("A2 (old Herdr, by its session id) = %+v", a2)
	}
	if a3 := byThing["A3"]; a3.Reply != "Wrote posts/x.md: Acme 1.0 is out." || !strings.Contains(a3.Source, "pi") {
		t.Fatalf("A3 = %+v", a3)
	}
	if a9 := byThing["A9"]; a9.Reply != "" || a9.Note == "" {
		t.Fatalf("A9 should say why there is no reply: %+v", a9)
	}
	for _, s := range sinces {
		if !s.Equal(t0) {
			t.Fatalf("commits should be read from the hand-off time, got %v", s)
		}
	}
	txt := ReportText(reps)
	for _, want := range []string{"A1", "t2 api", "Fixed total_items", "abc123", "A9"} {
		if !strings.Contains(txt, want) {
			t.Fatalf("report text lacks %q:\n%s", want, txt)
		}
	}
}

func TestReplyIsCutAt300Runes(t *testing.T) {
	long := strings.Repeat("字", 400)
	if got := cut(long, 300); len([]rune(got)) != 301 || !strings.HasSuffix(got, "…") {
		t.Fatalf("len %d", len([]rune(got)))
	}
}
